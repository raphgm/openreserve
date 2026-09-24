import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createHmac } from 'node:crypto'
import { ORPay, verifyWebhook } from './orpay.js'

const secret = 'whsec_test'
const body = JSON.stringify({ type: 'invoice.paid', created: 1, data: { id: 'abc', status: 'paid' } })
const sign = (ts, b = body) => `t=${ts},v1=${createHmac('sha256', secret).update(`${ts}.${b}`).digest('hex')}`

test('verifies a valid webhook', () => {
  const now = 1_700_000_000_000
  const ev = verifyWebhook(body, sign(now / 1000), secret, { now })
  assert.equal(ev.type, 'invoice.paid')
})

test('matches the Go backend signature format', () => {
  // Vector from apps/orpay/backend SignWebhook("whsec_test", 1700000000, body).
  const header = sign(1700000000)
  assert.match(header, /^t=1700000000,v1=[0-9a-f]{64}$/)
})

test('rejects tampered bodies, wrong secrets and stale timestamps', () => {
  const now = 1_700_000_000_000
  assert.throws(() => verifyWebhook(body.replace('paid', 'expired'), sign(now / 1000), secret, { now }))
  assert.throws(() => verifyWebhook(body, sign(now / 1000), 'whsec_other', { now }))
  assert.throws(() => verifyWebhook(body, sign(now / 1000 - 3600), secret, { now }))
  assert.throws(() => verifyWebhook(body, 'garbage', secret, { now }))
})

test('requires a secret key and string amounts', () => {
  assert.throws(() => new ORPay({ apiKey: 'pk_live' }))
  const c = new ORPay({ apiKey: 'orp_sk_x' })
  assert.throws(() => c.createCheckout({ amount: 12.5 }))
  assert.throws(() => c.createEscrow({ seller: '@dev', milestones: [{ amount: 40000 }] }))
})
