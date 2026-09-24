# @openreserve/orpay

Accept payments in your app (e.g. Gabis, Paynautik) through ORPay. Customers
pay from their ORPay wallet; money settles on-chain to your app's settlement
wallet and your server gets a signed webhook.

## 1. Get access

Open ORPay → **Developers** → **Request access**. Once an OpenReserve admin
approves your app, create an API key and copy your webhook signing secret.

## 2. Create a checkout (server side)

```js
import { ORPay } from '@openreserve/orpay'

const orpay = new ORPay({ apiKey: process.env.ORPAY_API_KEY, baseUrl: 'https://pay.example.com' })

const checkout = await orpay.createCheckout({
  amount: '12.50',              // decimal string, never a float
  description: 'Ride to Lekki',
  reference: 'ride-981',        // your own order id
  returnUrl: 'https://gabis.app/rides/981',
})
// Redirect the customer to checkout.checkout_url
```

## 3. Handle webhooks

```js
import express from 'express'
import { verifyWebhook } from '@openreserve/orpay'

app.post('/orpay/webhook', express.raw({ type: 'application/json' }), (req, res) => {
  let event
  try {
    event = verifyWebhook(req.body, req.get('ORPay-Signature'), process.env.ORPAY_WEBHOOK_SECRET)
  } catch {
    return res.sendStatus(400)
  }
  if (event.type === 'invoice.paid') markOrderPaid(event.data.reference, event.data.paid_tx)
  res.sendStatus(200)
})
```

Events: `invoice.paid`, `invoice.expired`. Deliveries are retried with backoff
for about a day, so make your handler idempotent (key on `event.data.id`).

## Savings pools

Pools are on-chain and belong to users, not apps. Your app can show a user's
pools read-only via the node API: `GET /v1/accounts/{address}/pools` and
`GET /v1/pools/{id}` (balance, who has paid, who has received, full history).
