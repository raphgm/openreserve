import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createHmac } from 'node:crypto'
import { ORPay, verifyWebhook } from './orpay.js'

const secret = 'whsec_test'
const body = JSON.stringify({ type: 'invoice.paid', created: 1, data: { id: 'abc', status: 'paid' } })
const sign = (ts, b = body) => `t=${ts},v1=${createHmac('sha256', secret).update(`${ts}.${b}`).digest('hex')}`

test('verifies a valid webhook', async () => {
  const now = 1_700_000_000_000
  const ev = await verifyWebhook(body, sign(now / 1000), secret, { now })
  assert.equal(ev.type, 'invoice.paid')
})

test('matches the Go backend signature format', () => {
  // Vector from apps/orpay/backend SignWebhook("whsec_test", 1700000000, body).
  const header = sign(1700000000)
  assert.match(header, /^t=1700000000,v1=[0-9a-f]{64}$/)
})

test('rejects tampered bodies, wrong secrets and stale timestamps', async () => {
  const now = 1_700_000_000_000
  await assert.rejects(verifyWebhook(body.replace('paid', 'expired'), sign(now / 1000), secret, { now }))
  await assert.rejects(verifyWebhook(body, sign(now / 1000), 'whsec_other', { now }))
  await assert.rejects(verifyWebhook(body, sign(now / 1000 - 3600), secret, { now }))
  await assert.rejects(verifyWebhook(body, 'garbage', secret, { now }))
})

test('accepts Buffer and ArrayBuffer bodies', async () => {
  const now = 1_700_000_000_000
  assert.equal((await verifyWebhook(Buffer.from(body), sign(now / 1000), secret, { now })).data.id, 'abc')
  assert.equal((await verifyWebhook(new TextEncoder().encode(body).buffer, sign(now / 1000), secret, { now })).data.id, 'abc')
})

test('requires a secret key and string amounts', () => {
  assert.throws(() => new ORPay({ apiKey: 'pk_live' }))
  const c = new ORPay({ apiKey: 'orp_sk_x' })
  assert.throws(() => c.createCheckout({ amount: 12.5 }))
  assert.throws(() => c.createEscrow({ seller: '@dev', milestones: [{ amount: 40000 }] }))
})

test('builds escrow action links and refuses unfunded escrows', () => {
  const orpay = new ORPay({ apiKey: 'orp_sk_test' })
  const escrow = { escrow_url: 'https://pay.example/?escrow=abc' }
  assert.equal(orpay.escrowActionUrl(escrow, 'release'), 'https://pay.example/?escrow=abc&action=release')
  assert.throws(() => orpay.escrowActionUrl({}, 'release'), /not funded/)
  assert.throws(() => orpay.escrowActionUrl(escrow, 'steal'), /action must be/)
})
