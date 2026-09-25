import './style.css'
import {
  addressOf, api, assetLabel, partnerMark, feeFor, formatAmount, formatMoney, fromHex, gateway, isAddress, newSeed, node, parseAmount,
  payLink, readPayLink, registerMessage, seedToWords, send, signMessage, waitForCommit, wordsToSeed,
} from './orp.js'
import { renderSVG } from 'uqr'
import { duePools, initPools, renderInvite, renderPool, renderPools } from './pools.js'
import { initCheckout, renderCheckout } from './checkout.js'
import { initDevelopers, renderDevelopers } from './developers.js'
import { confirmDeposit, initMoney, renderAddMoney, renderWithdraw } from './money.js'
import { initEscrow, renderEscrow, renderEscrows, renderFundRequest } from './escrow.js'
import { renderLanding } from './landing.js'
import { clearVault, hasVault, saveVault, unlockVault, vaultAddress } from './vault.js'

const app = document.getElementById('app')

const state = {
  seed: null,
  address: null,
  username: null,
  account: null,
  history: [],
  status: null,
  config: { faucet: false },
  names: new Map(), // address -> username cache
  tab: 'send',
  view: 'home',
  online: true,
  seen: null, // tx ids already shown, for incoming-payment notifications
}

// Deep links opened before unlocking are kept until the wallet opens:
// pay links (?to=...), partner checkouts (?invoice=...) and pools (?pool=...).
const PENDING = 'orpay.pending'
{
  const q = new URLSearchParams(location.search)
  const req = q.get('deposit')
    ? { deposit: q.get('deposit') }
    : q.get('invoice')
    ? { invoice: q.get('invoice'), card: q.get('card') }
    : q.get('pool')
      ? { pool: q.get('pool') }
      : q.get('escrow')
      ? { escrow: q.get('escrow') }
      : q.get('escrow_request')
      ? { escrow_request: q.get('escrow_request') }
      : q.get('ajo_invite')
      ? { ajo_invite: q.get('ajo_invite') }
      : readPayLink()
  if (req) {
    sessionStorage.setItem(PENDING, JSON.stringify(req))
    history.replaceState(null, '', location.pathname)
  }
}

const esc = (s) =>
  String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c])
const short = (a) => `${a.slice(0, 6)}…${a.slice(-4)}`
const $ = (sel) => app.querySelector(sel)

function toast(msg, kind = 'ok') {
  const t = document.getElementById('toast')
  t.textContent = msg
  t.className = `show ${kind}`
  clearTimeout(toast.timer)
  toast.timer = setTimeout(() => (t.className = ''), 3500)
}

// ---------- onboarding ----------

function renderWelcome() {
  app.innerHTML = `
    <main class="narrow">
      <div class="brand"><span class="logo" aria-label="ORPay"><span class="logo-pay">Pay</span></span></div>
      <h1>Money that moves like messages.</h1>
      <p class="muted">Send ORP to anyone by @username. Your keys stay on this device.</p>
      <div class="stack">
        <button class="primary" id="create">Create a wallet</button>
        <button class="ghost" id="import">Restore with recovery words</button>
      </div>
    </main>`
  $('#create').onclick = async () => showBackup(await newSeed())
  $('#import').onclick = renderImport
}

function wordGrid(seed) {
  return `<ol class="words">${seedToWords(seed).split(' ').map((w) => `<li>${w}</li>`).join('')}</ol>`
}

function showBackup(seed) {
  app.innerHTML = `
    <main class="narrow">
      <button class="link back" id="back">← Back</button>
      <h1>Write down your recovery words</h1>
      <p class="muted">These 24 words are the only way to restore your wallet. Anyone who has them can spend your ORP. Write them on paper, in order, and keep them offline.</p>
      ${wordGrid(seed)}
      <label class="check"><input type="checkbox" id="saved"> I wrote down all 24 words</label>
      <button class="primary" id="next" disabled>Continue</button>
    </main>`
  $('#back').onclick = renderWelcome
  $('#saved').onchange = (e) => ($('#next').disabled = !e.target.checked)
  $('#next').onclick = () => renderSetPassword(seed)
}

function renderImport() {
  app.innerHTML = `
    <main class="narrow">
      <button class="link back" id="back">← Back</button>
      <h1>Restore wallet</h1>
      <form id="f" class="stack">
        <label>Recovery words<textarea id="key" rows="4" spellcheck="false" autocomplete="off" autocapitalize="none" placeholder="24 words separated by spaces"></textarea></label>
        <p class="error" id="err"></p>
        <button class="primary">Continue</button>
      </form>
    </main>`
  $('#back').onclick = renderWelcome
  $('#f').onsubmit = (e) => {
    e.preventDefault()
    const v = $('#key').value.trim().toLowerCase()
    try {
      // Older wallets exported a 64-character hex key; still accept it.
      renderSetPassword(/^[0-9a-f]{64}$/.test(v) ? fromHex(v) : wordsToSeed(v))
    } catch (err) {
      $('#err').textContent = err.message
    }
  }
}

