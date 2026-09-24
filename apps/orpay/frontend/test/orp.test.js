// Unit tests for the wallet SDK (src/orp.js). Run: npm test
import { test } from 'node:test'
import assert from 'node:assert/strict'

globalThis.location = { origin: 'https://pay.example', search: '' }
const orp = await import('../src/orp.js')

const FROM = '1'.repeat(64)
const TO = '2'.repeat(64)
const ID = 'ab' + '0'.repeat(62)

// Must match node/types/vectors_test.go byte for byte.
const vectors = {
  transfer: [{ chain_id: 'c1', from: FROM, to: TO, amount: 1_500_000n, fee: 1000n, nonce: 7, memo: 'lunch ✓' },
    '5efb3fa58edd543e91f41e4d61c7355aa1cf90f6beb0478432ed538ad69d2bc5'],
  ngn: [{ chain_id: 'c1', from: FROM, to: TO, amount: 50_000_000n, fee: 10_000n, nonce: 1, asset: 'NGN' },
    'b3a15dfb5df2c960d3493acb5a7073761280bb0966322447e2da225697b78a6e'],
  burn: [{ chain_id: 'c1', from: FROM, to: '', amount: 20_000_000n, fee: 10_000n, nonce: 2, kind: 'burn', asset: 'NGN', memo: 'wd:abc' },
    '8ca4d14556866aa578420a5db2deabd0c3392543e4ef00a9370f88488cc40da7'],
  pool: [{ chain_id: 'c1', from: FROM, to: '', amount: 0n, fee: 1000n, nonce: 3, pool: { op: 'create', name: 'Ajo', members: [FROM, TO], contribution: 10_000_000n } },
    '36f569c0bb0fc3e642bcc2b5a0885c3282d5f35a4b188292c93a53c3df191e0c'],
  pooljoin: [{ chain_id: 'c1', from: TO, to: '', amount: 0n, fee: 1000n, nonce: 0, pool: { op: 'join', id: ID } },
    '82ebaedcce1ec1bbfc48bf170294f7a95c02913657098321586997bc922aedf7'],
  escrow: [{ chain_id: 'c1', from: FROM, to: '', amount: 0n, fee: 1000n, nonce: 4, asset: 'NGN', escrow: {
    op: 'create', seller: TO, arbiter: '3'.repeat(64), milestones: [40_000_000n, 60_000_000n], ship_by: 1_800_000_000_000, review_secs: 259200, ref: 'job-17' } },
    '75e8db01e8d1f033d182d1488505431e3f35f6bc858e5c0e722e10d13d7dfbb7'],
  resolve: [{ chain_id: 'c1', from: TO, to: '', amount: 0n, fee: 0n, nonce: 5, escrow: { op: 'resolve', id: ID, to_seller: 5_000_000n } },
    '690b1de96de2bb17c07bd8dda70bd9b7d40180218126757ab81b661672b3b25d'],
}

for (const [name, [tx, want]] of Object.entries(vectors)) {
  test(`signing bytes match the Go node: ${name}`, async () => {
    assert.equal(await orp.txID(tx), want)
  })
}

test('amounts parse and format without float errors', () => {
  assert.equal(orp.parseAmount('12.5'), 12_500_000n)
  assert.equal(orp.parseAmount('0.000001'), 1n)
  assert.equal(orp.formatAmount(orp.parseAmount('0.1') + orp.parseAmount('0.2')), '0.3')
  for (const bad of ['', '1.', '.5', '1.2345678', '-1', '1e3', 'abc']) assert.throws(() => orp.parseAmount(bad), bad)
  assert.equal(orp.formatMoney(2_500_000_000n, 'NGN'), '₦2,500.00')
  assert.equal(orp.formatMoney(1_500_000n, ''), '1.5 ORP')
})

test('recovery words round-trip and catch mistakes', async () => {
  const seed = await orp.newSeed()
  const words = orp.seedToWords(seed)
  assert.equal(words.split(' ').length, 24)
  assert.deepEqual(orp.wordsToSeed(words.toUpperCase()), seed)
  const ws = words.split(' ')
  ;[ws[0], ws[1]] = [ws[1], ws[0]]
  if (ws[0] !== ws[1]) assert.throws(() => orp.wordsToSeed(ws.join(' ')), /order|valid/)
  assert.throws(() => orp.wordsToSeed('abandon abandon'), /24 words/)
  // Matches the Go mnemonic package vector.
  const goVector = 'abandon debris luxury debate crawl blush thought trip essence people sudden spray alter satisfy bike mystery one ask cloud hope sword grass enter certain'
  assert.deepEqual([...orp.wordsToSeed(goVector)], Array.from({ length: 32 }, (_, i) => (i * 7) & 255))
})

test('signatures verify and addresses derive from the seed', async () => {
  const ed = await import('@noble/ed25519')
  const seed = await orp.newSeed()
  const addr = await orp.addressOf(seed)
  assert.ok(orp.isAddress(addr))
  const tx = await orp.signTx({ chain_id: 'c1', from: addr, to: TO, amount: 1n, fee: 1000n, nonce: 0 }, seed)
  assert.ok(await ed.verifyAsync(orp.fromHex(tx.sig), orp.txSignBytes(tx), orp.fromHex(addr)))
})

test('pay links round-trip', () => {
  const url = orp.payLink({ to: 'ada', amount: '5000', memo: 'Rent' })
  assert.equal(url, 'https://pay.example/?to=ada&amount=5000&memo=Rent')
  assert.deepEqual(orp.readPayLink(new URL(url).search), { to: 'ada', amount: '5000', memo: 'Rent' })
  assert.equal(orp.readPayLink('?amount=5'), null)
})
