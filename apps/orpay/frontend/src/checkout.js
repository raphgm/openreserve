// Hosted checkout for partner apps: /?invoice=<id>. The customer pays the
// app's settlement address on-chain from their own wallet; the invoice turns
// paid once the payment lands, and they get a receipt.
import { api, formatAmount, send, waitForCommit } from './orp.js'

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

export async function renderCheckout(id) {
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
  const app = inv.app ?? { name: 'Unknown merchant', status: 'unknown' }
  const verified = app.status === 'approved'
  const expires = new Date(inv.expires_at)
  const expired = inv.status === 'expired' || expires < new Date()

  root.innerHTML = `
    <section class="card checkout">
      <p class="label">Pay</p>
      <div class="merchant">
        <span class="pool-avatar">${ctx.esc(app.name.slice(0, 1).toUpperCase())}</span>
        <span class="who">
          <strong>${ctx.esc(app.name)} ${verified ? '<span class="chip-s ok">Verified</span>' : '<span class="chip-s warn">Not verified</span>'}</strong>
          <small>${ctx.esc(app.website ?? '')}</small>
        </span>
      </div>
      <p class="amount">${formatAmount(inv.amount)} <span class="unit">ORP</span></p>
      ${inv.description ? `<p class="muted">${ctx.esc(inv.description)}</p>` : ''}
      <dl class="summary">
        <dt>Network fee</dt><dd>${ctx.state.status ? formatAmount(ctx.state.status.min_fee) : '…'} ORP</dd>
        <dt>${expired ? 'Expired' : 'Expires'}</dt><dd id="expires">${expires.toLocaleString()}</dd>
      </dl>
      ${!verified ? '<p class="banner warn">This merchant is not approved on ORPay. Only pay if you trust them.</p>' : ''}
      <p class="error" id="err"></p>
      ${expired ? '<p class="banner">This checkout has expired. Ask the merchant for a new one.</p>' : `<button class="primary" id="pay">Pay ${formatAmount(inv.amount)} ORP</button>`}
      <button class="link" id="cancel">Cancel</button>
    </section>`
  ctx.$('#cancel').onclick = () => {
    const back = returnLink({ ...inv, status: 'cancelled' })
    back ? location.assign(back) : ctx.home()
  }
  const pay = ctx.$('#pay')
  if (!pay) return
  pay.onclick = async () => {
    const err = ctx.$('#err')
    err.textContent = ''
    const need = BigInt(inv.amount) + BigInt(ctx.state.status?.min_fee ?? 0)
    if (ctx.state.account && need > BigInt(ctx.state.account.balance)) {
      err.textContent = 'Not enough ORP for this payment plus the fee.'
      return
    }
    pay.disabled = true
    pay.textContent = 'Sending…'
    try {
      const txid = await send({ seed: ctx.state.seed, to: inv.merchant, amount: BigInt(inv.amount), memo: inv.memo })
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
      pay.textContent = `Pay ${formatAmount(inv.amount)} ORP`
    }
  }
}

function renderReceipt(inv) {
  const root = ctx.root()
  const app = inv.app ?? { name: 'Merchant' }
  const back = returnLink(inv)
  root.innerHTML = `
    <section class="card receipt">
      <div class="receipt-check" aria-hidden="true">✓</div>
      <h2>Payment complete</h2>
      <p class="amount">${formatAmount(inv.amount)} <span class="unit">ORP</span></p>
      <dl class="summary">
        <dt>Paid to</dt><dd>${ctx.esc(app.name)}</dd>
        ${inv.description ? `<dt>For</dt><dd>${ctx.esc(inv.description)}</dd>` : ''}
        <dt>Date</dt><dd>${new Date(inv.paid_at).toLocaleString()}</dd>
        ${inv.height ? `<dt>Block</dt><dd>${inv.height}</dd>` : ''}
        <dt>Transaction</dt><dd class="mono">${ctx.short(inv.paid_tx ?? '')}</dd>
        <dt>Receipt no.</dt><dd class="mono">${ctx.esc(inv.id)}</dd>
      </dl>
      <div class="row">
        ${back ? `<a class="button primary" href="${ctx.esc(back)}">Return to ${ctx.esc(app.name)}</a>` : '<button class="primary" id="done">Done</button>'}
        <button class="ghost" id="print">Save receipt</button>
      </div>
    </section>`
  ctx.$('#print').onclick = () => window.print()
  const done = ctx.$('#done')
  if (done) done.onclick = ctx.home
}
