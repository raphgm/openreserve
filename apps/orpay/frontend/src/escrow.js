// Smart escrow: funds locked on-chain between a buyer and a seller, released
// in milestones by the buyer, with an independent arbiter for disputes and
// timeouts so money is never stuck. Everything shown is read from the chain.
import { api, assetLabel, escrowOp, formatMoney, node, parseAmount, waitForCommit } from './orp.js'

let ctx

export function initEscrow(c) {
  ctx = c
}

const DAY = 86_400_000
const statusChip = (s) =>
  ({
    funded: '<span class="chip-s info">Funds locked</span>',
    dispatched: '<span class="chip-s info">In progress</span>',
    disputed: '<span class="chip-s bad">Disputed</span>',
    completed: '<span class="chip-s ok">Completed</span>',
    refunded: '<span class="chip-s">Refunded</span>',
    resolved: '<span class="chip-s">Resolved</span>',
  })[s] ?? s

const label = (a) => {
  if (a === ctx.state.address) return 'You'
  const n = ctx.nameOf(a)
  return n ? `@${ctx.esc(n)}` : `<span class="mono">${ctx.short(a)}</span>`
}
const roleOf = (e) => (e.buyer === ctx.state.address ? 'buyer' : e.seller === ctx.state.address ? 'seller' : e.arbiter === ctx.state.address ? 'arbiter' : 'viewer')
const total = (e) => e.milestones.reduce((s, m) => s + BigInt(m), 0n)
const when = (ms) => new Date(ms).toLocaleString([], { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' })

export async function renderEscrows() {
  const root = ctx.root()
  root.innerHTML = `
    <section class="card">
      <div class="section-head"><h2>Escrow</h2><button class="primary small" id="new">New escrow</button></div>
      <p class="muted small-text">Lock payment until the work or goods are delivered. The seller is paid in milestones as you approve, and an independent arbiter settles disputes.</p>
      <ul class="pool-list" id="list"><li class="empty">Loading…</li></ul>
    </section>`
  ctx.$('#new').onclick = renderCreate
  let list
  try {
    list = await node.escrows(ctx.state.address)
  } catch (err) {
    ctx.$('#list').innerHTML = `<li class="empty">${ctx.esc(err.message)}</li>`
    return
  }
  await ctx.resolveNames(list.flatMap((e) => [e.buyer, e.seller, e.arbiter]))
  const ul = ctx.$('#list')
  if (!ul) return
  if (!list.length) {
    ul.innerHTML = '<li class="empty">No escrows yet.</li>'
    return
  }
  ul.innerHTML = list
    .map((e) => {
      const role = roleOf(e)
      const other = role === 'buyer' ? e.seller : e.buyer
      const needsMe =
        (role === 'buyer' && (e.status === 'funded' || e.status === 'dispatched')) ||
        (role === 'seller' && e.status === 'funded') ||
        (role === 'arbiter' && e.status === 'disputed')
      return `
        <li><button class="pool-row" data-id="${e.id}">
          <span class="pool-avatar escrow-avatar">⛨</span>
          <span class="who">
            <strong>${e.ref ? `#${ctx.esc(e.ref)}` : 'Escrow'} · ${formatMoney(total(e), e.asset ?? '')}</strong>
            <small>${role === 'arbiter' ? 'You arbitrate' : `${role === 'buyer' ? 'To' : 'From'} ${label(other)}`} · ${e.released}/${e.milestones.length} released</small>
          </span>
          ${needsMe ? '<span class="chip-s warn">Action needed</span>' : statusChip(e.status)}
        </button></li>`
    })
    .join('')
  ul.querySelectorAll('[data-id]').forEach((b) => (b.onclick = () => renderEscrow(b.dataset.id)))
}

function renderCreate() {
  const root = ctx.root()
  const curs = ctx.currencies()
  root.innerHTML = `
    <section class="card">
      <button class="link back" id="back">← Escrow</button>
      <h2>New escrow</h2>
      <p class="muted small-text">You are the buyer: your funds are locked when you create it.</p>
      <form id="f" class="stack" autocomplete="off">
        <label>Seller<input id="seller" placeholder="@username or address" required></label>
        <label>Arbiter <span class="muted">(settles disputes; not you or the seller)</span><input id="arbiter" placeholder="@username or address" required></label>
        ${curs.length > 1 ? `<div class="seg" role="radiogroup" aria-label="Currency">${curs.map((c) => `<button type="button" role="radio" data-cur="${c}">${assetLabel(c)}</button>`).join('')}</div>` : ''}
        <div class="stack" id="ms"></div>
        <button type="button" class="ghost small" id="add-ms">+ Add milestone</button>
        <div class="row">
          <label>Ship / deliver within (days)<input id="ship" type="number" min="1" max="365" value="7" required></label>
          <label>Review period (days)<input id="review" type="number" min="1" max="90" value="3" required></label>
        </div>
        <label>Reference <span class="muted">(optional)</span><input id="ref" maxlength="64" placeholder="e.g. PN-8291-X"></label>
        <p class="hint" id="total"></p>
        <p class="error" id="err"></p>
        <button class="primary" id="go">Lock funds</button>
      </form>
    </section>`
  ctx.$('#back').onclick = renderEscrows
  let asset = curs[0]
  const setAsset = (a) => {
    asset = a
    root.querySelectorAll('[data-cur]').forEach((b) => b.setAttribute('aria-checked', b.dataset.cur === a))
    updateTotal()
  }
  const addMilestone = (v = '') => {
    const n = ctx.$('#ms').children.length + 1
    const row = document.createElement('label')
    row.innerHTML = `Milestone ${n} amount<input class="ms" inputmode="decimal" value="${ctx.esc(v)}" required>`
    ctx.$('#ms').append(row)
    row.querySelector('input').oninput = updateTotal
  }
  function updateTotal() {
    try {
      const sum = [...root.querySelectorAll('.ms')].reduce((s, i) => s + (i.value ? parseAmount(i.value.replace(/,/g, '')) : 0n), 0n)
      ctx.$('#total').textContent = `Total locked: ${formatMoney(sum, asset)}`
    } catch {
      ctx.$('#total').textContent = ''
    }
  }
  root.querySelectorAll('[data-cur]').forEach((b) => (b.onclick = () => setAsset(b.dataset.cur)))
  ctx.$('#add-ms').onclick = () => {
    if (ctx.$('#ms').children.length < 10) addMilestone()
  }
  addMilestone()
  setAsset(asset)

  ctx.$('#f').onsubmit = async (e) => {
    e.preventDefault()
    const err = ctx.$('#err')
    const btn = ctx.$('#go')
    err.textContent = ''
    try {
      const seller = await ctx.resolveRecipient(ctx.$('#seller').value.trim())
      const arbiter = await ctx.resolveRecipient(ctx.$('#arbiter').value.trim())
      if (seller === ctx.state.address) throw new Error("You can't be the seller of your own escrow.")
      if (arbiter === ctx.state.address || arbiter === seller) throw new Error('The arbiter must be someone other than you and the seller.')
      const milestones = [...root.querySelectorAll('.ms')].map((i) => {
        const v = parseAmount(i.value.replace(/,/g, ''))
        if (v === 0n) throw new Error('Milestone amounts must be more than 0.')
        if (asset === 'NGN' && v % 10_000n !== 0n) throw new Error('Naira amounts can have at most 2 decimals.')
        return v
      })
      const shipDays = Number(ctx.$('#ship').value)
      const reviewDays = Number(ctx.$('#review').value)
      btn.disabled = true
      btn.textContent = 'Locking…'
      const id = await escrowOp({
        seed: ctx.state.seed, op: 'create', seller, arbiter, milestones, asset,
        shipBy: Date.now() + shipDays * DAY, reviewSecs: reviewDays * 86_400, ref: ctx.$('#ref').value.trim(),
      })
      await waitForCommit(id)
      ctx.toast('Funds locked in escrow')
      ctx.refresh()
      renderEscrow(id)
    } catch (e2) {
      err.textContent = e2.message
      btn.disabled = false
      btn.textContent = 'Lock funds'
    }
  }
}

let watch = { timer: null }

export async function renderEscrow(id, preloaded) {
  const root = ctx.root()
  if (!preloaded) root.innerHTML = '<section class="card"><p class="muted">Loading escrow…</p></section>'
  let data = preloaded
  try {
    data ??= await node.escrow(id)
  } catch (err) {
    root.innerHTML = `<section class="card"><button class="link back" id="back">← Escrow</button><p class="error">${ctx.esc(err.message)}</p></section>`
    ctx.$('#back').onclick = renderEscrows
    return
  }
  const { escrow: e, history } = data
  await ctx.resolveNames([e.buyer, e.seller, e.arbiter])
  // Escrows created from a partner app's request carry milestone labels.
  if (e.ref?.startsWith('esr_') && !data.request) data.request = await api.escrowRequest(e.ref).catch(() => null)
  const req = data.request
  if (req?.app?.status === 'approved') ctx.cobrand(req.app)
  else ctx.cobrand(null)
  const msLabel = (i) => ctx.esc(req?.milestones?.[i]?.label ?? `Milestone ${i + 1}`)
  const money = (x) => formatMoney(x, e.asset ?? '')
  const role = roleOf(e)
  const now = Date.now()
  const reviewEnds = e.dispatched_at ? e.dispatched_at + e.review_secs * 1000 : 0
  const open = ['funded', 'dispatched', 'disputed'].includes(e.status)
  const done = !open

  // The four stages of the design, derived from on-chain state.
  const stage = (state, title, sub, detail) => `
    <li class="step ${state}">
      <span class="step-dot" aria-hidden="true">${state === 'done' ? '✓' : state === 'active' ? '●' : '🔒'}</span>
      <span class="who"><strong>${title}</strong><small>${sub}</small>${detail ? `<small class="muted">${detail}</small>` : ''}</span>
      <span class="step-tag">${{ done: 'Verified ✓', active: 'Action required', locked: 'Locked' }[state]}</span>
    </li>`
  const shipped = !!e.dispatched_at
  const steps = [
    stage('done', 'Deposit locked', `${label(e.buyer)} secured ${money(total(e))} in escrow`, when(e.created_at)),
    stage(shipped ? 'done' : open ? 'active' : 'locked', shipped ? 'Dispatched / delivered' : 'Awaiting dispatch',
      shipped ? `${label(e.seller)} marked it dispatched` : `${label(e.seller)} ships or delivers by ${when(e.ship_by)}`,
      e.tracking ? `Tracking: ${ctx.esc(e.tracking)}` : ''),
    stage(e.released === e.milestones.length || e.status === 'completed' ? 'done' : open && e.status !== 'disputed' ? 'active' : 'locked',
      'Buyer approval', `${e.released} of ${e.milestones.length} milestones released`,
      shipped && e.status === 'dispatched' ? `Auto-release to seller after ${when(reviewEnds)} if no dispute` : ''),
    stage(done ? 'done' : 'locked', { completed: 'Completed', refunded: 'Refunded', resolved: 'Resolved by arbiter' }[e.status] ?? 'Completed',
      done ? `${money(e.paid_seller)} to seller · ${money(e.paid_buyer)} back to buyer` : 'Funds released directly to the seller'),
  ].join('')

  const ms = e.milestones
    .map((m, i) => `<li class="${i < e.released ? 'paid' : ''}"><span>${msLabel(i)}</span><strong>${money(m)}</strong><span class="${i < e.released ? 'tick' : 'pending'}">${i < e.released ? 'Released' : 'Held'}</span></li>`)
    .join('')

  // What this person can do right now.
  const acts = []
  if (role === 'buyer' && (e.status === 'funded' || e.status === 'dispatched')) {
    acts.push(`<button class="primary" data-op="release">Release “${msLabel(e.released)}” · ${money(e.milestones[e.released])}</button>`)
    acts.push('<button class="danger" data-op="dispute">Open a dispute</button>')
    if (e.status === 'funded' && now > e.ship_by) acts.push('<button class="ghost" data-op="refund">Reclaim my funds (not shipped in time)</button>')
  }
  if (role === 'seller' && e.status === 'funded') {
    acts.push('<label>Tracking number or delivery note<input id="note" maxlength="140" placeholder="e.g. GIG Logistics #GL12345 or link to delivered work"></label>')
    acts.push('<button class="primary" data-op="dispatch">Mark dispatched / delivered</button>')
  }
  if (role === 'seller' && e.status === 'dispatched') {
    acts.push(now >= reviewEnds
      ? `<button class="primary" data-op="claim">Claim remaining ${money(e.balance)}</button>`
      : `<p class="muted small-text">The buyer is reviewing. If they neither release nor dispute by ${when(reviewEnds)}, you can claim the rest.</p>`)
  }
  if (role === 'seller' && open) {
    if (e.status !== 'disputed') acts.push('<button class="danger" data-op="dispute">Open a dispute</button>')
    acts.push('<button class="ghost" data-op="refund">Refund the buyer</button>')
  }
  if (role === 'arbiter' && e.status === 'disputed') {
    acts.push(`<label>Seller's share of ${money(e.balance)}<input id="split" inputmode="decimal" placeholder="0"></label>
      <p class="hint" id="split-hint">The rest goes back to the buyer.</p>
      <button class="primary" data-op="resolve">Resolve dispute</button>`)
  }
  if (e.status === 'disputed' && role !== 'arbiter') acts.push(`<p class="banner warn">This escrow is frozen. ${label(e.arbiter)} will decide how the ${money(e.balance)} is split.</p>`)
  if (!acts.length) acts.push(`<p class="muted small-text">${done ? 'This escrow is closed.' : 'Nothing for you to do right now.'}</p>`)

  root.innerHTML = `
    <section class="card escrow-hero" data-escrow-id="${e.id}">
      <button class="link back light" id="back">← Escrow</button>
      <div class="pool-title"><h2>${req?.app ? ctx.esc(req.app.brand_name || req.app.name) : e.ref ? `#${ctx.esc(e.ref)}` : 'Escrow'}</h2>${statusChip(e.status)}</div>
      ${req?.description ? `<p class="hero-sub">${ctx.esc(req.description)}</p>` : ''}
      <p class="label">${open ? 'Locked in escrow' : 'Escrow total'}</p>
      <p class="amount">${money(open ? e.balance : total(e))}</p>
      <div class="pool-stats">
        <div><span>Buyer</span><strong>${label(e.buyer)}</strong></div>
        <div><span>Seller</span><strong>${label(e.seller)}</strong></div>
        <div><span>Arbiter</span><strong>${label(e.arbiter)}</strong></div>
      </div>
    </section>
    <section class="card"><div class="stack" id="actions">${acts.join('')}</div><p class="error" id="err"></p></section>
    <section class="card"><h2>Progress</h2><ol class="steps">${steps}</ol></section>
    <section class="card"><h2>Milestones</h2><ul class="milestones">${ms}</ul></section>
    <section class="card"><h2>On-chain record</h2><ul class="timeline">${history
      .slice()
      .reverse()
      .map((h) => {
        const op = h.tx.escrow.op
        const who = label(h.tx.from)
        const text = {
          create: `${who} locked ${money(total(e))}`,
          dispatch: `${who} marked dispatched${h.tx.memo ? ` · ${ctx.esc(h.tx.memo)}` : ''}`,
          release: `${who} released a milestone`,
          dispute: `${who} opened a dispute`,
          resolve: `${who} resolved the dispute: ${money(h.tx.escrow.to_seller ?? 0)} to seller`,
          refund: `${who} refunded the buyer`,
          claim: `${who} claimed after the review period`,
        }[op]
        return `<li class="ev-${op}"><span class="ev-dot"></span><span class="who"><strong>${text}</strong><small>${when(h.time)} · block ${h.height} · tx ${ctx.short(h.id)}</small></span></li>`
      })
      .join('')}</ul></section>`

  ctx.$('#back').onclick = renderEscrows
  const split = ctx.$('#split')
  if (split)
    split.oninput = () => {
      try {
        const v = parseAmount(split.value || '0')
        ctx.$('#split-hint').textContent = v > BigInt(e.balance) ? 'More than the escrow holds.' : `Buyer gets ${money(BigInt(e.balance) - v)} back.`
      } catch {
        ctx.$('#split-hint').textContent = 'Enter an amount.'
      }
    }
  root.querySelectorAll('[data-op]').forEach((b) => {
    b.onclick = async () => {
      const op = b.dataset.op
      const confirmText = {
        release: `Release ${money(e.milestones[e.released])} to the seller? This cannot be undone.`,
        refund: 'Return the remaining funds to the buyer?',
        dispute: 'Freeze this escrow and ask the arbiter to decide?',
        resolve: 'Resolve this dispute with the split you entered?',
      }[op]
      if (confirmText && !confirm(confirmText)) return
      const text = b.textContent
      b.disabled = true
      b.textContent = 'Confirming…'
      try {
        const args = { seed: ctx.state.seed, op, id: e.id, asset: e.asset ?? '' }
        if (op === 'dispatch') args.memo = ctx.$('#note')?.value.trim() ?? ''
        if (op === 'resolve') args.toSeller = parseAmount(ctx.$('#split').value || '0')
        await waitForCommit(await escrowOp(args))
        ctx.toast({ release: 'Milestone released', dispatch: 'Marked as dispatched', dispute: 'Dispute opened', resolve: 'Dispute resolved', refund: 'Buyer refunded', claim: 'Funds claimed' }[op])
        ctx.refresh()
        renderEscrow(e.id)
      } catch (err) {
        ctx.$('#err').textContent = err.message
        b.disabled = false
        b.textContent = text
      }
    }
  })

  // Live updates while this escrow is on screen.
  clearInterval(watch.timer)
  const snap = JSON.stringify(data)
  watch.timer = setInterval(async () => {
    if (ctx.state.view !== 'escrow' || !ctx.root()?.querySelector(`[data-escrow-id="${e.id}"]`)) return clearInterval(watch.timer)
    if (ctx.root().querySelector('[data-op]:disabled') || document.activeElement?.matches('input')) return
    try {
      const d = await node.escrow(e.id)
      if (JSON.stringify({ ...d, request: data.request }) !== snap) renderEscrow(e.id, { ...d, request: data.request })
    } catch {}
  }, 3000)
}

// Funding page for a partner app's escrow request: /?escrow_request=<id>.
export async function renderFundRequest(id) {
  const root = ctx.root()
  root.innerHTML = '<section class="card"><p class="muted">Loading…</p></section>'
  let r
  try {
    r = await api.escrowRequest(id)
  } catch (err) {
    root.innerHTML = `<section class="card"><h2>Escrow request not found</h2><p class="error">${ctx.esc(err.message)}</p></section>`
    return
  }
  if (r.escrow_id) return renderEscrow(r.escrow_id)
  await ctx.resolveNames([r.seller, r.arbiter])
  const money = (x) => formatMoney(x, r.asset ?? '')
  const app = r.app ?? { name: 'A partner app', status: 'unknown' }
  if (app.status === 'approved') ctx.cobrand(app)
  const expired = r.status === 'expired' || new Date(r.expires_at) < new Date()
  root.innerHTML = `
    <section class="card checkout">
      <p class="label">Fund escrow</p>
      <div class="merchant">
        <span class="pool-avatar escrow-avatar ${app.status === 'approved' ? 'partner-bg' : ''}">⛨</span>
        <span class="who"><strong>${ctx.esc(app.brand_name || app.name)} ${app.status === 'approved' ? '<span class="chip-s ok">Verified</span>' : '<span class="chip-s warn">Not verified</span>'}</strong>
        <small>${ctx.esc(r.description || 'Payment held in escrow until milestones are approved')}</small></span>
      </div>
      <p class="amount">${money(r.total)}</p>
      <dl class="summary">
        <dt>Paid to</dt><dd>${label(r.seller)}</dd>
        <dt>Disputes settled by</dt><dd>${label(r.arbiter)}</dd>
        <dt>Delivery deadline</dt><dd>${r.ship_by_days} days after funding</dd>
        <dt>Your review period</dt><dd>${r.review_days} days after delivery</dd>
      </dl>
      <ul class="milestones">${r.milestones.map((m) => `<li><span>${ctx.esc(m.label)}</span><strong>${money(m.amount)}</strong><span class="pending">Held</span></li>`).join('')}</ul>
      <p class="small-text muted">Your money is locked on-chain, not held by ${ctx.esc(app.name)}. You release each milestone when you're satisfied. If there's a problem, open a dispute and the arbiter decides.</p>
      <p class="error" id="err"></p>
      ${expired ? '<p class="banner">This request has expired. Ask for a new one.</p>' : `<button class="primary" id="fund">Lock ${money(r.total)} in escrow</button>`}
    </section>`
  const btn = ctx.$('#fund')
  if (!btn) return
  if (!ctx.state.seed) btn.textContent = 'Open ORPay wallet to fund'
  btn.onclick = async () => {
    if (!ctx.state.seed) return ctx.requireWallet()
    const need = BigInt(r.total)
    const have = BigInt(r.asset ? ctx.state.account?.assets?.[r.asset] ?? 0 : ctx.state.account?.balance ?? 0)
    if (ctx.state.account && need > have) {
      ctx.$('#err').textContent = 'Not enough in your wallet. Add money first.'
      return
    }
    btn.disabled = true
    btn.textContent = 'Locking…'
    try {
      const txid = await escrowOp({
        seed: ctx.state.seed, op: 'create', seller: r.seller, arbiter: r.arbiter, asset: r.asset ?? '',
        milestones: r.milestones.map((m) => BigInt(m.amount)), ref: r.id,
        shipBy: Date.now() + r.ship_by_days * DAY, reviewSecs: r.review_days * 86_400,
      })
      await waitForCommit(txid)
      ctx.toast('Funds locked in escrow')
      ctx.refresh()
      renderEscrow(txid)
    } catch (err) {
      ctx.$('#err').textContent = err.message
      btn.disabled = false
      btn.textContent = `Lock ${money(r.total)} in escrow`
    }
  }
}
