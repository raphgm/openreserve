import './style.css'
import {
  addressOf, api, assetLabel, toHex, partnerMark, feeFor, formatAmount, formatMoney, fromHex, gateway, isAddress, newSeed, node, parseAmount,
  payLink, readPayLink, registerMessage, seedToWords, send, signMessage, waitForCommit, wordsToSeed,
} from './orp.js'
import { renderSVG } from 'uqr'
import { duePools, initPools, renderInvite, renderPool, renderPools } from './pools.js'
import { initCheckout, renderCheckout } from './checkout.js'
import { initDevelopers, renderDevelopers } from './developers.js'
import { confirmDeposit, initMoney, renderAddMoney, renderWithdraw } from './money.js'
import { initEscrow, renderEscrow, renderEscrows, renderFundRequest } from './escrow.js'
import { renderLanding } from './landing.js'
import { openScanner } from './scan.js'
import { fillTrust, showTrustCard, trustChip, trustOf } from './trust.js'
import { radarBanner, riskOf } from './radar.js'
import { COMING, LANGS, getLang, setLang, startI18n } from './i18n.js'
import { bioEnabled, bioSupported, disableBio, enableBio, unlockBio } from './bio.js'
import { newSignInWords, parseSignInWords, sealSignIn, signInToken, unsealSignIn } from './signin.js'
import { clearVault, hasVault, saveVault, unlockVault, vaultAddress } from './vault.js'

const app = document.getElementById('app')