function renderSetPassword(seed) {
  app.innerHTML = `
    <main class="narrow">
      <h1>Set a password</h1>
      <p class="muted">Encrypts your key on this device. You'll enter it each time you open ORPay.</p>
      <form id="f" class="stack">
        <label>Password<input type="password" id="p1" autocomplete="new-password" minlength="8" required></label>
        <label>Confirm password<input type="password" id="p2" autocomplete="new-password" required></label>
        <p class="error" id="err"></p>
        <button class="primary" id="go">Open wallet</button>
      </form>
    </main>`
  $('#f').onsubmit = async (e) => {
    e.preventDefault()
    if ($('#p1').value !== $('#p2').value) return ($('#err').textContent = 'Passwords do not match.')
    $('#go').disabled = true
    const address = await addressOf(seed)
    await saveVault(seed, address, $('#p1').value)
    openWallet(seed, address)
  }
}

function renderUnlock() {
  const addr = vaultAddress()
  app.innerHTML = `
    <main class="narrow">
      <div class="brand"><span class="logo" aria-label="ORPay"><span class="logo-pay">Pay</span></span></div>
      <h1>Welcome back</h1>
      ${addr ? `<div class="who-chip"><span class="avatar" style="--hue:${parseInt(addr.slice(0, 4), 16) % 360}">${addr.slice(0, 2).toUpperCase()}</span><span><small>Your wallet</small><span class="mono">${short(addr)}</span></span></div>` : ''}
      <form id="f" class="stack">
        <label>Password
          <span class="pw-field"><input type="password" id="pw" autocomplete="current-password" autofocus required placeholder="Enter your password">
          <button type="button" class="pw-eye" id="eye" aria-label="Show password">Show</button></span>
        </label>
        <p class="error" id="err"></p>
        <button class="primary" id="go">Unlock</button>
      </form>
      <div class="unlock-links">
        <button class="link" id="restore">Forgot password?</button>
        <button class="link muted-link" id="forget">Use a different wallet</button>
      </div>
      <p class="trust"><svg viewBox="0 0 24 24" aria-hidden="true"><rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/></svg>Your keys are encrypted and never leave this device.</p>
    </main>`
  $('#f').onsubmit = async (e) => {
    e.preventDefault()
    $('#go').disabled = true
    try {
      const seed = await unlockVault($('#pw').value)
      openWallet(seed, await addressOf(seed))
    } catch (err) {
      $('#err').textContent = err.message
      $('#go').disabled = false
    }
  }
  $('#eye').onclick = () => {
    const pw = $('#pw'), show = pw.type === 'password'
    pw.type = show ? 'text' : 'password'
    $('#eye').textContent = show ? 'Hide' : 'Show'
    $('#eye').setAttribute('aria-label', show ? 'Hide password' : 'Show password')
  }
  $('#restore').onclick = renderImport
  $('#forget').onclick = () => {
    if (confirm('Remove this wallet from this device? You can only get it back with its recovery key.')) {
      clearVault()
      renderWelcome()
    }
  }
}

// ---------- wallet ----------

async function openWallet(seed, address) {
  state.seed = seed
  state.address = address
  renderWallet()
  api.config().then((c) => ((state.config = c), renderBalance())).catch(() => {})
  gateway
    .config()
    .then((g) => {
      state.gateway = g
      renderBalance()
      if (state.view === 'home' && state.tab === 'send' && !$('#panel-card')?.hidden && !$('#to')?.value) renderSend()
    })
    .catch(() => {})
  api.lookup(address).then((r) => ((state.username = r.username), renderHeader())).catch(() => {})
  refresh()
  setInterval(refresh, 2000)
  const pending = sessionStorage.getItem(PENDING)
  if (pending) {
    sessionStorage.removeItem(PENDING)
    try {
      const req = JSON.parse(pending)
      if (req.deposit) showView('money', () => confirmDeposit(req.deposit))
      else if (req.invoice) showView('checkout', () => renderCheckout(req.invoice, req.card))
      else if (req.pool) showView('pools', () => renderPool(req.pool))
      else if (req.escrow) showView('escrow', () => renderEscrow(req.escrow))
      else if (req.escrow_request) showView('escrow', () => renderFundRequest(req.escrow_request))
      else if (req.ajo_invite) showView('pools', () => renderInvite(req.ajo_invite))
      else (showPanel('Send money'), renderSend(req))
    } catch {}
  }
}

// Switch the main area between Home, Pools, Developers and Checkout.
function showView(view, render) {
  state.view = view
  if (view !== 'checkout' && view !== 'escrow') cobrand(null)
  app.querySelectorAll('[data-view]').forEach((b) => b.setAttribute('aria-current', b.dataset.view === view ? 'page' : 'false'))
  window.scrollTo(0, 0)
  if (view === 'home') {
    $('#view').innerHTML = homeHTML()
    bindHome()
    return
  }
  ;(render ?? { pools: renderPools, escrow: renderEscrows, developers: renderDevelopers }[view])()
}

