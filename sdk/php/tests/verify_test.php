<?php
// Run: php tests/verify_test.php
require __DIR__ . '/../src/ORPay.php';

use OpenReserve\ORPay;
use OpenReserve\ORPayError;

$body = '{"type":"invoice.paid","created":1,"data":{"id":"abc","status":"paid"}}';
// Produced by the Go server: SignWebhook("whsec_test", 1700000000, body).
$header = 't=1700000000,v1=556489ff3b74ef0ccdfeba8df715a5b540e49d7dcbadd3a1aa2f9965bcade456';

$ev = ORPay::verifyWebhook($body, $header, 'whsec_test', 300, 1700000000);
assert($ev['type'] === 'invoice.paid') or exit("FAIL: go vector\n");

foreach ([[str_replace('paid', 'expired', $body), 'whsec_test', 1700000000], [$body, 'whsec_other', 1700000000], [$body, 'whsec_test', 1700003600]] as [$b, $s, $now]) {
    try {
        ORPay::verifyWebhook($b, $header, $s, 300, $now);
        exit("FAIL: accepted a bad webhook\n");
    } catch (ORPayError $e) {
    }
}
echo ORPay::escrowActionUrl(['escrow_url' => 'https://pay.example/?escrow=abc'], 'release') === 'https://pay.example/?escrow=abc&action=release' ? "ok\n" : "FAIL: action url\n";
