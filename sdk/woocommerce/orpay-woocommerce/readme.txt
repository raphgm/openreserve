=== ORPay for WooCommerce ===
Take payments with ORPay on OpenReserve.

Two modes:
* Instant checkout: the customer pays and the order is marked paid.
* Escrow: the customer's money is locked until they confirm delivery (with a
  checklist you set). Orders go On hold when funded and Completed when the
  buyer releases. Disputes are settled by a neutral arbiter.

Setup:
1. Upload the orpay-woocommerce folder to wp-content/plugins and activate it.
2. In ORPay → Developers, request access for your shop, then create an API key.
3. WooCommerce → Settings → Payments → ORPay: paste the ORPay URL, API key and
   webhook secret, and choose a mode.
4. In ORPay → Developers → settings, set your webhook URL to
   https://yourshop.com/?wc-api=orpay