const qaIcon = {
  send: '<svg viewBox="0 0 24 24"><path d="M12 19V5M6 11l6-6 6 6"/></svg>',
  receive: '<svg viewBox="0 0 24 24"><path d="M12 5v14M6 13l6 6 6-6"/></svg>',
  escrow: '<svg viewBox="0 0 24 24"><path d="M12 3 4.5 6v5.5c0 4.6 3.2 8.2 7.5 9.5 4.3-1.3 7.5-4.9 7.5-9.5V6z"/><path d="m9 12 2 2 4-4"/></svg>',
  ajo: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="8"/><path d="M12 8v4l3 2"/></svg>',
}

function greeting() {
  const h = new Date().getHours()
  const t = $('#greet-time'), n = $('#greet-name')
  if (!t) return
  t.textContent = h < 12 ? 'Good morning' : h < 17 ? 'Good afternoon' : 'Good evening'
  n.textContent = state.username ? `@${state.username}` : 'Welcome to ORPay'
}

function showPanel(title) {
  const card = $('#panel-card')
  if (!card) return
  card.hidden = false
  $('#panel-title').textContent = title
  card.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

function openPanel(tab) {
  state.tab = tab
  showPanel(tab === 'send' ? 'Send money' : 'Receive money')
  renderPanel()
}

function homeHTML() {
  return `
      <div class="greet"><small id="greet-time"></small><strong id="greet-name"></strong></div>
      <section class="card balance" id="balance"></section>
      <nav class="quick-actions" aria-label="Quick actions">
        <button data-qa="send"><i>${qaIcon.send}</i>Send</button>
        <button data-qa="receive"><i>${qaIcon.receive}</i>Receive</button>
        <button data-qa="escrow"><i>${qaIcon.escrow}</i>Escrow</button>
        <button data-qa="ajo"><i>${qaIcon.ajo}</i>Ajo</button>
      </nav>
      <section id="reminders"></section>
      <section class="card" id="panel-card" hidden>
        <div class="section-head"><h2 id="panel-title">Send</h2><button class="link" id="panel-close" aria-label="Close">Close</button></div>
        <div id="panel"></div>
      </section>
      <section class="card">
        <div class="section-head"><h2>Recent activity</h2><button class="link" id="see-all" hidden>See all</button></div>
        <ul class="activity" id="activity"><li class="empty"><span class="empty-ico"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 9h13l-3-3M19 15H6l3 3"/></svg></span><strong>No payments yet</strong><small>Add money or ask a friend to pay you.</small></li></ul>
      </section>`
}

// Ajo reminders: contributions this wallet still owes, soonest first.
let lastPoolCheck = 0
async function checkReminders(force = false) {
  if (!force && Date.now() - lastPoolCheck < 30_000) return
  lastPoolCheck = Date.now()
  let pools = []
  try {
    pools = await node.pools(state.address)
  } catch {
    return
  }
  state.due = duePools(pools, state.address)
    .map((p) => ({ p, due: p.round_secs ? p.started_at + (p.round + 1) * p.round_secs * 1000 : 0 }))
    .sort((a, b) => (a.due || Infinity) - (b.due || Infinity))
  renderReminders()
  for (const { p, due } of state.due) {
    if (!due || due - Date.now() > 86_400_000) continue
    const key = `orpay.remind.${p.id}.${p.round}`
    try {
      if (localStorage.getItem(key)) continue
      localStorage.setItem(key, '1')
    } catch {
      continue
    }
    const msg = `${p.name}: your ${formatMoney(p.contribution, p.asset ?? '')} is ${due < Date.now() ? 'overdue' : 'due soon'}`
    toast(msg, due < Date.now() ? 'err' : 'ok')
    try {
      if (document.hidden && Notification.permission === 'granted') new Notification('ORPay ajo', { body: msg, icon: '/icon-192.png' })
    } catch {}
  }
}

function renderReminders() {
  const el = $('#reminders')
  if (!el) return
  const due = state.due ?? []
  el.innerHTML = due.length
    ? `<div class="card reminders"><h2>Ajo payments due</h2><ul class="x-list">${due
        .map(({ p, due }) => {
          const overdue = due && due < Date.now()
          return `<li><span class="x-badge ${overdue ? 'bad' : ''}">◎</span><span class="who"><strong>${esc(p.name)}</strong><small>${formatMoney(p.contribution, p.asset ?? '')} · ${
            due ? (overdue ? 'overdue' : `due ${new Date(due).toLocaleDateString()}`) : 'this round'
          }</small></span><button class="primary small" data-remind="${p.id}">Pay</button></li>`
        })
        .join('')}</ul></div>`
    : ''
  el.querySelectorAll('[data-remind]').forEach((b) => (b.onclick = () => showView('pools', () => renderPool(b.dataset.remind))))
}

function bindHome() {
  app.querySelectorAll('[data-qa]').forEach((b) => (b.onclick = () => {
    const a = b.dataset.qa
    if (a === 'send' || a === 'receive') openPanel(a)
    else if (a === 'escrow') showView('escrow', renderEscrows)
    else showView('pools', renderPools)
  }))
  $('#panel-close').onclick = () => ($('#panel-card').hidden = true)
  $('#see-all').onclick = () => ((state.allActivity = true), renderActivity())
  greeting()
  renderBalance()
  renderActivity()
  renderReminders()
  checkReminders(true)
}

// Tell the user about incoming payments that arrive while the app is open.
function notifyIncoming(history) {
  const ids = new Set(history.map((e) => e.id))
  if (state.seen) {
    for (const e of history) {
      if (state.seen.has(e.id) || e.tx.to !== state.address) continue
      const from = state.names.get(e.tx.from)
      const msg = `Received ${formatMoney(e.tx.amount, e.tx.asset ?? '')}${from ? ` from @${from}` : ''}`
      toast(msg)
      try {
        if (document.hidden && Notification.permission === 'granted') new Notification('ORPay', { body: msg, icon: '/icon-192.png' })
      } catch {}
    }
  }
  state.seen = ids
}

async function refresh() {
  try {
    const [status, account, history] = await Promise.all([
      node.status(), node.account(state.address), node.history(state.address),
    ])
    const changed = JSON.stringify(history) !== JSON.stringify(state.history)
    Object.assign(state, { status, account, history, online: true })
    notifyIncoming(history)
    checkReminders()
    renderBalance()
    renderHeader()
    const fee = $('#fee')
    if (fee && !$('[data-cur]')) fee.textContent = formatMoney(status.min_fee, '')
    if (changed) {
      renderActivity()
      resolveNames(history)
    }
  } catch {
    state.online = false
    renderHeader()
  }
}

async function resolveNames(history) {
  const n = await resolveAddrs(history.map((e) => (e.tx.from === state.address ? e.tx.to : e.tx.from)))
  if (n) renderActivity()
}

// Look up usernames for addresses not yet in the cache. Returns how many.
async function resolveAddrs(addrs) {
  const unknown = [...new Set(addrs)].filter((a) => a && !state.names.has(a))
  await Promise.all(
    unknown.map((a) => api.lookup(a).then((r) => state.names.set(a, r.username)).catch(() => state.names.set(a, null))),
  )
  return unknown.length
}

function renderWallet() {
  app.innerHTML = `
    <header id="header"></header>
    <main class="wallet" id="view"></main>
    <nav class="tabbar" aria-label="Main">
      <button data-view="home"><span class="ti">⌂</span>Home</button>
      <button data-view="pools"><span class="ti">◎</span>Pools</button>
      <button data-view="escrow"><span class="ti">⛨</span>Escrow</button>
      <button data-view="developers"><span class="ti">⌘</span>Developers</button>
    </nav>`
  app.querySelectorAll('[data-view]').forEach((b) => (b.onclick = () => showView(b.dataset.view)))
  renderHeader()
  showView('home')
}

function renderHeader() {
  const h = $('#header')
  if (!h) return
  const net = state.status
    ? `<span class="dot ${state.online ? 'on' : 'off'}"></span>${state.online ? `${esc(state.status.chain_id)} · block ${state.status.height}` : 'Node offline'}`
    : 'Connecting…'
  const who = state.username ?? ''
  if (h.dataset.who !== who || !h.firstChild) {
    h.dataset.who = who
    h.innerHTML = `
      <span class="logo" aria-label="ORPay"><span class="logo-pay">Pay</span></span>
      <span class="net" id="net"></span>
      <a class="chip ghost-chip" href="/explorer.html" target="_blank" rel="noopener" title="Explorer: see every block and transaction" aria-label="Explorer"><svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/></svg><span>Explorer</span></a>
      <button class="chip" id="me">${who ? `@${esc(who)}` : 'Claim @name'}</button>`
    $('#me').onclick = () => {
      if (state.view !== 'home') showView('home')
      who ? openPanel('receive') : renderClaim()
    }
  }
  $('#net').innerHTML = net
  greeting()
}

// The currencies this wallet can hold. Naira comes first whenever the
// chain issues it: people save and trade in naira. ORP is the network's
// fee token and is listed last.
function currencies() {
  const list = []
  const naira = state.gateway?.asset ?? state.status?.assets?.find((a) => a.symbol === 'NGN')?.symbol
  if (naira) list.push(naira)
  list.push('')
  return list
}

const balanceOf = (asset) =>
  BigInt(asset ? state.account?.assets?.[asset] ?? 0 : state.account?.balance ?? 0)

function renderBalance() {
  const el = $('#balance')
  if (!el) return
  const primary = currencies()[0]
  // Build the card once; later polls only update the numbers so buttons
  // are never swapped out from under the user's click.
  const key = `${state.config.faucet}|${primary}`
  if (el.dataset.built !== key) {
    el.dataset.built = key
    el.innerHTML = `
      <p class="label">Total balance</p>
      <p class="amount" id="bal"></p>
      ${primary ? '<p class="sub-balance" title="ORP pays network fees. It has no cash value.">Network credits: <span id="bal-orp"></span> ORP</p>' : ''}
      <div class="balance-actions">
        ${primary && state.gateway ? '<button class="ghost small" id="add">＋ Add money</button><button class="ghost small" id="withdraw">↗ Withdraw</button>' : ''}
        ${state.config.faucet ? `<button class="ghost small" id="faucet">Get ${formatAmount(state.config.faucet_amount)} test ORP</button>` : ''}
      </div>`
    const f = $('#faucet')
    if (f)
      f.onclick = async () => {
        f.disabled = true
        try {
          await api.faucet(state.address)
          toast('Test ORP on the way')
        } catch (err) {
          toast(err.message, 'err')
        }
        f.disabled = false
      }
    if ($('#add')) $('#add').onclick = () => showView('money', renderAddMoney)
    if ($('#withdraw')) $('#withdraw').onclick = () => showView('money', renderWithdraw)
  }
  if (!state.account) {
    $('#bal').textContent = '—'
    return
  }
  $('#bal').innerHTML = primary
    ? esc(formatMoney(balanceOf(primary), primary))
    : `${esc(formatAmount(balanceOf('')))} <span class="unit">ORP</span>`
  if ($('#bal-orp')) $('#bal-orp').textContent = formatAmount(balanceOf(''))
}

function renderPanel() {
  app.querySelectorAll('[data-tab]').forEach((b) => b.setAttribute('aria-selected', b.dataset.tab === state.tab))
  state.tab === 'send' ? renderSend() : renderReceive()
}

function renderSend(prefill = {}) {
  state.tab = 'send'
  app.querySelectorAll('[data-tab]').forEach((b) => b.setAttribute('aria-selected', b.dataset.tab === 'send'))
  const curs = currencies()
  let asset = prefill.asset ?? curs[0]
  const feeText = () => (state.status ? formatMoney(feeFor(state.status, asset), asset) : '…')
  $('#panel').innerHTML = `
    <form id="send" class="stack" autocomplete="off">
      ${prefill.to ? '<p class="banner">Payment request. Check the details before you send.</p>' : ''}
      <label>To<input id="to" placeholder="@username or address" required></label>
      <p class="hint" id="to-hint"></p>
      ${curs.length > 1 ? `<div class="seg" role="radiogroup" aria-label="Currency">${curs.map((c) => `<button type="button" role="radio" data-cur="${c}">${assetLabel(c)}</button>`).join('')}</div>` : ''}
      <label><span id="amount-label">Amount</span><input id="amount" inputmode="decimal" placeholder="0.00" required></label>
      <label><span>Note <span class="muted">(optional, public)</span></span><input id="memo" maxlength="140"></label>
      <p class="hint">Network fee: <span id="fee">${feeText()}</span></p>
      <p class="error" id="err"></p>
      <button class="primary" id="go">Review</button>
    </form>`
  const setAsset = (a) => {
    asset = a
    app.querySelectorAll('[data-cur]').forEach((b) => b.setAttribute('aria-checked', b.dataset.cur === a))
    $('#amount-label').textContent = a === 'NGN' ? 'Amount (₦)' : `Amount (${a || 'ORP'})`
    $('#fee').textContent = feeText()
  }
  app.querySelectorAll('[data-cur]').forEach((b) => (b.onclick = () => setAsset(b.dataset.cur)))
  setAsset(asset)
  let resolved = null
  const toInput = $('#to')
  if (prefill.to) {
    toInput.value = isAddress(prefill.to) ? prefill.to : '@' + prefill.to.replace(/^@/, '')
    $('#amount').value = prefill.amount ?? ''
    $('#memo').value = prefill.memo ?? ''
  }
  toInput.oninput = debounce(async () => {
    resolved = null
    const v = toInput.value.trim()
    const hint = $('#to-hint')
    if (!hint) return // form was replaced before the debounce fired
    hint.textContent = ''
    if (!v) return
    try {
      resolved = await resolveRecipient(v)
      if (!hint.isConnected) return
      hint.textContent = v.startsWith('@') || !isAddress(v) ? `→ ${short(resolved)}` : state.names.get(v) ? `@${state.names.get(v)}` : ''
    } catch (err) {
      if (hint.isConnected) hint.textContent = err.message
    }
  }, 300)

  if (prefill.to) toInput.oninput()

  $('#send').onsubmit = async (e) => {
    e.preventDefault()
    const err = $('#err')
    err.textContent = ''
    try {
      const to = resolved ?? (await resolveRecipient(toInput.value.trim()))
      if (to === state.address) throw new Error("That's your own wallet.")
      const amount = parseAmount($('#amount').value.replace(/,/g, ''))
      if (amount === 0n) throw new Error('Amount must be more than 0.')
      if (asset === 'NGN' && amount % 10_000n !== 0n) throw new Error('Naira amounts can have at most 2 decimals.')
      const total = amount + feeFor(state.status, asset)
      if (state.account && total > balanceOf(asset)) throw new Error(`Not enough ${asset === 'NGN' ? 'naira' : 'ORP'} for amount plus fee.`)
      renderReview({ to, label: toInput.value.trim(), amount, memo: $('#memo').value, asset })
    } catch (e2) {
      err.textContent = e2.message
    }
  }
}

function renderReview({ to, label, amount, memo, asset }) {
  $('#panel').innerHTML = `
    <div class="review">
      <p class="label">You're sending</p>
      <p class="amount">${esc(formatMoney(amount, asset))}</p>
      <dl>
        <dt>To</dt><dd>${esc(label.startsWith('@') ? label : short(to))}</dd>
        <dt>Fee</dt><dd>${esc(formatMoney(feeFor(state.status, asset), asset))}</dd>
        ${memo ? `<dt>Note</dt><dd>${esc(memo)}</dd>` : ''}
      </dl>
      <p class="error" id="err"></p>
      <div class="row">
        <button class="ghost" id="cancel">Back</button>
        <button class="primary" id="confirm">Send now</button>
      </div>
    </div>`
  $('#cancel').onclick = () => renderSend({ asset })
  $('#confirm').onclick = async () => {
    const btn = $('#confirm')
    btn.disabled = true
    btn.textContent = 'Sending…'
    try {
      const id = await send({ seed: state.seed, to, amount, memo, asset })
      btn.textContent = 'Confirming…'
      const t = await waitForCommit(id)
      toast(`Sent ${formatMoney(amount, asset)} · block ${t.location.height}`)
      refresh()
      renderSend({ asset })
    } catch (err) {
      $('#err').textContent = err.message
      btn.disabled = false
      btn.textContent = 'Send now'
    }
  }
}

function renderReceive() {
  const handle = state.username ?? state.address
  $('#panel').innerHTML = `
    <div class="stack">
      <div class="qr-wrap">
        <div class="qr" id="qr" role="img" aria-label="QR code to pay you"></div>
        <div class="qr-id">
          ${state.username ? `<p class="big">@${esc(state.username)}</p>` : `<button class="ghost small" id="claim">Claim a username so people can pay @you</button>`}
          <p class="muted mono small-text">${short(state.address)}</p>
        </div>
      </div>
      <form id="req" class="request">
        <p class="label">Request a specific amount</p>
        <div class="row">
          <input id="req-amount" inputmode="decimal" placeholder="Amount (optional)">
          <input id="req-memo" maxlength="140" placeholder="What for? (optional)">
        </div>
        <p class="error" id="req-err"></p>
      </form>
      <div class="row">
        <button class="primary" id="share">Share pay link</button>
        <button class="ghost" id="copy">Copy link</button>
      </div>
      <button class="link" id="copy-addr">Copy full address</button>
      <details>
        <summary>Wallet settings</summary>
        <div class="stack">
          <button class="ghost small" id="reveal">Show recovery words</button>
          <button class="ghost small" id="notify">Notify me about incoming payments</button>
          <button class="danger small" id="lock">Lock wallet</button>
        </div>
      </details>
    </div>`
  let link = ''
  const update = () => {
    const amount = $('#req-amount').value.trim()
    $('#req-err').textContent = ''
    if (amount) {
      try {
        parseAmount(amount)
      } catch (err) {
        $('#req-err').textContent = err.message
        return
      }
    }
    link = payLink({ to: handle, amount, memo: $('#req-memo').value.trim() })
    $('#qr').innerHTML = renderSVG(link, { border: 1 })
  }
  update()
  $('#req-amount').oninput = update
  $('#req-memo').oninput = update
  $('#req').onsubmit = (e) => e.preventDefault()
  $('#copy').onclick = () => navigator.clipboard.writeText(link).then(() => toast('Pay link copied'))
  $('#share').onclick = async () => {
    if (navigator.share) {
      try {
        await navigator.share({ title: 'Pay me with ORPay', url: link })
      } catch {}
    } else {
      navigator.clipboard.writeText(link).then(() => toast('Pay link copied'))
    }
  }
  $('#copy-addr').onclick = () => navigator.clipboard.writeText(state.address).then(() => toast('Address copied'))
  if ($('#claim')) $('#claim').onclick = renderClaim
  $('#reveal').onclick = (e) => {
    if (confirm('Show your recovery words? Make sure nobody can see your screen.')) e.target.outerHTML = wordGrid(state.seed)
  }
  $('#lock').onclick = () => location.reload()
  const nb = $('#notify')
  if (!('Notification' in window) || !('serviceWorker' in navigator)) nb.remove()
  else {
    if (Notification.permission === 'granted') nb.textContent = 'Notifications are on'
    nb.onclick = async () => {
      nb.disabled = true
      try {
        await enablePush()
        nb.textContent = 'Notifications are on'
        toast("You'll be notified about payments, ajo due dates and escrow updates")
      } catch (err) {
        toast(err.message, 'err')
      }
      nb.disabled = false
    }
  }
}

function renderClaim() {
  showPanel('Claim a username')
  state.tab = 'receive'
  app.querySelectorAll('[data-tab]').forEach((b) => b.setAttribute('aria-selected', b.dataset.tab === 'receive'))
  $('#panel').innerHTML = `
    <form id="f" class="stack" autocomplete="off">
      <label>Choose a username<input id="name" placeholder="yourname" pattern="[a-z0-9_]{3,20}" required></label>
      <p class="hint">3–20 characters: lowercase letters, numbers, underscore.</p>
      <p class="error" id="err"></p>
      <button class="primary" id="go">Claim</button>
    </form>`
  $('#f').onsubmit = async (e) => {
    e.preventDefault()
    const username = $('#name').value.trim().toLowerCase()
    $('#go').disabled = true
    try {
      const sig = await signMessage(registerMessage(username, state.address), state.seed)
      await api.register({ username, address: state.address, sig })
      state.username = username
      toast(`You're @${username}`)
      renderHeader()
      renderReceive()
    } catch (err) {
      $('#err').textContent = err.message
      $('#go').disabled = false
    }
  }
}

function renderActivity() {
  const ul = $('#activity')
  if (!ul) return
  if (!state.history.length) return (ul.innerHTML = '<li class="empty"><span class="empty-ico"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 9h13l-3-3M19 15H6l3 3"/></svg></span><strong>No payments yet</strong><small>Add money or ask a friend to pay you.</small></li>')
  const all = state.allActivity || state.history.length <= 6
  $('#see-all').hidden = all
  ul.innerHTML = (all ? state.history : state.history.slice(0, 6))
    .map((e) => {
      if (e.tx.pool) return poolActivity(e)
      if (e.tx.escrow) return escrowActivity(e)
      if (e.tx.kind === 'mint' || e.tx.kind === 'burn') return cashActivity(e)
      const out = e.tx.from === state.address
      const other = out ? e.tx.to : e.tx.from
      const name = state.names.get(other)
      const when = new Date(e.time).toLocaleString([], { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' })
      return `
        <li>
          <span class="icon ${out ? 'out' : 'in'}" aria-hidden="true">${out ? '↑' : '↓'}</span>
          <span class="who">
            <strong>${name ? `@${esc(name)}` : `<span class="mono">${short(other)}</span>`}</strong>
            <small>${when}${e.tx.memo ? ` · ${esc(e.tx.memo)}` : ''}</small>
          </span>
          <span class="amt ${out ? 'out' : 'in'}">${out ? '−' : '+'}${esc(formatMoney(e.tx.amount, e.tx.asset ?? ''))}</span>
        </li>`
    })
    .join('')
  ul.querySelectorAll('[data-pool]').forEach((li) => (li.onclick = () => showView('pools', () => renderPool(li.dataset.pool))))
  ul.querySelectorAll('[data-escrow]').forEach((li) => (li.onclick = () => showView('escrow', () => renderEscrow(li.dataset.escrow))))
}

function cashActivity(e) {
  const when = new Date(e.time).toLocaleString([], { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' })
  const mint = e.tx.kind === 'mint'
  const memo = e.tx.memo ?? ''
  const text = !mint ? 'Withdrawal to bank' : memo.startsWith('inv:') ? 'Card payment received' : memo.startsWith('withdrawal refund') ? 'Withdrawal refunded' : 'Added money'
  return `
    <li>
      <span class="icon ${mint ? 'in' : 'out'}" aria-hidden="true">${mint ? '＋' : '↗'}</span>
      <span class="who"><strong>${text}</strong><small>${when} · bank or card</small></span>
      <span class="amt ${mint ? 'in' : 'out'}">${mint ? '+' : '−'}${esc(formatMoney(e.tx.amount, e.tx.asset ?? ''))}</span>
    </li>`
}

function escrowActivity(e) {
  const when = new Date(e.time).toLocaleString([], { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' })
  const x = e.tx.escrow
  const mine = e.tx.from === state.address
  const text = {
    create: mine ? 'Locked funds in escrow' : 'Escrow opened with you',
    dispatch: mine ? 'Marked escrow dispatched' : 'Seller dispatched',
    release: mine ? 'Released an escrow milestone' : 'Escrow milestone released',
    dispute: 'Escrow dispute opened',
    resolve: 'Escrow dispute resolved',
    refund: 'Escrow refunded',
    claim: 'Escrow claimed by seller',
  }[x.op]
  const id = x.op === 'create' ? e.id : x.id
  return `
    <li class="clickable" data-escrow="${id}">
      <span class="icon pool" aria-hidden="true">⛨</span>
      <span class="who"><strong>${text}</strong><small>${when}${x.ref ? ` · #${esc(x.ref)}` : ''} · tap to view</small></span>
      <span class="amt"></span>
    </li>`
}

function poolActivity(e) {
  const when = new Date(e.time).toLocaleString([], { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' })
  const text = { create: 'Created a savings pool', join: 'Joined a savings pool', contribute: 'Pool contribution', claim: 'Received pool payout' }[e.tx.pool.op]
  const id = e.tx.pool.op === 'create' ? e.id : e.tx.pool.id
  return `
    <li class="clickable" data-pool="${id}">
      <span class="icon pool" aria-hidden="true">◎</span>
      <span class="who"><strong>${text}</strong><small>${when} · tap to view pool</small></span>
      <span class="amt">${e.tx.pool.op === 'claim' ? '<span class="in">payout</span>' : ''}</span>
    </li>`
}

async function resolveRecipient(v) {
  if (isAddress(v)) return v
  const name = v.replace(/^@/, '').toLowerCase()
  if (!/^[a-z0-9_]{3,20}$/.test(name)) throw new Error('Enter an @username or a 64-character address')
  const r = await api.resolve(name)
  state.names.set(r.address, name)
  return r.address
}

function debounce(fn, ms) {
  let t
  return (...a) => {
    clearTimeout(t)
    t = setTimeout(() => fn(...a), ms)
  }
}

// Installable app: register the service worker in production builds.
if ('serviceWorker' in navigator && import.meta.env.PROD && !window.Capacitor) {
  navigator.serviceWorker.register('/sw.js').catch(() => {})
}

// Web push: ask permission, subscribe this device, register it with ORPay.
// On iPhone this works once ORPay is added to the Home Screen.
async function enablePush() {
  const p = await Notification.requestPermission()
  if (p !== 'granted') throw new Error('Notifications are blocked in your browser settings')
  if (!('PushManager' in window)) throw new Error('This browser cannot receive push notifications. On iPhone, add ORPay to your Home Screen first.')
  const reg = await navigator.serviceWorker.register('/sw.js')
  await navigator.serviceWorker.ready
  const { public_key } = await api.pushKey()
  const raw = Uint8Array.from(atob(public_key.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - (public_key.length % 4)) % 4)), (c) => c.charCodeAt(0))
  const sub = (await reg.pushManager.getSubscription()) ?? (await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: raw }))
  await api.pushSubscribe(state.seed, sub.toJSON())
}

// Shared context handed to the feature modules (pools, checkout, money...).
const ctx = {
  state, $, esc, short, toast, resolveRecipient,
  nameOf: (a) => state.names.get(a),
  resolveNames: resolveAddrs,
  root: () => $('#view'),
  home: () => showView('home'),
  refresh: () => refresh(),
  currencies: () => currencies(),
  gatewayCall: (fn) => fn(),
  // Checkout works without a wallet (card payment). Paying from the wallet
  // asks the customer to unlock or create one, then returns to the checkout.
  requireWallet: () => (hasVault() ? renderUnlock() : renderWelcome()),
  // Show a partner's name next to ORPay on its checkout/escrow pages.
  cobrand: (partner) => cobrand(partner),
}

function cobrand(partner) {
  const logo = app.querySelector('header .logo')
  if (!logo) return
  const root = document.documentElement // the colour also tints partner marks inside the page
  if (!partner) {
    logo.classList.remove('cobrand')
    logo.innerHTML = '<span class="logo-pay">Pay</span>'
    root.style.removeProperty('--partner')
    return
  }
  const name = partner.brand_name || partner.name
  logo.classList.add('cobrand')
  logo.innerHTML = `${partnerMark(partner)}<span class="partner-name">${esc(name)}</span><span class="cobrand-x">×</span><span class="cobrand-orpay">ORPay</span>`
  logo.setAttribute('aria-label', `${name} with ORPay`)
  if (/^#[0-9a-f]{6}$/i.test(partner.brand_color ?? '')) root.style.setProperty('--partner', partner.brand_color)
  else root.style.removeProperty('--partner')
}
initPools(ctx)
initCheckout(ctx)
initDevelopers(ctx)
initMoney(ctx)
initEscrow(ctx)

async function renderGuestCheckout(invoice) {
  app.innerHTML = `
    <header id="header"><span class="logo" aria-label="ORPay"><span class="logo-pay">Pay</span></span><span class="net"></span>
      <button class="chip" id="signin">${hasVault() ? 'Unlock wallet' : 'Open ORPay'}</button></header>
    <main class="wallet" id="view"></main>`
  $('#signin').onclick = ctx.requireWallet
  // Load network status (fees) and payment providers before drawing the page.
  await Promise.all([
    node.status().then((st) => (state.status = st)).catch(() => {}),
    gateway.config().then((g) => (state.gateway = g)).catch(() => {}),
  ])
  renderCheckout(invoice.invoice, invoice.card)
}

{
  let pending = null
  try {
    pending = JSON.parse(sessionStorage.getItem(PENDING))
  } catch {}
  if (pending?.invoice) renderGuestCheckout(pending)
  else if (hasVault()) renderUnlock()
  else showLanding()
}

function showLanding() {
  renderLanding(app, {
    onStart: async () => showBackup(await newSeed()),
    onSignIn: renderImport,
    signInLabel: 'Sign in',
  })
  window.scrollTo(0, 0)
}
