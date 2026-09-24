// Minimal OpenReserve client: keys, amounts, transaction signing and the node API.
// The signing encoding must match node/types.Tx.SignBytes exactly.
import * as ed from '@noble/ed25519'

export const UNIT = 1_000_000n

const enc = new TextEncoder()

export const toHex = (bytes) => Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')

export function fromHex(hex) {
  if (!/^([0-9a-f]{2})*$/.test(hex)) throw new Error('invalid hex')
  return Uint8Array.from(hex.match(/../g) ?? [], (h) => parseInt(h, 16))
}

export function isAddress(s) {
  return /^[0-9a-f]{64}$/.test(s)
}

// Amounts are integer micro-ORP, handled as BigInt to avoid float rounding.
export function parseAmount(s) {
  const m = /^(\d+)(?:\.(\d{1,6}))?$/.exec(s.trim())
  if (!m) throw new Error('Enter an amount like 12 or 12.5 (max 6 decimals)')
  const micro = BigInt(m[1]) * UNIT + BigInt((m[2] ?? '').padEnd(6, '0'))
  if (micro > BigInt(Number.MAX_SAFE_INTEGER)) throw new Error('Amount too large')
  return micro
}

export function formatAmount(micro) {
  const v = BigInt(micro)
  const frac = (v % UNIT).toString().padStart(6, '0').replace(/0+$/, '')
  const whole = (v / UNIT).toLocaleString('en-US')
  return frac ? `${whole}.${frac}` : whole
}

class Encoder {
  parts = []
  str(s) {
    const b = enc.encode(s)
    this.u32(b.length)
    this.parts.push(b)
  }
  u32(n) {
    const b = new Uint8Array(4)
    new DataView(b.buffer).setUint32(0, n)
    this.parts.push(b)
  }
  u64(n) {
    const b = new Uint8Array(8)
    new DataView(b.buffer).setBigUint64(0, BigInt(n))
    this.parts.push(b)
  }
  bytes() {
    const out = new Uint8Array(this.parts.reduce((n, p) => n + p.length, 0))
    let o = 0
    for (const p of this.parts) {
      out.set(p, o)
      o += p.length
    }
    return out
  }
}

export function txSignBytes(tx) {
  const e = new Encoder()
  e.str('openreserve/tx/v1')
  e.str(tx.chain_id)
  e.str(tx.from)
  e.str(tx.to)
  e.u64(tx.amount)
  e.u64(tx.fee)
  e.u64(tx.nonce)
  e.str(tx.memo ?? '')
  return e.bytes()
}

export async function txID(tx) {
  return toHex(new Uint8Array(await crypto.subtle.digest('SHA-256', txSignBytes(tx))))
}

export async function newSeed() {
  return ed.utils.randomPrivateKey()
}

export async function addressOf(seed) {
  return toHex(await ed.getPublicKeyAsync(seed))
}

export async function signTx(tx, seed) {
  return { ...tx, sig: toHex(await ed.signAsync(txSignBytes(tx), seed)) }
}

export async function signMessage(message, seed) {
  return toHex(await ed.signAsync(enc.encode(message), seed))
}

// Node API. Paths are relative so the dev server can proxy them.
async function call(path, opts) {
  const res = await fetch(path, opts)
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(body.error || `Request failed (${res.status})`)
  return body
}

export const node = {
  status: () => call('/v1/status'),
  account: (addr) => call(`/v1/accounts/${addr}`),
  history: (addr) => call(`/v1/accounts/${addr}/txs?limit=100`),
  tx: (id) => call(`/v1/txs/${id}`),
  submit: (tx) =>
    call('/v1/txs', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ...tx, amount: Number(tx.amount), fee: Number(tx.fee) }),
    }),
}

// Build, sign and submit a transfer. Returns the tx id.
export async function send({ seed, to, amount, memo = '' }) {
  const from = await addressOf(seed)
  const [st, acc] = await Promise.all([node.status(), node.account(from)])
  const tx = await signTx(
    { chain_id: st.chain_id, from, to, amount, fee: BigInt(st.min_fee), nonce: acc.next_nonce, memo },
    seed,
  )
  const res = await node.submit(tx)
  return res.id
}

export async function waitForCommit(id, timeoutMs = 20_000) {
  const end = Date.now() + timeoutMs
  while (Date.now() < end) {
    const t = await node.tx(id).catch(() => null)
    if (t?.status === 'committed') return t
    await new Promise((r) => setTimeout(r, 500))
  }
  throw new Error('Timed out waiting for confirmation')
}

export const api = {
  resolve: (username) => call(`/api/users/${encodeURIComponent(username)}`),
  lookup: (addr) => call(`/api/addresses/${addr}`),
  register: (body) =>
    call('/api/users', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }),
  faucet: (address) =>
    call('/api/faucet', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ address }) }),
  config: () => call('/api/config'),
}

export function registerMessage(username, address) {
  return `orpay/register/v1\n${username}\n${address}`
}