// Theme: follow the phone (default), or a fixed light/dark choice.
const THEME = 'orpay.theme'
function applyTheme(t = localStorage.getItem(THEME) ?? 'system') {
  if (t === 'system') delete document.documentElement.dataset.theme
  else document.documentElement.dataset.theme = t
}
try {
  applyTheme()
} catch {}
startI18n()

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
  if (q.get('ref')) {
    try {
      localStorage.setItem('orpay.ref', q.get('ref'))
    } catch {}
  }
  const req = q.get('deposit')
    ? { deposit: q.get('deposit') }
    : q.get('invoice')
    ? { invoice: q.get('invoice'), card: q.get('card') }
    : q.get('pool')
      ? { pool: q.get('pool') }
      : q.get('escrow')
      ? { escrow: q.get('escrow'), action: q.get('action') }
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
      <div class="brand"><span class="logo" aria-label="ORPay"><span class="logo-or">OR</span><span class="logo-pay">Pay</span></span></div>
      <h1>Money that moves like messages.</h1>
      <p class="muted">Send ORP to anyone by @username. Your keys stay on this device.</p>
      <div class="stack">
        <button class="primary" id="create">Create a wallet</button>
        <button class="ghost" id="import">Sign in</button>
      </div>
    </main>`
  $('#create').onclick = async () => showBackup(await newSeed())
  $('#import').onclick = renderSignIn
}

// Sign in on this device with @username and six words. The 24 recovery
// words are the fallback if the six are lost.
function renderSignIn() {
  app.innerHTML = `
    <main class="narrow">
      <button class="link back" id="back">← Back</button>
      <h1>Sign in</h1>
      <p class="muted">Use your @username and your six sign-in words.</p>
      <form id="f" class="stack" autocomplete="off">
        <label>Username<input id="user" placeholder="@yourname" autocapitalize="none" spellcheck="false" required></label>
        <label>Six sign-in words<textarea id="words" rows="2" spellcheck="false" autocapitalize="none" placeholder="e.g. river panel ocean sugar actor midnight" required></textarea></label>
        <p class="error" id="err"></p>
        <button class="primary" id="go">Sign in</button>
      </form>
      <button class="link" id="recover">Lost your six words? Recover with your 24 recovery words</button>
    </main>`
  $('#back').onclick = () => (hasVault() ? renderUnlock() : showLanding())
  $('#recover').onclick = renderImport
  $('#f').onsubmit = async (e) => {
    e.preventDefault()
    $('#err').textContent = ''
    const go = $('#go')
    go.disabled = true
    go.textContent = 'Signing in…'
    try {
      const user = $('#user').value.trim().replace(/^@/, '').toLowerCase()
      const words = parseSignInWords($('#words').value)
      const { salt } = await api.signInSalt(user)
      const sealed = await api.signInOpen(user, await signInToken(words, salt))
      const seed = await unsealSignIn(words, sealed)
      if ((await addressOf(seed)) !== sealed.address) throw new Error('Those words do not match this wallet')
      renderSetPassword(seed)
    } catch (err) {
      $('#err').textContent = err.message
      go.disabled = false
      go.textContent = 'Sign in'
    }
  }
}

// Profile menu (tap your @username): account, security and sign out.
function renderProfile() {
  showPanel(state.username ? `@${state.username}` : 'Your wallet')
  const item = (id, icon, title, sub, cls = '') =>
    `<button class="menu-item ${cls}" id="${id}"><span class="mi-ico">${icon}</span><span><b>${title}</b><small>${sub}</small></span></button>`
  $('#panel').innerHTML = `<div id="my-trust"></div><div class="menu">
    ${state.username ? '' : item('m-claim', '@', 'Claim a username', 'So people can pay you by name')}
    ${item('m-receive', '↓', 'My address & QR', 'Share to get paid')}
    ${state.username ? item('m-invite', '🎁', 'Invite friends', 'Share your link and see who joined') : ''}
    ${item('m-six', '6', 'Six-word sign-in', 'Open this wallet on another device')}
    ${item('m-words', '24', 'Recovery words', 'Your offline backup')}
    ${item('m-bio', '☝︎', 'Face ID / fingerprint', bioEnabled() ? 'On: unlock without your password' : 'Unlock without typing your password')}
    ${item('m-notify', '🔔', 'Notifications', 'Payments, ajo turns, escrow updates')}
    ${item('m-lang', 'Aa', 'Language', LANGS.find((l) => l.id === getLang())?.name ?? 'English')}
    ${item('m-theme', '◐', 'Theme', { system: 'Follows your phone', light: 'Light', dark: 'Dark' }[localStorage.getItem(THEME) ?? 'system'])}
    ${item('m-lock', '⎋', 'Sign out', 'Lock the wallet on this device', 'danger')}
    ${item('m-forget', '✕', 'Remove from this device', 'Needs your six or 24 words to sign in again', 'danger subtle')}
  </div>`
  const on = (id, fn) => $(id) && ($(id).onclick = fn)
  showTrustCard($('#my-trust'), state.address, { title: 'Your trust profile' })
  on('#m-claim', renderClaim)
  on('#m-receive', () => openPanel('receive'))
  on('#m-invite', renderInvites)
  on('#m-six', renderSignInSetup)
  on('#m-words', () => {
    if (!confirm('Show your 24 recovery words? Make sure nobody can see your screen.')) return
    showPanel('Recovery words')
    $('#panel').innerHTML = `<p class="muted">Anyone with these words can take your money. Keep them offline.</p>${wordGrid(state.seed)}`
  })
  on('#m-notify', () => enablePush().then(() => toast('Notifications are on')).catch((err) => toast(err.message, 'err')))
  on('#m-bio', async () => {
    if (bioEnabled()) {
      if (confirm('Turn off Face ID / fingerprint unlock on this device?')) disableBio(), toast('Turned off'), renderProfile()
      return
    }
    try {
      if (!(await bioSupported())) throw new Error('No Face ID, Touch ID or fingerprint is available in this browser.')
      await enableBio(state.seed, state.address, state.username ? `@${state.username}` : 'ORPay wallet')
      toast('Face ID / fingerprint unlock is on')
      renderProfile()
    } catch (err) {
      if (err.name !== 'NotAllowedError') toast(err.message, 'err')
    }
  })
  on('#m-lang', () => {
    showPanel('Language')
    $('#panel').innerHTML = `<div class="menu">${LANGS.map((l) => `<button class="menu-item" data-lang="${l.id}"><span class="mi-ico">${l.id === getLang() ? '✓' : ''}</span><span><b>${l.name}</b></span></button>`).join('')}</div>
      <p class="muted small-text">Coming soon: ${COMING.join(', ')}. We're having native speakers check every word first.</p>`
    app.querySelectorAll('[data-lang]').forEach((b) => (b.onclick = () => setLang(b.dataset.lang)))
  })
  on('#m-theme', () => {
    const order = ['system', 'light', 'dark']
    const next = order[(order.indexOf(localStorage.getItem(THEME) ?? 'system') + 1) % 3]
    try {
      localStorage.setItem(THEME, next)
    } catch {}
    applyTheme(next)
    renderProfile()
  })
  on('#m-lock', lock)
  on('#m-forget', () => {
    if (!confirm('Remove this wallet from this device? You will need your six sign-in words or 24 recovery words to use it here again.')) return
    clearVault()
    disableBio()
    try {
      sessionStorage.removeItem(SESSION)
    } catch {}
    location.href = '/'
  })
}

