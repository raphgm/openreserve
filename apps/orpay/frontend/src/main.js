import './style.css'
import {
  addressOf, api, formatAmount, fromHex, isAddress, newSeed, node, parseAmount,
  payLink, readPayLink, registerMessage, seedToWords, send, signMessage, waitForCommit, wordsToSeed,
} from './orp.js'
import { renderSVG } from 'uqr'
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
  online: true,
}

// A pay link (?to=...) opened before unlocking is kept until the wallet opens.
const PENDING = 'orpay.pending'
{
  const req = readPayLink()
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
  api.lookup(address).then((r) => ((state.username = r.username), renderHeader())).catch(() => {})
  refresh()
  setInterval(refresh, 2000)
  const pending = sessionStorage.getItem(PENDING)
  if (pending) {
    sessionStorage.removeItem(PENDING)
    try {
      renderSend(JSON.parse(pending))
    } catch {}
  }
}

async function refresh() {
  try {
    const [status, account, history] = await Promise.all([
      node.status(), node.account(state.address), node.history(state.address),
    ])
    const changed = JSON.stringify(history) !== JSON.stringify(state.history)
    Object.assign(state, { status, account, history, online: true })
    renderBalance()
    renderHeader()
    const fee = $('#fee')
    if (fee) fee.textContent = formatAmount(status.min_fee)
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
  const unknown = new Set()
  for (const e of history) {
    const other = e.tx.from === state.address ? e.tx.to : e.tx.from
    if (!state.names.has(other)) unknown.add(other)
  }
  await Promise.all(
    [...unknown].map((a) => api.lookup(a).then((r) => state.names.set(a, r.username)).catch(() => state.names.set(a, null))),
  )
  if (unknown.size) renderActivity()
}

function renderWallet() {
  app.innerHTML = `
    <header id="header"></header>
    <main class="wallet">
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
      </section>
    </main>`
  app.querySelectorAll('[data-tab]').forEach((b) => (b.onclick = () => ((state.tab = b.dataset.tab), renderPanel())))
  renderHeader()
  renderBalance()
  renderPanel()
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
    $('#me').onclick = who ? () => ((state.tab = 'receive'), renderPanel()) : renderClaim
  }
  $('#net').innerHTML = net
}

function renderBalance() {
  const el = $('#balance')
  if (!el) return
  const bal = state.account ? formatAmount(state.account.balance) : '—'
  // Build the card once; later polls only update the number so buttons
  // are never swapped out from under the user's click.
  const key = String(state.config.faucet)
  if (el.dataset.built !== key) {
    el.dataset.built = key
    el.innerHTML = `
      <p class="label">Balance</p>
      <p class="amount"><span id="bal"></span> <span class="unit">ORP</span></p>
      ${state.config.faucet ? `<button class="ghost small" id="faucet">Get ${formatAmount(state.config.faucet_amount)} test ORP</button>` : ''}`
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
  }
  $('#bal').textContent = bal
}

function renderPanel() {
  app.querySelectorAll('[data-tab]').forEach((b) => b.setAttribute('aria-selected', b.dataset.tab === state.tab))
  state.tab === 'send' ? renderSend() : renderReceive()
}

function renderSend(prefill = {}) {
  state.tab = 'send'
  app.querySelectorAll('[data-tab]').forEach((b) => b.setAttribute('aria-selected', b.dataset.tab === 'send'))
  const fee = state.status ? formatAmount(state.status.min_fee) : '…'
  $('#panel').innerHTML = `
    <form id="send" class="stack" autocomplete="off">
      ${prefill.to ? '<p class="banner">Payment request. Check the details before you send.</p>' : ''}
      <label>To<input id="to" placeholder="@username or address" required></label>
      <p class="hint" id="to-hint"></p>
      <label>Amount (ORP)<input id="amount" inputmode="decimal" placeholder="0.00" required></label>
      <label><span>Note <span class="muted">(optional, public)</span></span><input id="memo" maxlength="140"></label>
      <p class="hint">Network fee: <span id="fee">${fee}</span> ORP</p>
      <p class="error" id="err"></p>
      <button class="primary" id="go">Review</button>
    </form>`
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
      const amount = parseAmount($('#amount').value)
      if (amount === 0n) throw new Error('Amount must be more than 0.')
      const total = amount + BigInt(state.status.min_fee)
      if (state.account && total > BigInt(state.account.balance)) throw new Error('Not enough ORP for amount plus fee.')
      renderReview({ to, label: toInput.value.trim(), amount, memo: $('#memo').value })
    } catch (e2) {
      err.textContent = e2.message
    }
  }
}

function renderReview({ to, label, amount, memo }) {
  $('#panel').innerHTML = `
    <div class="review">
      <p class="label">You're sending</p>
      <p class="amount">${formatAmount(amount)} <span class="unit">ORP</span></p>
      <dl>
        <dt>To</dt><dd>${esc(label.startsWith('@') ? label : short(to))}</dd>
        <dt>Fee</dt><dd>${formatAmount(state.status.min_fee)} ORP</dd>
        ${memo ? `<dt>Note</dt><dd>${esc(memo)}</dd>` : ''}
      </dl>
      <p class="error" id="err"></p>
      <div class="row">
        <button class="ghost" id="cancel">Back</button>
        <button class="primary" id="confirm">Send now</button>
      </div>
    </div>`
  $('#cancel').onclick = () => renderSend()
  $('#confirm').onclick = async () => {
    const btn = $('#confirm')
    btn.disabled = true
    btn.textContent = 'Sending…'
    try {
      const id = await send({ seed: state.seed, to, amount, memo })
      btn.textContent = 'Confirming…'
      const t = await waitForCommit(id)
      toast(`Sent ${formatAmount(amount)} ORP · block ${t.location.height}`)
      refresh()
      renderSend()
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
          <span class="amt ${out ? 'out' : 'in'}">${out ? '−' : '+'}${formatAmount(e.tx.amount)}</span>
        </li>`
    })
    .join('')
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

hasVault() ? renderUnlock() : renderWelcome()
