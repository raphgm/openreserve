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

## Escrow: hold payouts until work is done

```js
const escrow = await orpay.createEscrow({
  seller: '@developer',                 // who gets paid (username or address)
  currency: 'NGN',
  description: 'Landing page build',
  milestones: [
    { label: 'Design approved', amount: '40000' },
    { label: 'Site delivered', amount: '60000' },
  ],
  shipByDays: 14,   // client can reclaim if nothing is delivered in time
  reviewDays: 3,    // developer can claim if the client goes silent after delivery
  reference: 'job-17',
})
// Send the client to escrow.funding_url.
```

The client locks the funds on-chain from their own wallet; neither your app
nor ORPay holds them. The client releases each milestone; either side can
open a dispute, which your app's arbiter wallet settles in ORPay. Webhooks:
`escrow.funded`, `escrow.dispatched`, `escrow.milestone_released`,
`escrow.disputed`, `escrow.completed`, `escrow.refunded`, `escrow.resolved`,
`escrow.expired`.

## Savings pools

Pools are on-chain and belong to users, not apps. Your app can show a user's
pools read-only via the node API: `GET /v1/accounts/{address}/pools` and
`GET /v1/pools/{id}` (balance, who has paid, who has received, full history).