// Invite friends: personal link, share buttons and who joined.
async function renderInvites() {
  showPanel('Invite friends')
  const link = `${location.origin}/?ref=${encodeURIComponent(state.username)}`
  const text = `Join me on ORPay: save in ajo and buy safely with escrow. ${link}`
  $('#panel').innerHTML = `
    <p class="muted small-text">Share your link. Everyone who joins and makes their first payment counts as your referral.</p>
    <div class="invite-link"><span class="mono">${esc(link)}</span><button class="ghost small" id="inv-copy">Copy</button></div>
    <div class="row"><a class="button primary" href="https://wa.me/?text=${encodeURIComponent(text)}" target="_blank" rel="noopener">Share on WhatsApp</a>
    ${navigator.share ? '<button class="ghost" id="inv-share">More…</button>' : ''}</div>
    <div class="tiles" id="inv-stats"><div class="tile"><span>Joined</span><strong>…</strong></div><div class="tile"><span>Active</span><strong>…</strong></div></div>
    <ul class="activity" id="inv-list"></ul>`
  $('#inv-copy').onclick = () => navigator.clipboard.writeText(link).then(() => toast('Link copied'))
  if ($('#inv-share')) $('#inv-share').onclick = () => navigator.share({ title: 'ORPay', text, url: link }).catch(() => {})
  try {
    const r = await api.referrals(state.seed)
    $('#inv-stats').innerHTML = `<div class="tile"><span>Joined</span><strong>${r.invited}</strong></div><div class="tile"><span>Active</span><strong>${r.active}</strong><small>made a payment</small></div>`
    $('#inv-list').innerHTML = r.people.map((p) => `<li><span class="who"><strong>${p.username ? '@' + esc(p.username) : short(p.address)}</strong></span><span class="chip-s ${p.active ? 'ok' : ''}">${p.active ? 'Active' : 'Joined'}</span></li>`).join('')
  } catch {}
}

// Turn on six-word sign-in from Wallet settings.
async function renderSignInSetup() {
  showPanel('Six-word sign-in')
  const panel = $('#panel')
  if (!state.username) {
    panel.innerHTML = '<p class="muted">Claim a @username first: you sign in with your @username and six words.</p><button class="primary" id="claim">Claim a username</button>'
    $('#claim').onclick = renderClaim
    return
  }
  let on = false
  try {
    on = (await api.signInStatus(state.seed)).enabled
  } catch {}
  const intro = `<p class="muted">Open your wallet on any phone or computer with <b>@${esc(state.username)}</b> and six words. Your 24 recovery words stay your backup if you ever lose these.</p>`
  if (on) {
    panel.innerHTML = `${intro}<p class="banner ok">Six-word sign-in is on.</p>
      <div class="row"><button class="ghost" id="renew">Get new words</button><button class="danger" id="off">Turn off</button></div><p class="error" id="err"></p>`
    $('#renew').onclick = () => showSignInWords()
    $('#off').onclick = async () => {
      if (!confirm('Turn off six-word sign-in? You will need your 24 recovery words on new devices.')) return
      await api.deleteSignIn(state.seed).catch((err) => ($('#err').textContent = err.message))
      renderSignInSetup()
    }
    return
  }
  panel.innerHTML = `${intro}<button class="primary" id="start">Set up six-word sign-in</button>`
  $('#start').onclick = () => showSignInWords()
}

