<?php
/**
 * Plugin Name: ORPay for WooCommerce
 * Description: Take payments with ORPay on OpenReserve: instant checkout, or escrow so customers pay only after they receive the goods.
 * Version: 0.2.0
 * Requires PHP: 7.4
 * License: Apache-2.0
 */

if (!defined('ABSPATH')) {
    exit;
}

add_action('plugins_loaded', function () {
    if (!class_exists('WC_Payment_Gateway')) {
        return;
    }
    require_once __DIR__ . '/includes/ORPay.php';

    class WC_Gateway_ORPay extends WC_Payment_Gateway
    {
        public function __construct()
        {
            $this->id = 'orpay';
            $this->method_title = 'ORPay';
            $this->method_description = 'Pay with ORPay. In escrow mode the customer\'s money is locked until they confirm delivery.';
            $this->has_fields = false;
            $this->init_form_fields();
            $this->init_settings();
            $this->title = $this->get_option('title');
            $this->description = $this->get_option('description');
            add_action('woocommerce_update_options_payment_gateways_' . $this->id, [$this, 'process_admin_options']);
            add_action('woocommerce_api_orpay', [$this, 'webhook']); // https://yourshop/?wc-api=orpay
        }

        public function init_form_fields()
        {
            $this->form_fields = [
                'enabled' => ['title' => 'Enable', 'type' => 'checkbox', 'label' => 'Enable ORPay', 'default' => 'no'],
                'title' => ['title' => 'Title', 'type' => 'text', 'default' => 'ORPay (pay safely)'],
                'description' => ['title' => 'Description', 'type' => 'textarea', 'default' => 'Pay with your ORPay wallet, card or bank.'],
                'base_url' => ['title' => 'ORPay URL', 'type' => 'text', 'description' => 'Your ORPay deployment, e.g. https://pay.example'],
                'api_key' => ['title' => 'API key', 'type' => 'password', 'description' => 'From ORPay → Developers (starts with orp_sk_).'],
                'webhook_secret' => ['title' => 'Webhook secret', 'type' => 'password', 'description' => 'Set your app\'s webhook URL in ORPay to ' . home_url('/?wc-api=orpay')],
                'mode' => ['title' => 'Mode', 'type' => 'select', 'options' => ['checkout' => 'Instant checkout', 'escrow' => 'Escrow (buyer confirms delivery first)'], 'default' => 'checkout'],
                'seller' => ['title' => 'Your ORPay @username', 'type' => 'text', 'description' => 'Escrow mode: who receives the money.'],
                'ship_by_days' => ['title' => 'Deliver within (days)', 'type' => 'number', 'default' => 7],
                'checklist' => ['title' => 'Buyer checklist', 'type' => 'textarea', 'description' => 'Escrow mode: one item per line, ticked by the buyer before releasing.', 'default' => "Item matches the description\nItem is not damaged"],
            ];
        }

        private function client(): \OpenReserve\ORPay
        {
            return new \OpenReserve\ORPay($this->get_option('api_key'), $this->get_option('base_url'));
        }

        public function process_payment($order_id)
        {
            $order = wc_get_order($order_id);
            $amount = number_format((float) $order->get_total(), 2, '.', '');
            $return = $this->get_return_url($order);
            try {
                if ($this->get_option('mode') === 'escrow') {
                    $checklist = array_values(array_filter(array_map('trim', explode("\n", (string) $this->get_option('checklist')))));
                    $r = $this->client()->createEscrow($this->get_option('seller'), [['label' => 'Order #' . $order->get_order_number(), 'amount' => $amount]], [
                        'currency' => $order->get_currency() === 'NGN' ? 'NGN' : 'ORP',
                        'description' => 'Order #' . $order->get_order_number(),
                        'reference' => (string) $order_id,
                        'returnUrl' => $return,
                        'shipByDays' => (int) $this->get_option('ship_by_days'),
                        'checklist' => $checklist,
                    ]);
                    $order->update_meta_data('_orpay_escrow', $r['id']);
                    $url = $r['funding_url'];
                } else {
                    $r = $this->client()->createCheckout($amount, [
                        'description' => 'Order #' . $order->get_order_number(),
                        'reference' => (string) $order_id,
                        'returnUrl' => $return,
                    ]);
                    $order->update_meta_data('_orpay_checkout', $r['id']);
                    $url = $r['checkout_url'];
                }
                $order->update_status('pending', 'Waiting for ORPay payment.');
                $order->save();
                return ['result' => 'success', 'redirect' => $url];
            } catch (\Throwable $e) {
                wc_add_notice('ORPay: ' . $e->getMessage(), 'error');
                return ['result' => 'failure'];
            }
        }

        public function webhook()
        {
            $raw = file_get_contents('php://input');
            try {
                $event = \OpenReserve\ORPay::verifyWebhook($raw, $_SERVER['HTTP_ORPAY_SIGNATURE'] ?? '', $this->get_option('webhook_secret'));
            } catch (\Throwable $e) {
                status_header(400);
                exit;
            }
            $order = wc_get_order((int) ($event['data']['reference'] ?? 0));
            if ($order) {
                switch ($event['type']) {
                    case 'invoice.paid':
                        $order->payment_complete($event['data']['paid_tx'] ?? '');
                        break;
                    case 'escrow.funded':
                        $order->update_status('on-hold', 'Paid into ORPay escrow. Ship the order; the buyer releases payment after checking it.');
                        break;
                    case 'escrow.dispatched':
                        $order->add_order_note('Marked dispatched in ORPay.');
                        break;
                    case 'escrow.completed':
                        $order->payment_complete($event['data']['escrow_id'] ?? '');
                        $order->add_order_note('Buyer released the escrow: payment received.');
                        break;
                    case 'escrow.disputed':
                        $order->add_order_note('The buyer opened a dispute in ORPay. The arbiter will decide.');
                        break;
                    case 'escrow.refunded':
                    case 'escrow.resolved':
                        $order->update_status('refunded', 'ORPay escrow ended: ' . $event['type']);
                        break;
                    case 'invoice.expired':
                    case 'escrow.expired':
                        $order->update_status('cancelled', 'ORPay payment window expired.');
                        break;
                }
            }
            status_header(200);
            exit;
        }
    }

    add_filter('woocommerce_payment_gateways', function ($gateways) {
        $gateways[] = 'WC_Gateway_ORPay';
        return $gateways;
    });
});
