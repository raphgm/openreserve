// Hosted checkout for partner apps: /?invoice=<id>. The customer pays the
// app's settlement address on-chain from their own wallet; the invoice turns
// paid once the payment lands, and they get a receipt.
import { api, feeFor, formatMoney, gateway, providerLabel, send, waitForCommit } from './orp.js'
import { openCheckout } from './native.js'

let ctx

export function initCheckout(c) {
  ctx = c
}

function returnLink(inv) {
  if (!inv.return_url) return ''
  try {
    const u = new URL(inv.return_url)
    if (u.protocol !== 'https:' && u.protocol !== 'http:') return ''
    u.searchParams.set('invoice', inv.id)
    u.searchParams.set('status', inv.status)
    return u.toString()
  } catch {
    return ''
  }
}

export async function renderCheckout(id, cardRef) {
  const root = ctx.root()
  root.innerHTML = '<section class="card"><p class="muted">Loading checkout…</p></section>'
  let inv
  try {
    inv = await api.invoice(id)
  } catch (err) {
    root.innerHTML = `<section class="card"><h2>Checkout not found</h2><p class="error">${ctx.esc(err.message)}</p></section>`
    return
  }
  if (inv.status === 'paid') return renderReceipt(inv)
  if (cardRef) return awaitCardPayment(inv)
  const asset = inv.asset ?? ''
  const money = (x) => formatMoney(x, asset)
  const app = inv.app ?? { name: 'Unknown merchant', status: 'unknown' }
  const verified = app.status === 'approved'
  if (verified) ctx.cobrand(app) // only approved partners get their name beside ORPay
  const expires = new Date(inv.expires_at)
  const expired = inv.status === 'expired' || expires < new Date()

  root.innerHTML = `
    <section class="card checkout">
      <p class="label">Pay</p>
      <div class="merchant">
        <span class="pool-avatar ${verified ? 'partner-bg' : ''}">${ctx.esc((verified && app.brand_name ? app.brand_name : app.name).slice(0, 1).toUpperCase())}</span>
        <span class="who">
          <strong>${ctx.esc(verified && app.brand_name ? app.brand_name : app.name)} ${verified ? '<span class="chip-s ok">Verified</span>' : '<span class="chip-s warn">Not verified</span>'}</strong>
          <small>${ctx.esc(app.website ?? '')}</small>
        </span>
      </div>
      <p class="amount">${money(inv.amount)}</p>
      ${inv.description ? `<p class="muted">${ctx.esc(inv.description)}</p>` : ''}
      <dl class="summary">
        <dt>Network fee</dt><dd>${ctx.state.status ? money(feeFor(ctx.state.status, asset)) : '…'}</dd>
        <dt>${expired ? 'Expired' : 'Expires'}</dt><dd id="expires">${expires.toLocaleString()}</dd>
      </dl>
      ${!verified ? '<p class="banner warn">This merchant is not approved on ORPay. Only pay if you trust them.</p>' : ''}
      <p class="error" id="err"></p>
      ${expired ? '<p class="banner">This checkout has expired. Ask the merchant for a new one.</p>' : `
      <button class="primary" id="pay">Pay ${money(inv.amount)} from wallet</button>
      ${inv.card_payments ? `<div class="or"><span>or</span></div>
      <label class="card-email">Email for your receipt<input id="card-email" type="email" placeholder="you@example.com"></label>
      ${(ctx.state.gateway?.providers?.length ? ctx.state.gateway.providers : ['']).map((p) => `<button class="ghost" data-card="${p}">Pay with card, bank or USSD${p ? ` (${providerLabel(p)})` : ''}</button>`).join('')}` : ''}`}
      <button class="link" id="cancel">Cancel</button>
    </section>`
  ctx.$('#cancel').onclick = () => {
    const back = returnLink({ ...inv, status: 'cancelled' })
    back ? location.assign(back) : ctx.home()
  }
  const pay = ctx.$('#pay')
  if (!pay) return
  if (!ctx.state.seed) pay.textContent = 'Pay with ORPay wallet'
  pay.onclick = async () => {
    const err = ctx.$('#err')
    err.textContent = ''
    if (!ctx.state.seed) return ctx.requireWallet() // the checkout link is kept; we return here after unlock
    const need = BigInt(inv.amount) + feeFor(ctx.state.status, asset)
    const have = BigInt(asset ? ctx.state.account?.assets?.[asset] ?? 0 : ctx.state.account?.balance ?? 0)
    if (ctx.state.account && need > have) {
      err.textContent = `Not enough in your wallet for this payment plus the fee.${inv.card_payments ? ' You can pay with card instead.' : ''}`
      return
    }
    pay.disabled = true
    pay.textContent = 'Sending…'
    try {
      const txid = await send({ seed: ctx.state.seed, to: inv.merchant, amount: BigInt(inv.amount), memo: inv.memo, asset })
      pay.textContent = 'Confirming…'
      await waitForCommit(txid)
      // The merchant's invoice turns paid once ORPay's watcher sees it.
      let latest = inv
      for (let i = 0; i < 20 && latest.status !== 'paid'; i++) {
        await new Promise((r) => setTimeout(r, 1000))
        latest = await api.invoice(id).catch(() => latest)
      }
      renderReceipt(latest.status === 'paid' ? latest : { ...inv, status: 'paid', paid_tx: txid, paid_by: ctx.state.address, paid_at: new Date().toISOString() })
    } catch (e) {
      err.textContent = e.message
      pay.disabled = false
      pay.textContent = `Pay ${money(inv.amount)} from wallet`
    }
  }
  ctx.root().querySelectorAll('[data-card]').forEach((card) => {
    const label = card.textContent
    card.onclick = async () => {
      const email = ctx.$('#card-email').value.trim()
      if (!/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(email)) {
        ctx.$('#err').textContent = 'Enter your email for the payment receipt.'
        return
      }
      card.disabled = true
      card.textContent = 'Opening checkout…'
      try {
        const r = await gateway.cardPay(inv.id, email, card.dataset.card)
        if (await openCheckout(r.authorization_url)) awaitCardPayment(inv)
      } catch (e) {
        ctx.$('#err').textContent = e.message
        card.disabled = false
        card.textContent = label
      }
    }
  })
}

