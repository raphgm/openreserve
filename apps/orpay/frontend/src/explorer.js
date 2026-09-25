// OpenReserve block explorer: a public, read-only view of the chain. Every
// number here comes straight from a node's API; nothing is cached or edited.
import './style.css'
import { API_BASE, api, formatMoney, isAddress, node, txID } from './orp.js'

const app = document.getElementById('app')
const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c])
const short = (a) => (a ? `${a.slice(0, 8)}…${a.slice(-6)}` : '')
const ago = (ms) => {
  const s = Math.max(0, Math.round((Date.now() - ms) / 1000))
  return s < 60 ? `${s}s ago` : s < 3600 ? `${Math.floor(s / 60)}m ago` : s < 86400 ? `${Math.floor(s / 3600)}h ago` : new Date(ms).toLocaleDateString()
}
const link = (kind, id, text) => `<a class="x-link mono" href="#/${kind}/${esc(id)}">${esc(text ?? short(id))}</a>`
const addr = (a) => (a ? link('address', a) : '—')
const names = new Map()

async function name(a) {
  if (!a || names.has(a)) return names.get(a)
  const r = await api.lookup(a).catch(() => null)
  names.set(a, r?.username ?? null)
  return names.get(a)
}

// A one-line description of any transaction.
function describe(tx) {
  const money = (x) => formatMoney(x, tx.asset ?? '')
  if (tx.pool) return `Pool ${tx.pool.op}${tx.pool.name ? ` “${esc(tx.pool.name)}”` : ''}`
  if (tx.escrow) return `Escrow ${tx.escrow.op}${tx.escrow.ref ? ` #${esc(tx.escrow.ref)}` : ''}`
  if (tx.kind === 'mint') return `Issued ${money(tx.amount)} to ${addr(tx.to)}`
  if (tx.kind === 'burn') return `Redeemed ${money(tx.amount)}`
  return `${money(tx.amount)} to ${addr(tx.to)}`
}

// The header (with the search box) is built once; pages only replace the
// content below it, so auto-refresh never wipes what someone is typing.
function shell(content) {
  if (!document.getElementById('view')) {
    app.innerHTML = `
      <div class="x-top">
        <header class="x-header">
          <a class="logo" href="#/">Explorer</a>
          <form id="search" class="x-search" role="search">
            <svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/></svg>
            <input id="q" placeholder="Search block, transaction, address, pool or escrow" aria-label="Search" autocomplete="off">
          </form>
          <a class="chip" href="/">Open wallet</a>
        </header>
        <div id="hero"></div>
      </div>
      <main class="x-main" id="view"></main>`
    document.getElementById('search').onsubmit = (e) => {
      e.preventDefault()
      const q = document.getElementById('q')
      search(q.value.trim())
      q.blur()
    }
  }
  document.getElementById('view').innerHTML = content
  document.getElementById('hero').innerHTML = hero
  document.body.classList.toggle('x-home', !!hero)
  hero = ''
}

let hero = ''
const icon = {
  height: '<path d="M4 7h16M4 12h16M4 17h10"/>',
  supply: '<circle cx="12" cy="12" r="8"/><path d="M12 8v8M9.5 10.5h4a1.5 1.5 0 0 1 0 3h-3a1.5 1.5 0 0 0 0 3h4"/>',
  naira: '<path d="M7 18V6l10 12V6M5 10.5h14M5 13.5h14"/>',
  accounts: '<circle cx="9" cy="9" r="3.2"/><path d="M3.5 18a5.5 5.5 0 0 1 11 0M16 7.5a3 3 0 0 1 0 5.5M17.5 18a5 5 0 0 0-2-4"/>',
  block: '<rect x="4" y="4" width="16" height="16" rx="3"/><path d="M4 10h16"/>',
  tx: '<path d="M5 9h13l-3-3M19 15H6l3 3"/>',
}
const svg = (name) => `<svg viewBox="0 0 24 24" aria-hidden="true">${icon[name]}</svg>`
const stat = (name, label, value, note) =>
  `<div class="card x-stat"><span class="x-ico">${svg(name)}</span><span class="label">${label}</span><strong>${value}</strong><small>${note}</small></div>`
