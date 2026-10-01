# orpay (PHP)

Server SDK for apps that take payments through ORPay on OpenReserve. PHP 7.4+, curl and json.

```php
use OpenReserve\ORPay;

$orpay = new ORPay(getenv('ORPAY_KEY'), 'https://pay.example');
$escrow = $orpay->createEscrow('@gadgethub', [['label' => 'iPhone 14', 'amount' => '420000']], [
    'currency' => 'NGN', 'description' => 'iPhone 14', 'checklist' => ['IMEI matches'],
]);
header('Location: ' . $escrow['funding_url']);
```

Webhooks:

```php
$event = ORPay::verifyWebhook(file_get_contents('php://input'), $_SERVER['HTTP_ORPAY_SIGNATURE'], getenv('ORPAY_WEBHOOK_SECRET'));
if ($event['type'] === 'invoice.paid') { /* mark order paid */ }
```

Test: `php tests/verify_test.php` (checks signatures against a vector produced by the ORPay server).

For WordPress shops, see the WooCommerce plugin in `../woocommerce`.