function showSignInWords() {
  const words = newSignInWords()
  $('#panel').innerHTML = `
    <p class="muted">Write these six words down, in order. Anyone with them and your @username can open your wallet, so keep them private.</p>
    <ol class="words six">${words.map((w) => `<li>${w}</li>`).join('')}</ol>
    <label class="check"><input type="checkbox" id="saved"> I wrote down all six words</label>
    <p class="error" id="err"></p>
    <button class="primary" id="save" disabled>Turn on six-word sign-in</button>`
  $('#saved').onchange = (e) => ($('#save').disabled = !e.target.checked)
  $('#save').onclick = async () => {
    const b = $('#save')
    b.disabled = true
    b.textContent = 'Securing…'
    try {
      await api.setSignIn(state.seed, await sealSignIn(state.seed, words))
      toast('Six-word sign-in is on')
      renderSignInSetup()
    } catch (err) {
      $('#err').textContent = err.message
      b.disabled = false
      b.textContent = 'Turn on six-word sign-in'
    }
  }
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
      <h1>Recover with 24 words</h1>
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
      <div class="brand"><span class="logo" aria-label="ORPay"><span class="logo-or">OR</span><span class="logo-pay">Pay</span></span></div>
      <h1>Welcome back</h1>
      ${addr ? `<div class="who-chip"><span class="avatar" style="--hue:${parseInt(addr.slice(0, 4), 16) % 360}">${addr.slice(0, 2).toUpperCase()}</span><span><small>Your wallet</small><span class="mono">${short(addr)}</span></span></div>` : ''}
      ${bioEnabled() ? `<button class="primary bio-btn" id="bio" type="button"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 11v3M8.5 9a4 4 0 0 1 7 2.5V14a8 8 0 0 1-1 4M7 12v2a5 5 0 0 0 .5 2.2M12 4a8 8 0 0 1 8 8v2M4 14v-2a8 8 0 0 1 3-6.2"/></svg>Unlock with Face ID / fingerprint</button><p class="or-line"><span>or use your password</span></p>` : ''}
      <form id="f" class="stack">
        <label>Password
          <span class="pw-field"><input type="password" id="pw" autocomplete="current-password" ${bioEnabled() ? '' : 'autofocus'} required placeholder="Enter your password">
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
  if ($('#bio'))
    $('#bio').onclick = async () => {
      $('#err').textContent = ''
      try {
        const seed = await unlockBio()
        openWallet(seed, await addressOf(seed))
      } catch (err) {
        if (err.name !== 'NotAllowedError') $('#err').textContent = err.message
      }
    }
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
  $('#restore').onclick = renderSignIn
  $('#forget').onclick = () => {
    if (confirm('Remove this wallet from this device? You can only get it back with its recovery key.')) {
      clearVault()
      disableBio()
      renderWelcome()
    }
  }
}

// ---------- wallet ----------

// Stay unlocked across reloads in this tab until it is closed or idle for
// 15 minutes. The key lives in sessionStorage, which the browser drops when
// the tab closes and never shares with other tabs or sites.
const SESSION = 'orpay.session'
const IDLE_MS = 15 * 60 * 1000
function saveSession(seed, address) {
  try {
    sessionStorage.setItem(SESSION, JSON.stringify({ seed: toHex(seed), address, at: Date.now() }))
  } catch {}
}
function lock() {
  try {
    sessionStorage.removeItem(SESSION)
  } catch {}
  location.href = '/'
}
function resumeSession() {
  try {
    const s = JSON.parse(sessionStorage.getItem(SESSION))
    if (s && Date.now() - s.at < IDLE_MS && s.address === vaultAddress()) return s
    sessionStorage.removeItem(SESSION)
  } catch {}
  return null
}
function watchIdle() {
  const touch = () => {
    try {
      const s = JSON.parse(sessionStorage.getItem(SESSION))
      if (s) sessionStorage.setItem(SESSION, JSON.stringify({ ...s, at: Date.now() }))
    } catch {}
  }
  ;['pointerdown', 'keydown', 'scroll'].forEach((e) => addEventListener(e, debounce(touch, 1000), { passive: true }))
  setInterval(() => !resumeSession() && lock(), 30_000)
}