const empty = (name, title, hint) =>
  `<li class="empty"><span class="x-ico big">${svg(name)}</span><strong>${title}</strong><small>${hint}</small></li>`

async function search(q) {
  if (!q) return
  if (/^\d+$/.test(q)) return go(`block/${q}`)
  if (q.startsWith('@')) {
    const r = await api.resolve(q.slice(1)).catch(() => null)
    return r ? go(`address/${r.address}`) : notFound(`No user ${q}`)
  }
  if (!isAddress(q)) return notFound('Enter a block number, a 64-character id, or an @username.')
  // A 64-hex string can be a tx id, an address, a pool or an escrow: try each.
  if (await node.tx(q).catch(() => null)) return go(`tx/${q}`)
  if (await node.pool(q).catch(() => null)) return go(`pool/${q}`)
  if (await node.escrow(q).catch(() => null)) return go(`escrow/${q}`)
  go(`address/${q}`)
}

const go = (path) => (location.hash = `#/${path}`)
const notFound = (msg) => shell(`<section class="card"><h2>Not found</h2><p class="muted">${esc(msg)}</p></section>`)

let timer
async function home() {
  const st = await node.status()
  const from = Math.max(1, st.height - 14)
  const blocks = st.height ? (await call(`/v1/blocks?from=${from}&limit=15`)).reverse() : []
  const txs = blocks.flatMap((b) => b.txs.map((tx) => ({ tx, height: b.header.height, time: b.header.time }))).slice(0, 12)
  const assets = st.assets ?? []
  hero = `
    <div class="x-hero">
      <span class="x-live"><span class="dot on"></span>${esc(st.chain_id)} · ${st.height ? `block ${st.height.toLocaleString()}` : 'waiting for first block'}</span>
      <h1>Every ajo, escrow and payment, <span>in the open.</span></h1>
      <p>Look up any block, transaction, wallet, savings pool or escrow on OpenReserve.</p>
    </div>`
  shell(`
    <section class="x-stats">
      ${stat('height', 'Block height', st.height.toLocaleString(), st.height ? `last block ${ago(st.tip_time)}` : 'no blocks yet')}
      ${stat('supply', 'ORP supply', formatMoney(st.supply, ''), `${formatMoney(st.burned, '')} burned in fees`)}
      ${assets.map((a) => stat('naira', `${esc(a.name || a.symbol)} issued`, formatMoney(a.supply, a.symbol), `issuer ${short(a.issuer)}`)).join('')}
      ${stat('accounts', 'Accounts', st.accounts.toLocaleString(), `${st.mempool_size} pending transaction${st.mempool_size === 1 ? '' : 's'}`)}
    </section>
    <div class="x-cols">
      <section class="card"><h2>Latest blocks</h2><ul class="x-list">${blocks
        .map((b) => `<li><span class="x-badge">#${b.header.height}</span><span class="who"><strong>${link('block', b.header.height, `Block ${b.header.height}`)}</strong><small>${b.txs.length} tx · ${ago(b.header.time)}</small></span></li>`)
        .join('') || empty('block', 'No blocks yet', 'Blocks appear here as soon as the chain produces them.')}</ul></section>
      <section class="card"><h2>Latest transactions</h2><ul class="x-list">${(await Promise.all(txs.map(txRow))).join('') || empty('tx', 'No transactions yet', 'Payments, ajo contributions and escrows will show up here.')}</ul></section>
    </div>
    <p class="x-foot muted small-text">Chain ${esc(st.chain_id)} · state root <span class="mono">${short(st.state_root)}</span></p>`)
  clearTimeout(timer)
  timer = setTimeout(() => location.hash.length <= 2 && route(), 5000)
}

async function txRow({ tx, height, time }) {
  const txid = await txID(tx)
  return `<li><span class="x-badge tx">tx</span><span class="who"><strong>${link('tx', txid)}</strong><small>${describe(tx)} · block ${height} · ${ago(time)}</small></span></li>`
}

async function call(path) {
  const r = await fetch(API_BASE + path)
  if (!r.ok) throw new Error((await r.json().catch(() => ({}))).error || r.statusText)
  return r.json()
}

async function block(h) {
  const { hash, block: b } = await call(`/v1/blocks/${h}`)
  const rows = await Promise.all(b.txs.map((tx) => txRow({ tx, height: b.header.height, time: b.header.time })))
  shell(`
    <section class="card">
      <div class="section-head"><h2>Block ${b.header.height}</h2>
        <span>${b.header.height > 1 ? link('block', b.header.height - 1, '← prev') : ''} ${link('block', b.header.height + 1, 'next →')}</span></div>
      <dl class="summary x-dl">
        <dt>Time</dt><dd>${new Date(b.header.time).toLocaleString()}</dd>
        <dt>Hash</dt><dd class="mono">${esc(hash)}</dd>
        <dt>Previous</dt><dd class="mono">${esc(b.header.prev_hash)}</dd>
        <dt>State root</dt><dd class="mono">${esc(b.header.state_root)}</dd>
        <dt>Tx root</dt><dd class="mono">${esc(b.header.tx_root)}</dd>
        <dt>Proposer</dt><dd>${addr(b.header.proposer)}</dd>
      </dl>
    </section>
    <section class="card"><h2>${b.txs.length} transaction${b.txs.length === 1 ? '' : 's'}</h2><ul class="x-list">${rows.join('')}</ul></section>`)
}

async function tx(id) {
  const t = await node.tx(id)
  const x = t.tx
  shell(`
    <section class="card">
      <div class="section-head"><h2>Transaction</h2><span class="chip-s ${t.status === 'committed' ? 'ok' : 'warn'}">${t.status}</span></div>
      <p class="x-desc">${describe(x)}</p>
      <dl class="summary x-dl">
        <dt>ID</dt><dd class="mono">${esc(id)}</dd>
        ${t.location ? `<dt>Block</dt><dd>${link('block', t.location.height, String(t.location.height))}</dd>` : ''}
        <dt>From</dt><dd>${addr(x.from)}</dd>
        ${x.to ? `<dt>To</dt><dd>${addr(x.to)}</dd>` : ''}
        <dt>Amount</dt><dd>${formatMoney(x.amount, x.asset ?? '')}</dd>
        <dt>Fee</dt><dd>${formatMoney(x.fee, x.asset ?? '')}</dd>
        <dt>Nonce</dt><dd>${x.nonce}</dd>
        ${x.memo ? `<dt>Memo</dt><dd>${esc(x.memo)}</dd>` : ''}
        ${x.pool ? `<dt>Pool</dt><dd>${link('pool', x.pool.op === 'create' ? id : x.pool.id)}</dd>` : ''}
        ${x.escrow ? `<dt>Escrow</dt><dd>${link('escrow', x.escrow.op === 'create' ? id : x.escrow.id)}</dd>` : ''}
        <dt>Signature</dt><dd class="mono">${short(x.sig)}</dd>
      </dl>
    </section>`)
}

async function address(a) {
  const [acc, hist, pools, escrows, n] = await Promise.all([
    node.account(a), node.history(a), node.pools(a), node.escrows(a), name(a),
  ])
  const assets = Object.entries(acc.assets ?? {})
  const rows = await Promise.all(hist.slice(0, 50).map((e) => txRow({ tx: e.tx, height: e.height, time: e.time })))
  shell(`
    <section class="card">
      <h2>${n ? `@${esc(n)}` : 'Address'}</h2>
      <p class="mono small-text x-wrap">${esc(a)}</p>
      <div class="x-balances">
        <div><span class="label">ORP</span><strong>${formatMoney(acc.balance, '')}</strong></div>
        ${assets.map(([s, v]) => `<div><span class="label">${esc(s)}</span><strong>${formatMoney(v, s)}</strong></div>`).join('')}
        <div><span class="label">Transactions sent</span><strong>${acc.nonce}</strong></div>
      </div>
    </section>
    ${pools.length ? `<section class="card"><h2>Savings pools</h2><ul class="x-list">${pools.map((p) => `<li><span class="x-badge">◎</span><span class="who"><strong>${link('pool', p.id, p.name)}</strong><small>${p.status} · ${p.members.length} members · ${formatMoney(p.contribution, p.asset ?? '')} each</small></span></li>`).join('')}</ul></section>` : ''}
    ${escrows.length ? `<section class="card"><h2>Escrows</h2><ul class="x-list">${escrows.map((e) => `<li><span class="x-badge">⛨</span><span class="who"><strong>${link('escrow', e.id, e.ref ? `#${e.ref}` : short(e.id))}</strong><small>${e.status} · ${formatMoney(e.balance, e.asset ?? '')} locked</small></span></li>`).join('')}</ul></section>` : ''}
    <section class="card"><h2>Transactions</h2><ul class="x-list">${rows.join('') || '<li class="empty">None yet.</li>'}</ul></section>`)
}

async function pool(id) {
  const { pool: p, pot, history } = await node.pool(id)
  const money = (x) => formatMoney(x, p.asset ?? '')
  shell(`
    <section class="card">
      <div class="section-head"><h2>Pool “${esc(p.name)}”</h2><span class="chip-s">${p.status}</span></div>
      <dl class="summary x-dl">
        <dt>Balance</dt><dd>${money(p.balance)}</dd><dt>Pot</dt><dd>${money(pot)}</dd>
        <dt>Contribution</dt><dd>${money(p.contribution)}</dd>
        <dt>Round</dt><dd>${p.status === 'active' ? `${p.round + 1} of ${p.members.length}` : p.status}</dd>
      </dl>
      <ul class="x-list">${p.members.map((m, i) => `<li><span class="x-badge">${i + 1}</span><span class="who"><strong>${addr(m)}</strong><small>${p.claimed[i] ? 'received' : p.status === 'active' && i === p.round ? 'receiving now' : `round ${i + 1}`} · ${p.status === 'active' ? (p.paid[i] ? 'paid this round' : 'not paid') : p.joined[i] ? 'joined' : 'not joined'}</small></span></li>`).join('')}</ul>
    </section>
    <section class="card"><h2>History</h2><ul class="x-list">${(await Promise.all(history.slice().reverse().map((e) => txRow(e)))).join('')}</ul></section>`)
}

async function escrow(id) {
  const { escrow: e, history } = await node.escrow(id)
  const money = (x) => formatMoney(x, e.asset ?? '')
  shell(`
    <section class="card">
      <div class="section-head"><h2>Escrow ${e.ref ? `#${esc(e.ref)}` : ''}</h2><span class="chip-s">${e.status}</span></div>
      <dl class="summary x-dl">
        <dt>Buyer</dt><dd>${addr(e.buyer)}</dd><dt>Seller</dt><dd>${addr(e.seller)}</dd><dt>Arbiter</dt><dd>${addr(e.arbiter)}</dd>
        <dt>Locked</dt><dd>${money(e.balance)}</dd>
        <dt>Milestones</dt><dd>${e.released}/${e.milestones.length} released (${e.milestones.map(money).join(', ')})</dd>
        <dt>Paid to seller</dt><dd>${money(e.paid_seller)}</dd><dt>Returned to buyer</dt><dd>${money(e.paid_buyer)}</dd>
        <dt>Ship by</dt><dd>${new Date(e.ship_by).toLocaleString()}</dd>
        ${e.tracking ? `<dt>Tracking</dt><dd>${esc(e.tracking)}</dd>` : ''}
      </dl>
    </section>
    <section class="card"><h2>History</h2><ul class="x-list">${(await Promise.all(history.slice().reverse().map((h) => txRow(h)))).join('')}</ul></section>`)
}

async function route() {
  const [, kind, id] = location.hash.split('/')
  try {
    if (!kind) return await home()
    const view = { block, tx, address, pool, escrow }[kind]
    if (!view) return notFound('Unknown page.')
    await view(decodeURIComponent(id))
  } catch (err) {
    notFound(err.message)
  }
  window.scrollTo(0, 0)
}

window.addEventListener('hashchange', route)
route()
