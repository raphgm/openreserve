// Unit tests for QR scan parsing (src/scan.js). Run: npm test
import { test } from 'node:test'
import assert from 'node:assert/strict'

globalThis.location = { origin: 'https://pay.example', search: '' }
const { parseScan } = await import('../src/scan.js')

test('reads ORPay QR codes and pay links', () => {
  const addr = 'ab'.repeat(32)
  assert.deepEqual(parseScan(addr), { to: addr })
  assert.deepEqual(parseScan(addr.toUpperCase()), { to: addr })
  assert.deepEqual(parseScan('@Tunde_9'), { to: 'tunde_9' })
  assert.deepEqual(parseScan('https://pay.example/?to=ada&amount=500&memo=rent'), { to: 'ada', amount: '500', memo: 'rent' })
})

test('ignores QR codes that are not payments', () => {
  assert.equal(parseScan('https://example.com/menu'), null)
  assert.equal(parseScan('hello world'), null)
  assert.equal(parseScan(''), null)
})
