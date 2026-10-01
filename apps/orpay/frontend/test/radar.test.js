// Unit tests for the scam radar (src/radar.js). Run: npm test
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { asksToPayOutside, riskOf } from '../src/radar.js'

test('flags new and risky accounts, not good ones', () => {
  assert.equal(riskOf({ since: 0, escrow: {}, ajo: {} }).level, 'caution')
  assert.equal(riskOf({ since: Date.now() - 90 * 86_400_000, escrow: { disputes: 3 }, ajo: {} }).level, 'high')
  assert.equal(riskOf({ since: Date.now() - 90 * 86_400_000, escrow: { sold_completed: 9 }, ajo: { rounds_missed: 0 } }), null)
})

test('spots requests to pay outside escrow', () => {
  assert.ok(asksToPayOutside('Just transfer directly to my Opay 8031234567'))
  assert.ok(asksToPayOutside('cancel the escrow and pay cash'))
  assert.ok(!asksToPayOutside('Delivered this morning, please check the screen'))
})
