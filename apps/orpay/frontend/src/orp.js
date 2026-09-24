// Minimal OpenReserve client: keys, amounts, transaction signing and the node API.
// The signing encoding must match node/types.Tx.SignBytes exactly.
import * as ed from '@noble/ed25519'
import { entropyToMnemonic, mnemonicToEntropy, validateMnemonic } from '@scure/bip39'
import { wordlist } from '@scure/bip39/wordlists/english'

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

// Currency display: NGN as naira with 2 decimals, ORP as a plain decimal.
export function formatMoney(micro, asset = '') {
  if (asset === 'NGN') {
    const kobo = BigInt(micro) / 10_000n
    const naira = Number(kobo) / 100
    return '₦' + naira.toLocaleString('en-NG', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
  }
  return `${formatAmount(micro)} ${asset || 'ORP'}`
}

export const assetLabel = (asset) => (asset === 'NGN' ? 'Naira (₦)' : asset || 'ORP')

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

// Plain transfers use the v1 encoding; pool operations use v2 with the pool
// fields appended (see node/types.Tx.SignBytes).
export function txSignBytes(tx) {
  const e = new Encoder()
  e.str(tx.pool || tx.kind || tx.asset || tx.escrow ? 'openreserve/tx/v2' : 'openreserve/tx/v1')
  e.str(tx.chain_id)
  e.str(tx.from)
  e.str(tx.to ?? '')
  e.u64(tx.amount)
  e.u64(tx.fee)
  e.u64(tx.nonce)
  e.str(tx.memo ?? '')
  const v2 = tx.pool || tx.kind || tx.asset || tx.escrow
  if (!v2) return e.bytes()
  e.str(tx.kind ?? '')
  e.str(tx.asset ?? '')
  if (tx.pool) {
    e.parts.push(new Uint8Array([1]))
    const p = tx.pool
    e.str(p.op)
    e.parts.push(p.id ? fromHex(p.id) : new Uint8Array(32))
    e.str(p.name ?? '')
    const members = p.members ?? []
    e.u64(members.length)
    for (const m of members) e.str(m)
    e.u64(p.contribution ?? 0)
  } else {
    e.parts.push(new Uint8Array([0]))
  }
  if (tx.escrow) {
    const x = tx.escrow
    e.parts.push(new Uint8Array([2]))
    e.str(x.op)
    e.parts.push(x.id ? fromHex(x.id) : new Uint8Array(32))
    e.str(x.seller ?? '')
    e.str(x.arbiter ?? '')
    const ms = x.milestones ?? []
    e.u64(ms.length)
    for (const m of ms) e.u64(m)
    e.u64(x.ship_by ?? 0)
    e.u64(x.review_secs ?? 0)
    e.str(x.ref ?? '')
    e.u64(x.to_seller ?? 0)
  }
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

// API origin. Empty on the web (same-origin paths, proxied in development);
// the native app is built with VITE_API_BASE=https://<your domain>.
export const API_BASE = (import.meta.env?.VITE_API_BASE ?? '').replace(/\/$/, '')

// Node API.
async function call(path, opts) {
  const res = await fetch(API_BASE + path, opts)
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(body.error || `Request failed (${res.status})`)
  return body
}

export const node = {
  status: () => call('/v1/status'),
  account: (addr) => call(`/v1/accounts/${addr}`),
  history: (addr) => call(`/v1/accounts/${addr}/txs?limit=100`),
  tx: (id) => call(`/v1/txs/${id}`),
  pool: (id) => call(`/v1/pools/${id}`),
  escrow: (id) => call(`/v1/escrows/${id}`),
  escrows: (addr) => call(`/v1/accounts/${addr}/escrows`),
  pools: (addr) => call(`/v1/accounts/${addr}/pools`),
  submit: (tx) => {
    const body = { ...tx, amount: Number(tx.amount), fee: Number(tx.fee) }
    if (!body.kind) delete body.kind
    if (!body.asset) delete body.asset
    if (tx.pool || tx.escrow || tx.kind === 'burn') if (!body.to) delete body.to
    if (tx.escrow) {
      const x = { ...tx.escrow }
      if (x.milestones) x.milestones = x.milestones.map(Number)
      if (x.to_seller != null) x.to_seller = Number(x.to_seller)
      for (const k of Object.keys(x)) if (x[k] == null || x[k] === '' || x[k] === 0 || x[k] === 0n) delete x[k]
      body.escrow = x
    }
    if (tx.pool) {
      body.pool = { ...tx.pool }
      if (body.pool.contribution != null) body.pool.contribution = Number(body.pool.contribution)
      for (const k of Object.keys(body.pool)) if (body.pool[k] == null || body.pool[k] === '') delete body.pool[k]
      if (!body.to) delete body.to
    }
    return call('/v1/txs', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
  },
}

// Sign and submit an escrow operation. For "create" the returned tx id is
// also the new escrow's id. Steps after create carry no fee.
export async function escrowOp({ seed, op, id, seller, arbiter, milestones, shipBy, reviewSecs, ref, toSeller, memo = '', asset = '' }) {
  const from = await addressOf(seed)
  const [st, acc] = await Promise.all([node.status(), node.account(from)])
  const escrow = { op }
  if (id) escrow.id = id
  if (op === 'create') Object.assign(escrow, { seller, arbiter, milestones: milestones.map(BigInt), ship_by: shipBy, review_secs: reviewSecs, ref: ref ?? '' })
  if (op === 'resolve') escrow.to_seller = BigInt(toSeller ?? 0)
  const tx = await signTx(
    { chain_id: st.chain_id, from, to: '', amount: 0n, fee: op === 'create' ? feeFor(st, asset) : 0n, nonce: acc.next_nonce, memo, escrow, ...(asset ? { asset } : {}) },
    seed,
  )
  return (await node.submit(tx)).id
}

// Sign and submit a savings-pool operation. For "create" the returned tx id
// is also the new pool's id.
export async function poolOp({ seed, op, id, name, members, contribution, asset = '' }) {
  const from = await addressOf(seed)
  const [st, acc] = await Promise.all([node.status(), node.account(from)])
  const pool = { op }
  if (id) pool.id = id
  if (op === 'create') Object.assign(pool, { name, members, contribution: BigInt(contribution) })
  const tx = await signTx(
    { chain_id: st.chain_id, from, to: '', amount: 0n, fee: feeFor(st, asset), nonce: acc.next_nonce, memo: '', pool, ...(asset ? { asset } : {}) },
    seed,
  )
  return (await node.submit(tx)).id
}

// Build, sign and submit a transfer. Returns the tx id.
export function feeFor(status, asset = '') {
  if (!asset) return BigInt(status.min_fee)
  const a = (status.assets ?? []).find((x) => x.symbol === asset)
  return BigInt(a?.min_fee ?? 0)
}

export async function send({ seed, to, amount, memo = '', asset = '', kind = '' }) {
  const from = await addressOf(seed)
  const [st, acc] = await Promise.all([node.status(), node.account(from)])
  const tx = { chain_id: st.chain_id, from, to, amount, fee: feeFor(st, asset), nonce: acc.next_nonce, memo }
  if (asset) tx.asset = asset
  if (kind) tx.kind = kind
  const signed = await signTx(tx, seed)
  const res = await node.submit(signed)
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

// Signed request to the ORPay backend: proves the caller controls the wallet.
async function signedCall(seed, method, path, body) {
  const data = body === undefined ? '' : JSON.stringify(body)
  const ts = Date.now()
  const hash = toHex(new Uint8Array(await crypto.subtle.digest('SHA-256', enc.encode(data))))
  const msg = `orpay/req/v1\n${method}\n${path}\n${ts}\n${hash}`
  return call(path, {
    method,
    headers: {
      'Content-Type': 'application/json',
      'X-ORP-Address': await addressOf(seed),
      'X-ORP-Time': String(ts),
      'X-ORP-Sig': await signMessage(msg, seed),
    },
    body: data || undefined,
  })
}

export const api = {
  escrowRequest: (id) => call(`/api/escrow-requests/${encodeURIComponent(id)}`),
  invoice: (id) => call(`/api/invoices/${encodeURIComponent(id)}`),
  apps: (seed) => signedCall(seed, 'GET', '/api/apps'),
  requestApp: (seed, body) => signedCall(seed, 'POST', '/api/apps', body),
  reviewApp: (seed, id, decision, note = '') => signedCall(seed, 'POST', `/api/apps/${id}/review`, { decision, note }),
  rotateKey: (seed, id) => signedCall(seed, 'POST', `/api/apps/${id}/keys`),
  updateApp: (seed, id, body) => signedCall(seed, 'POST', `/api/apps/${id}/settings`, body),
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

// Recovery words: the 32-byte seed as 24 BIP39 words (same as `orctl words`).
export const seedToWords = (seed) => entropyToMnemonic(seed, wordlist)

export function wordsToSeed(phrase) {
  const words = phrase.trim().toLowerCase().split(/\s+/)
  if (words.length !== 24) throw new Error(`Enter all 24 words (you entered ${words.length}).`)
  const bad = words.findIndex((w) => !wordlist.includes(w))
  if (bad >= 0) throw new Error(`Word ${bad + 1} ("${words[bad]}") isn't a recovery word. Check the spelling.`)
  const joined = words.join(' ')
  if (!validateMnemonic(joined, wordlist)) throw new Error("These words don't form a valid key. Check their order.")
  return mnemonicToEntropy(joined, wordlist)
}

// Pay links: https://<orpay>/?to=bob&amount=5&memo=Lunch
export function payLink({ to, amount, memo }) {
  const u = new URL(location.origin + '/')
  u.searchParams.set('to', to)
  if (amount) u.searchParams.set('amount', amount)
  if (memo) u.searchParams.set('memo', memo)
  return u.toString()
}

export function readPayLink(search = location.search) {
  const q = new URLSearchParams(search)
  const to = q.get('to')
  if (!to) return null
  return { to, amount: q.get('amount') ?? '', memo: (q.get('memo') ?? '').slice(0, 140) }
}

// Paystack gateway: add naira, withdraw to a bank, pay checkouts by card.
export const gateway = {
  config: () => call('/pay/config'),
  deposit: (seed, amount, email, provider) => signedCall(seed, 'POST', '/pay/deposits', { amount, email, provider }),
  deposit_status: (ref) => call(`/pay/deposits/${encodeURIComponent(ref)}`),
  banks: (provider = '') => call(`/pay/banks${provider ? `?provider=${encodeURIComponent(provider)}` : ''}`),
  resolve: (account_number, bank_code, provider = '') =>
    call('/pay/banks/resolve', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ account_number, bank_code, provider }) }),
  withdraw: (seed, body) => signedCall(seed, 'POST', '/pay/withdrawals', body),
  withdrawals: (seed) => signedCall(seed, 'GET', '/pay/withdrawals'),
  cardPay: (invoice, email, provider = '') =>
    call(`/pay/invoices/${encodeURIComponent(invoice)}/card`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email, provider }) }),
}

export const providerLabel = (p) => ({ paystack: 'Paystack', flutterwave: 'Flutterwave' })[p] ?? p
