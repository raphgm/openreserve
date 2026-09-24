// Cash in and out through the gateway's money providers (Paystack,
// Flutterwave, ...). Add money: pay by card, bank transfer or USSD; naira
// arrives as NGN once the provider confirms. Withdraw: NGN is destroyed
// on-chain and the provider pays the bank.
import { formatMoney, gateway, parseAmount, providerLabel, send, waitForCommit } from './orp.js'

let ctx

export function initMoney(c) {
  ctx = c
}

const EMAIL_KEY = 'orpay.email'
const savedEmail = () => {
  try {
    return localStorage.getItem(EMAIL_KEY) ?? ''
  } catch {
    return ''
  }
}

// A segmented picker when the network offers more than one money provider.
function providerPicker(onChange) {
  const list = ctx.state.gateway?.providers ?? []
  let current = ctx.state.gateway?.default_provider ?? list[0] ?? ''
  const html = list.length > 1
    ? `<div class="seg" role="radiogroup" aria-label="Payment provider">${list.map((p) => `<button type="button" role="radio" data-prov="${p}">${providerLabel(p)}</button>`).join('')}</div>`
    : ''
  const bind = () => {
    const set = (p) => {
      current = p
      ctx.root().querySelectorAll('[data-prov]').forEach((b) => b.setAttribute('aria-checked', b.dataset.prov === p))
      onChange?.(p)
    }
    ctx.root().querySelectorAll('[data-prov]').forEach((b) => (b.onclick = () => set(b.dataset.prov)))
    set(current)
  }
  return { html, bind, get: () => current }
}

function nairaAmount(v) {
  const amt = parseAmount(v.replace(/,/g, ''))
  if (amt % 10_000n !== 0n) throw new Error('Naira amounts can have at most 2 decimals.')
  return amt
}

export function renderAddMoney() {
  const root = ctx.root()
  const prov = providerPicker()
  root.innerHTML = `
    <section class="card">
      <button class="link back" id="back">← Home</button>
      <h2>Add money</h2>
      <p class="muted small-text">Pay with card, bank transfer or USSD. The naira arrives in your wallet as soon as the payment is confirmed.</p>
      <form id="f" class="stack" autocomplete="off">
        ${prov.html}
        <label>Amount (₦)<input id="amount" inputmode="decimal" placeholder="5,000" required></label>
        <div class="quick">${['1000', '5000', '10000', '50000'].map((a) => `<button type="button" class="ghost small" data-q="${a}">₦${Number(a).toLocaleString()}</button>`).join('')}</div>
        <label>Email for your receipt<input id="email" type="email" value="${ctx.esc(savedEmail())}" required></label>
        <p class="hint">The provider's processing fee is added at checkout, so the full amount reaches your wallet.</p>
        ${ctx.state.gateway?.test_mode ? '<p class="banner warn">Test mode: use the provider\'s test cards. No real money moves.</p>' : ''}
        <p class="error" id="err"></p>
        <button class="primary" id="go">Continue</button>
      </form>
    </section>`
  ctx.$('#back').onclick = ctx.home
  prov.bind()
  root.querySelectorAll('[data-q]').forEach((b) => (b.onclick = () => (ctx.$('#amount').value = b.dataset.q)))
  ctx.$('#f').onsubmit = async (e) => {
    e.preventDefault()
    const btn = ctx.$('#go')
    const err = ctx.$('#err')
    err.textContent = ''
    try {
      const amount = nairaAmount(ctx.$('#amount').value)
      const email = ctx.$('#email').value.trim()
      try {
        localStorage.setItem(EMAIL_KEY, email)
      } catch {}
      btn.disabled = true
      btn.textContent = `Opening ${providerLabel(prov.get())}…`
      const res = await ctx.gatewayCall(() =>
        gateway.deposit(ctx.state.seed, (Number(amount / 10_000n) / 100).toFixed(2), email, prov.get()),
      )
      location.assign(res.authorization_url)
    } catch (e2) {
      err.textContent = e2.message
      btn.disabled = false
      btn.textContent = 'Continue'
    }
  }
}

// Called when the provider redirects back with ?deposit=<ref>.
export async function confirmDeposit(ref) {
  const root = ctx.root()
  root.innerHTML = `
    <section class="card receipt">
      <div class="spinner" aria-hidden="true"></div>
      <h2>Confirming your payment…</h2>
      <p class="muted" id="msg">Waiting for the payment provider. This usually takes a few seconds.</p>
    </section>`
  for (let i = 0; i < 60; i++) {
    let d
    try {
      d = await gateway.deposit_status(ref)
    } catch {}
    if (d?.status === 'minted') {
      root.innerHTML = `
        <section class="card receipt">
          <div class="receipt-check" aria-hidden="true">✓</div>
          <h2>Money added</h2>
          <p class="amount">${formatMoney(d.net, 'NGN')}</p>
          <p class="muted small-text">Reference ${ctx.esc(ref)}</p>
          <button class="primary" id="done">Done</button>
        </section>`
      ctx.$('#done').onclick = ctx.home
      ctx.refresh()
      return
    }
    if (d?.status === 'failed') {
      root.innerHTML = `<section class="card"><h2>Payment not completed</h2><p class="error">${ctx.esc(d.error || 'The payment did not go through.')}</p><button class="primary" id="done">Back</button></section>`
      ctx.$('#done').onclick = ctx.home
      return
    }
    await new Promise((r) => setTimeout(r, 2000))
  }
  const msg = ctx.$('#msg')
  if (msg) msg.textContent = "Still waiting for confirmation. You can leave this page; the money will appear in your wallet once it's confirmed."
}

