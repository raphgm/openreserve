import './style.css'
import {
  addressOf, api, assetLabel, feeFor, formatAmount, formatMoney, fromHex, gateway, isAddress, newSeed, node, parseAmount,
  payLink, readPayLink, registerMessage, seedToWords, send, signMessage, waitForCommit, wordsToSeed,
} from './orp.js'
import { renderSVG } from 'uqr'
import { initPools, renderPool, renderPools } from './pools.js'
import { initCheckout, renderCheckout } from './checkout.js'
import { initDevelopers, renderDevelopers } from './developers.js'
import { confirmDeposit, initMoney, renderAddMoney, renderWithdraw } from './money.js'
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
      <div class="brand"><span class="logo">ORPay</span></div>
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
      <div class="brand"><span class="logo">ORPay</span></div>
      <h1>Welcome back</h1>
      <p class="muted mono">${addr ? short(addr) : ''}</p>
      <form id="f" class="stack">
        <label>Password<input type="password" id="pw" autocomplete="current-password" autofocus required></label>
        <p class="error" id="err"></p>
        <button class="primary" id="go">Unlock</button>
      </form>
      <button class="link" id="forget">Use a different wallet</button>
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
      if (state.view === 'home' && state.tab === 'send' && !$('#to')?.value) renderSend()
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
      else renderSend(req)
    } catch {}
  }
}

// Switch the main area between Home, Pools, Developers and Checkout.
function showView(view, render) {
  state.view = view
  app.querySelectorAll('[data-view]').forEach((b) => b.setAttribute('aria-current', b.dataset.view === view ? 'page' : 'false'))
  window.scrollTo(0, 0)
  if (view === 'home') {
    $('#view').innerHTML = homeHTML()
    bindHome()
    return
  }
  ;(render ?? { pools: renderPools, developers: renderDevelopers }[view])()
}

function homeHTML() {
  return `
      <section class="card balance" id="balance"></section>
      <section class="card">
        <div class="tabs" role="tablist">
          <button role="tab" data-tab="send">Send</button>
          <button role="tab" data-tab="receive">Receive</button>
        </div>
        <div id="panel"></div>
      </section>
      <section class="card">
        <h2>Activity</h2>
        <ul class="activity" id="activity"><li class="empty">No payments yet.</li></ul>
      </section>`
}

function bindHome() {
  app.querySelectorAll('[data-tab]').forEach((b) => (b.onclick = () => ((state.tab = b.dataset.tab), renderPanel())))
  renderBalance()
  renderPanel()
  renderActivity()
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
      <span class="logo">ORPay</span>
      <span class="net" id="net"></span>
      <button class="chip" id="me">${who ? `@${esc(who)}` : 'Claim a username'}</button>`
    $('#me').onclick = () => {
      if (state.view !== 'home') showView('home')
      who ? ((state.tab = 'receive'), renderPanel()) : renderClaim()
    }
  }
  $('#net').innerHTML = net
}

// The currencies this wallet can hold: naira first when the naira
// gateway runs, then ORP.
function currencies() {
  const list = []
  if (state.gateway) list.push(state.gateway.asset)
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
      <p class="label">${primary ? 'Naira balance' : 'Balance'}</p>
      <p class="amount" id="bal"></p>
      ${primary ? '<p class="sub-balance">ORP <span id="bal-orp"></span></p>' : ''}
      <div class="balance-actions">
        ${primary ? '<button class="ghost small" id="add">＋ Add money</button><button class="ghost small" id="withdraw">↗ Withdraw</button>' : ''}
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
  if (!('Notification' in window)) nb.remove()
  else {
    if (Notification.permission === 'granted') nb.textContent = 'Payment notifications are on'
    nb.onclick = async () => {
      const p = await Notification.requestPermission()
      toast(p === 'granted' ? 'You will be notified about incoming payments' : 'Notifications are blocked in your browser settings', p === 'granted' ? 'ok' : 'err')
      if (p === 'granted') nb.textContent = 'Payment notifications are on'
    }
  }
}

function renderClaim() {
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
  if (!state.history.length) return (ul.innerHTML = '<li class="empty">No payments yet.</li>')
  ul.innerHTML = state.history
    .map((e) => {
      if (e.tx.pool) return poolActivity(e)
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
}
initPools(ctx)
initCheckout(ctx)
initDevelopers(ctx)
initMoney(ctx)

function renderGuestCheckout(invoice) {
  app.innerHTML = `
    <header id="header"><span class="logo">ORPay</span><span class="net"></span>
      <button class="chip" id="signin">${hasVault() ? 'Unlock wallet' : 'Open ORPay'}</button></header>
    <main class="wallet" id="view"></main>`
  $('#signin').onclick = ctx.requireWallet
  node.status().then((st) => (state.status = st)).catch(() => {})
  gateway.config().then((g) => (state.gateway = g)).catch(() => {})
  renderCheckout(invoice.invoice, invoice.card)
}

{
  let pending = null
  try {
    pending = JSON.parse(sessionStorage.getItem(PENDING))
  } catch {}
  if (pending?.invoice) renderGuestCheckout(pending)
  else hasVault() ? renderUnlock() : renderWelcome()
}
