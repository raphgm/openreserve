<?php
// ORPay server SDK for PHP: checkouts, milestone escrow and webhooks.
// PHP 7.4+ with the curl and json extensions. Keep your API key on the server.

namespace OpenReserve;

class ORPayError extends \Exception
{
    public ?int $status;

    public function __construct(string $message, ?int $status = null)
    {
        parent::__construct($message);
        $this->status = $status;
    }
}

class ORPay
{
    public const VERSION = '0.2.0';
    public const ESCROW_ACTIONS = ['release', 'dispute', 'dispatch', 'refund', 'resolve', 'claim'];

    private string $apiKey;
    private string $baseUrl;
    private int $timeout;

    public function __construct(string $apiKey, string $baseUrl = 'http://localhost:4000', int $timeout = 15)
    {
        if (strpos($apiKey, 'orp_sk_') !== 0) {
            throw new \InvalidArgumentException('ORPay: apiKey must start with orp_sk_');
        }
        $this->apiKey = $apiKey;
        $this->baseUrl = rtrim($baseUrl, '/');
        $this->timeout = $timeout;
    }

    private function call(string $method, string $path, ?array $body = null)
    {
        $ch = curl_init($this->baseUrl . $path);
        $headers = ['Authorization: Bearer ' . $this->apiKey, 'Content-Type: application/json', 'User-Agent: orpay-php/' . self::VERSION];
        curl_setopt_array($ch, [
            CURLOPT_CUSTOMREQUEST => $method,
            CURLOPT_RETURNTRANSFER => true,
            CURLOPT_HTTPHEADER => $headers,
            CURLOPT_TIMEOUT => $this->timeout,
        ]);
        if ($body !== null) {
            curl_setopt($ch, CURLOPT_POSTFIELDS, json_encode(array_filter($body, fn ($v) => $v !== null)));
        }
        $raw = curl_exec($ch);
        if ($raw === false) {
            $err = curl_error($ch);
            curl_close($ch);
            throw new ORPayError('ORPay request failed: ' . $err);
        }
        $status = curl_getinfo($ch, CURLINFO_RESPONSE_CODE);
        curl_close($ch);
        $data = json_decode($raw, true) ?? [];
        if ($status >= 300) {
            throw new ORPayError($data['error'] ?? "ORPay request failed ($status)", $status);
        }
        return $data;
    }

    /** Create a checkout; redirect the customer to checkout_url. $amount is a decimal string. */
    public function createCheckout(string $amount, array $opts = []): array
    {
        return $this->call('POST', '/api/v1/checkout', ['amount' => $amount] + self::snake($opts));
    }

    public function getCheckout(string $id): array
    {
        return $this->call('GET', '/api/v1/checkout/' . rawurlencode($id));
    }

    public function listCheckouts(?string $status = null): array
    {
        return $this->call('GET', '/api/v1/checkout' . ($status ? '?status=' . rawurlencode($status) : ''));
    }

    /** Ask a buyer to lock funds; send them to funding_url. Milestone amounts are decimal strings. */
    public function createEscrow(string $seller, array $milestones, array $opts = []): array
    {
        foreach ($milestones as $m) {
            if (!isset($m['amount']) || !is_string($m['amount'])) {
                throw new \InvalidArgumentException('ORPay: milestones must be [["label" => ..., "amount" => "40000.00"]]');
            }
        }
        return $this->call('POST', '/api/v1/escrows', ['seller' => $seller, 'milestones' => $milestones] + self::snake($opts));
    }

    public function getEscrow(string $id): array
    {
        return $this->call('GET', '/api/v1/escrows/' . rawurlencode($id));
    }

    public function listEscrows(): array
    {
        return $this->call('GET', '/api/v1/escrows');
    }

    /** Withdraw an escrow request nobody has funded yet. */
    public function cancelEscrow(string $id): array
    {
        return $this->call('POST', '/api/v1/escrows/' . rawurlencode($id) . '/cancel');
    }

    /** Link for the buyer, seller or arbiter to sign one step. Your key can never move escrowed money. */
    public static function escrowActionUrl(array $escrow, string $action): string
    {
        if (empty($escrow['escrow_url'])) {
            throw new ORPayError('Escrow is not funded yet: send the buyer to funding_url first', 409);
        }
        if (!in_array($action, self::ESCROW_ACTIONS, true)) {
            throw new \InvalidArgumentException('ORPay: action must be one of ' . implode(', ', self::ESCROW_ACTIONS));
        }
        return $escrow['escrow_url'] . '&action=' . $action;
    }

    /** Verify an ORPay webhook and return the event. Pass the raw request body (file_get_contents('php://input')). */
    public static function verifyWebhook(string $rawBody, string $signatureHeader, string $secret, int $tolerance = 300, ?int $now = null): array
    {
        $parts = [];
        foreach (explode(',', $signatureHeader) as $kv) {
            $p = explode('=', trim($kv), 2);
            if (count($p) === 2) {
                $parts[$p[0]] = $p[1];
            }
        }
        if (!isset($parts['t'], $parts['v1']) || !ctype_digit($parts['t']) || strlen($parts['v1']) !== 64) {
            throw new ORPayError('Malformed ORPay-Signature header', 400);
        }
        $ts = (int) $parts['t'];
        if (abs(($now ?? time()) - $ts) > $tolerance) {
            throw new ORPayError('Webhook timestamp outside tolerance', 400);
        }
        $expected = hash_hmac('sha256', $ts . '.' . $rawBody, $secret);
        if (!hash_equals($expected, $parts['v1'])) {
            throw new ORPayError('Invalid webhook signature', 400);
        }
        return json_decode($rawBody, true);
    }

    private static function snake(array $opts): array
    {
        $out = [];
        foreach ($opts as $k => $v) {
            $out[strtolower(preg_replace('/[A-Z]/', '_$0', $k))] = $v;
        }
        return $out;
    }
}