export async function renderWithdraw() {
  const root = ctx.root()
  const prov = providerPicker((p) => loadBanks(p))
  const bal = BigInt(ctx.state.account?.assets?.NGN ?? 0)
  const fee = BigInt(ctx.state.gateway?.withdraw_fee ?? 0)
  root.innerHTML = `
    <section class="card">
      <button class="link back" id="back">← Home</button>
      <h2>Withdraw to bank</h2>
      <p class="muted small-text">Available: ${formatMoney(bal, 'NGN')}. A ${formatMoney(fee, 'NGN')} transfer fee is deducted.</p>
      <form id="f" class="stack" autocomplete="off">
        ${prov.html}
        <label>Amount (₦)<input id="amount" inputmode="decimal" required></label>
        <label>Bank<select id="bank" required><option value="">Loading banks…</option></select></label>
        <label>Account number<input id="acct" inputmode="numeric" maxlength="10" pattern="\\d{10}" required></label>
        <p class="hint" id="name"></p>
        <p class="error" id="err"></p>
        <button class="primary" id="go" disabled>Withdraw</button>
      </form>
      <h2 class="spaced">Recent withdrawals</h2>
      <ul class="activity" id="wds"><li class="empty">None yet.</li></ul>
    </section>`
  ctx.$('#back').onclick = ctx.home
  loadWithdrawals()
  async function loadBanks(p) {
    const sel = ctx.$('#bank')
    if (!sel) return
    sel.innerHTML = '<option value="">Loading banks…</option>'
    try {
      const banks = await gateway.banks(p)
      sel.innerHTML =
        '<option value="">Choose your bank</option>' + banks.map((b) => `<option value="${ctx.esc(b.code)}">${ctx.esc(b.name)}</option>`).join('')
    } catch (e) {
      ctx.$('#err').textContent = e.message
    }
    ctx.$('#name').textContent = ''
  }
  prov.bind()
  let resolved = ''
  const lookup = async () => {
    resolved = ''
    ctx.$('#go').disabled = true
    const acct = ctx.$('#acct').value.trim()
    const bank = ctx.$('#bank').value
    ctx.$('#name').textContent = ''
    if (!/^\d{10}$/.test(acct) || !bank) return
    ctx.$('#name').textContent = 'Checking account…'
    try {
      const r = await gateway.resolve(acct, bank, prov.get())
      resolved = r.account_name
      ctx.$('#name').innerHTML = `Account name: <strong>${ctx.esc(resolved)}</strong>`
      ctx.$('#go').disabled = false
    } catch (e) {
      ctx.$('#name').textContent = e.message
    }
  }
  ctx.$('#acct').oninput = lookup
  ctx.$('#bank').onchange = lookup

  ctx.$('#f').onsubmit = async (e) => {
    e.preventDefault()
    const err = ctx.$('#err')
    const btn = ctx.$('#go')
    err.textContent = ''
    try {
      const amount = nairaAmount(ctx.$('#amount').value)
      const txFee = BigInt((ctx.state.status.assets ?? []).find((a) => a.symbol === 'NGN')?.min_fee ?? 0)
      if (amount + txFee > bal) throw new Error('Not enough naira for this withdrawal.')
      if (!confirm(`Send ${formatMoney(amount - fee, 'NGN')} to ${resolved}? This cannot be undone.`)) return
      btn.disabled = true
      btn.textContent = 'Preparing…'
      const q = await gateway.withdraw(ctx.state.seed, {
        amount: (Number(amount / 10_000n) / 100).toFixed(2),
        bank_code: ctx.$('#bank').value,
        account_number: ctx.$('#acct').value.trim(),
        provider: prov.get(),
      })
      btn.textContent = 'Confirming…'
      // Destroy the naira on-chain; the gateway pays the bank once it sees this.
      const id = await send({ seed: ctx.state.seed, kind: 'burn', asset: 'NGN', amount: BigInt(q.burn.amount), memo: q.burn.memo, to: '' })
      await waitForCommit(id)
      ctx.toast('Withdrawal on its way to your bank')
      ctx.refresh()
      renderWithdraw()
    } catch (e2) {
      err.textContent = e2.message
      btn.disabled = false
      btn.textContent = 'Withdraw'
    }
  }
}

async function loadWithdrawals() {
  let list = []
  try {
    list = await gateway.withdrawals(ctx.state.seed)
  } catch {
    return
  }
  const ul = ctx.$('#wds')
  if (!ul || !list.length) return
  const label = { awaiting_burn: 'Waiting', processing: 'Sending to bank', paid: 'Paid', refunded: 'Refunded', expired: 'Expired' }
  const chip = { paid: 'ok', refunded: 'warn', processing: 'info' }
  ul.innerHTML = list
    .sort((a, b) => b.created_at.localeCompare(a.created_at))
    .slice(0, 10)
    .map(
      (w) => `
      <li>
        <span class="icon out" aria-hidden="true">↗</span>
        <span class="who"><strong>${ctx.esc(w.bank_name)} · ${ctx.esc(w.account_number.slice(-4).padStart(10, '•'))}</strong>
          <small>${new Date(w.created_at).toLocaleString()}${w.error ? ` · ${ctx.esc(w.error)}` : ''}</small></span>
        <span class="amt"><span class="chip-s ${chip[w.status] ?? ''}">${label[w.status] ?? w.status}</span><br>${formatMoney(BigInt(w.payout_kobo) * 10_000n, 'NGN')}</span>
      </li>`,
    )
    .join('')
}