// Back from the provider: wait for the gateway to settle the card payment.
async function awaitCardPayment(inv) {
  const root = ctx.root()
  root.innerHTML = `
    <section class="card receipt">
      <div class="spinner" aria-hidden="true"></div>
      <h2>Confirming your payment…</h2>
      <p class="muted" id="msg">Waiting for the payment provider to confirm.</p>
    </section>`
  for (let i = 0; i < 60; i++) {
    const latest = await api.invoice(inv.id).catch(() => null)
    if (latest?.status === 'paid') return renderReceipt(latest)
    await new Promise((r) => setTimeout(r, 2000))
  }
  const msg = ctx.$('#msg')
  if (msg) msg.textContent = 'Still waiting. If you completed payment, the merchant will be notified once it is confirmed.'
}

function renderReceipt(inv) {
  const root = ctx.root()
  const app = inv.app ?? { name: 'Merchant' }
  if (app.status === 'approved') ctx.cobrand(app)
  const back = returnLink(inv)
  root.innerHTML = `
    <section class="card receipt">
      <div class="receipt-check" aria-hidden="true">✓</div>
      <h2>Payment complete</h2>
      <p class="amount">${formatMoney(inv.amount, inv.asset ?? '')}</p>
      <dl class="summary">
        <dt>Paid to</dt><dd>${ctx.esc(app.brand_name || app.name)}</dd>
        ${inv.description ? `<dt>For</dt><dd>${ctx.esc(inv.description)}</dd>` : ''}
        <dt>Date</dt><dd>${new Date(inv.paid_at).toLocaleString()}</dd>
        ${inv.height ? `<dt>Block</dt><dd>${inv.height}</dd>` : ''}
        <dt>Transaction</dt><dd class="mono">${ctx.short(inv.paid_tx ?? '')}</dd>
        <dt>Receipt no.</dt><dd class="mono">${ctx.esc(inv.id)}</dd>
      </dl>
      <div class="row">
        ${back ? `<a class="button primary" href="${ctx.esc(back)}">Return to ${ctx.esc(app.brand_name || app.name)}</a>` : '<button class="primary" id="done">Done</button>'}
        <button class="ghost" id="print">Save receipt</button>
      </div>
    </section>`
  ctx.$('#print').onclick = () => window.print()
  const done = ctx.$('#done')
  if (done) done.onclick = ctx.home
}