async function openWallet(seed, address) {
  state.seed = seed
  state.address = address
  saveSession(seed, address)
  watchIdle()
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
      else if (req.escrow) showView('escrow', () => renderEscrow(req.escrow, undefined, req.action))
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

// Scan a QR to pay: opens the Send form filled in from the code.
function scanToPay() {
  openScanner((req) => {
    if (req.escrow) return showView('escrow', () => renderEscrow(req.escrow, undefined, req.action))
    if (req.escrow_request) return showView('escrow', () => renderFundRequest(req.escrow_request))
    if (state.view !== 'home') showView('home')
    showPanel('Send money')
    renderSend(req)
  }).catch((err) => toast(err.message, 'err'))
}

// People this wallet paid or was paid by, most recent first.
function recentContacts(limit = 8) {
  const seen = new Set()
  const out = []
  for (const e of state.history) {
    const tx = e.tx
    if (tx.pool || tx.escrow || tx.kind === 'mint' || tx.kind === 'burn' || !tx.to) continue
    const other = tx.from === state.address ? tx.to : tx.from
    if (!other || other === state.address || seen.has(other)) continue
    seen.add(other)
    out.push(other)
    if (out.length === limit) break
  }
  return out
}

function openPanel(tab) {
  state.tab = tab
  showPanel(tab === 'send' ? 'Send money' : 'Receive money')
  renderPanel()
}

function homeHTML() {
  return `
      <div class="greet"><span><small id="greet-time"></small><strong id="greet-name"></strong></span>
        <button class="scan-btn" id="scan" aria-label="Scan a QR code to pay"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 8V5a1 1 0 0 1 1-1h3M16 4h3a1 1 0 0 1 1 1v3M20 16v3a1 1 0 0 1-1 1h-3M8 20H5a1 1 0 0 1-1-1v-3M4 12h16"/></svg></button></div>
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
  $('#scan').onclick = scanToPay
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
      <a class="logo" href="/?home" aria-label="ORPay home"><span class="logo-or">OR</span><span class="logo-pay">Pay</span></a>
      <span class="net" id="net"></span>
      <a class="chip ghost-chip" href="/explorer.html" target="_blank" rel="noopener" title="Explorer: see every block and transaction" aria-label="Explorer"><svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/></svg><span>Explorer</span></a>
      <button class="chip" id="me">${who ? `@${esc(who)}` : 'Claim @name'}</button>`
    $('#me').onclick = () => {
      if (state.view !== 'home') showView('home')
      renderProfile()
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
      ${recentContacts().length ? `<div class="contacts" aria-label="Recent">${recentContacts().map((a) => {
        const n = state.names.get(a)
        return `<button type="button" class="contact" data-addr="${a}"><span class="avatar" style="--hue:${parseInt(a.slice(0, 4), 16) % 360}">${esc((n ?? a).slice(0, 2).toUpperCase())}</span><small>${n ? '@' + esc(n) : short(a)}</small></button>`
      }).join('')}</div>` : ''}
      <label><span class="to-row">To <button type="button" class="link small-link" id="scan-to">Scan QR</button></span><input id="to" placeholder="@username or address" required></label>
      <p class="hint" id="to-hint"></p>
      <p class="to-trust" id="to-trust"></p>
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
  $('#scan-to').onclick = scanToPay
  app.querySelectorAll('.contact').forEach((b) => (b.onclick = () => {
    const n = state.names.get(b.dataset.addr)
    toInput.value = n ? '@' + n : b.dataset.addr
    toInput.oninput()
    $('#amount').focus()
  }))
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
    if ($('#to-trust')) $('#to-trust').innerHTML = ''
    if (!v) return
    try {
      resolved = await resolveRecipient(v)
      if (!hint.isConnected) return
      trustOf(resolved).then((t) => $('#to-trust') && ($('#to-trust').innerHTML = t ? trustChip(t) + radarBanner(riskOf(t), 'Paying for goods or work? <b>Use escrow</b> so your money is only released after you check it. Only send directly to people you know.') : ''))
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
          <button class="ghost small" id="sixwords">Six-word sign-in</button>
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
  $('#sixwords').onclick = renderSignInSetup
  $('#reveal').onclick = (e) => {
    if (confirm('Show your recovery words? Make sure nobody can see your screen.')) e.target.outerHTML = wordGrid(state.seed)
  }
  $('#lock').onclick = lock
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
      try {
        const ref = localStorage.getItem('orpay.ref')
        if (ref) api.recordReferral(state.seed, ref).catch(() => {}).finally(() => localStorage.removeItem('orpay.ref'))
      } catch {}
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
  const dayOf = (t) => {
    const d = new Date(t), today = new Date()
    const start = (x) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime()
    const diff = Math.round((start(today) - start(d)) / 86400000)
    return diff === 0 ? 'Today' : diff === 1 ? 'Yesterday' : diff < 7 ? d.toLocaleDateString([], { weekday: 'long' }) : d.toLocaleDateString([], { day: 'numeric', month: 'short', year: d.getFullYear() === today.getFullYear() ? undefined : 'numeric' })
  }
  let lastDay = ''
  ul.innerHTML = (all ? state.history : state.history.slice(0, 6))
    .map((e) => {
      const day = dayOf(e.time)
      const head = day !== lastDay ? `<li class="day">${day}</li>` : ''
      lastDay = day
      return head + activityRow(e)
    })
    .join('')
  ul.querySelectorAll('[data-pool]').forEach((li) => (li.onclick = () => showView('pools', () => renderPool(li.dataset.pool))))
  ul.querySelectorAll('[data-escrow]').forEach((li) => (li.onclick = () => showView('escrow', () => renderEscrow(li.dataset.escrow))))
}

function activityRow(e) {
  return [e].map((e) => {
      if (e.tx.pool) return poolActivity(e)
      if (e.tx.escrow) return escrowActivity(e)
      if (e.tx.kind === 'mint' || e.tx.kind === 'burn') return cashActivity(e)
      const out = e.tx.from === state.address
      const other = out ? e.tx.to : e.tx.from
      const name = state.names.get(other)
      const when = new Date(e.time).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' })
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
    logo.innerHTML = '<span class="logo-or">OR</span><span class="logo-pay">Pay</span>'
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
    <header id="header"><span class="logo" aria-label="ORPay"><span class="logo-or">OR</span><span class="logo-pay">Pay</span></span><span class="net"></span>
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
  const wantsLanding = new URLSearchParams(location.search).has('home')
  if (pending?.invoice) renderGuestCheckout(pending)
  else if (hasVault() && !wantsLanding) {
    const s = resumeSession()
    s ? openWallet(fromHex(s.seed), s.address) : renderUnlock()
  }
  else showLanding()
}

function openSaved() {
  const s = resumeSession()
  s ? openWallet(fromHex(s.seed), s.address) : renderUnlock()
}

// The public landing page. People who already have a wallet on this device
// can reach it at /?home or from the logo; its buttons lead to their wallet.
function showLanding() {
  const has = hasVault()
  renderLanding(app, {
    onStart: has ? openSaved : async () => showBackup(await newSeed()),
    onSignIn: has ? openSaved : renderSignIn,
    signInLabel: has ? 'Open wallet' : 'Sign in',
  })
  window.scrollTo(0, 0)
}
