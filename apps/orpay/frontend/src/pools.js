// Savings pools (ajo/esusu). Everything shown here is read from the chain:
// the pool's balance, who has paid this round, who has received their pot,
// who is next, and a timeline of every join, contribution and claim.
import { formatAmount, node, parseAmount, poolOp, waitForCommit } from './orp.js'

let ctx // { state, $, esc, short, toast, nameOf, resolveRecipient, root }

export function initPools(c) {
  ctx = c
}

const label = (addr) => {
  const n = ctx.nameOf(addr)
  if (addr === ctx.state.address) return 'You'
  return n ? `@${ctx.esc(n)}` : `<span class="mono">${ctx.short(addr)}</span>`
}

const statusChip = (p) =>
  ({ forming: '<span class="chip-s warn">Waiting for members</span>', active: '<span class="chip-s ok">Active</span>', done: '<span class="chip-s">Completed</span>' })[p.status]

export async function renderPools() {
  const root = ctx.root()
  root.innerHTML = `
    <section class="card">
      <div class="section-head">
        <h2>Savings pools</h2>
        <button class="primary small" id="new-pool">New pool</button>
      </div>
      <p class="muted small-text">Members take turns receiving the pot. Every contribution and payout is recorded on-chain, so everyone can see who has paid, who has received and who is next.</p>
      <ul class="pool-list" id="pool-list"><li class="empty">Loading…</li></ul>
    </section>`
  ctx.$('#new-pool').onclick = renderCreate
  let pools
  try {
    pools = await node.pools(ctx.state.address)
  } catch (err) {
    ctx.$('#pool-list').innerHTML = `<li class="empty">${ctx.esc(err.message)}</li>`
    return
  }
  await ctx.resolveNames(pools.flatMap((p) => p.members))
  const list = ctx.$('#pool-list')
  if (!list) return
  if (!pools.length) {
    list.innerHTML = '<li class="empty">No pools yet. Start one with friends, family or colleagues.</li>'
    return
  }
  list.innerHTML = pools
    .map((p) => {
      const me = p.members.indexOf(ctx.state.address)
      const myTurn = p.status === 'active' && p.round === me
      const needsMe =
        (p.status === 'forming' && !p.joined[me]) || (p.status === 'active' && !p.paid[me])
      return `
        <li><button class="pool-row" data-id="${p.id}">
          <span class="pool-avatar">${ctx.esc(p.name.slice(0, 1).toUpperCase())}</span>
          <span class="who">
            <strong>${ctx.esc(p.name)}</strong>
            <small>${formatAmount(p.contribution)} ORP each · ${p.members.length} members · ${
              p.status === 'active' ? `round ${p.round + 1} of ${p.members.length}` : p.status
            }</small>
          </span>
          ${myTurn ? '<span class="chip-s ok">Your turn</span>' : needsMe ? '<span class="chip-s warn">Action needed</span>' : statusChip(p)}
        </button></li>`
    })
    .join('')
  list.querySelectorAll('[data-id]').forEach((b) => (b.onclick = () => renderPool(b.dataset.id)))
}

function renderCreate() {
  const root = ctx.root()
  root.innerHTML = `
    <section class="card">
      <button class="link back" id="back">← Pools</button>
      <h2>New savings pool</h2>
      <form id="f" class="stack" autocomplete="off">
        <label>Pool name<input id="name" maxlength="64" placeholder="Family ajo" required></label>
        <label>Contribution per round (ORP)<input id="amount" inputmode="decimal" placeholder="50" required></label>
        <label>Members in payout order
          <textarea id="members" rows="5" placeholder="One per line: @username or address&#10;The first person receives the first pot."></textarea>
        </label>
        <p class="hint">You are added as the first member unless you list yourself. Each member must join before the pool starts.</p>
        <p class="error" id="err"></p>
        <button class="primary" id="go">Create pool</button>
      </form>
    </section>`
  ctx.$('#back').onclick = renderPools
  ctx.$('#f').onsubmit = async (e) => {
    e.preventDefault()
    const err = ctx.$('#err')
    const btn = ctx.$('#go')
    err.textContent = ''
    try {
      const contribution = parseAmount(ctx.$('#amount').value)
      if (contribution === 0n) throw new Error('Contribution must be more than 0.')
      const refs = ctx.$('#members').value.split(/[\n,]+/).map((s) => s.trim()).filter(Boolean)
      const members = []
      for (const ref of refs) {
        const addr = ref.toLowerCase() === '@me' ? ctx.state.address : await ctx.resolveRecipient(ref)
        if (members.includes(addr)) throw new Error(`${ref} is listed twice.`)
        members.push(addr)
      }
      if (!members.includes(ctx.state.address)) members.unshift(ctx.state.address)
      if (members.length < 2) throw new Error('Add at least one other member.')
      btn.disabled = true
      btn.textContent = 'Creating…'
      const id = await poolOp({
        seed: ctx.state.seed, op: 'create', name: ctx.$('#name').value.trim(), members, contribution,
      })
      await waitForCommit(id)
      ctx.toast('Pool created. Share it with the members so they can join.')
      renderPool(id)
    } catch (e2) {
      err.textContent = e2.message
      btn.disabled = false
      btn.textContent = 'Create pool'
    }
  }
}

// Rebuild the round of each contribution/claim from the ordered history.
function annotate(history) {
  let round = 0
  return history.map((e) => {
    const op = e.tx.pool.op
    const entry = { ...e, op, round }
    if (op === 'claim') round++
    return entry
  })
}

// While a pool is on screen, poll it so other members' payments and claims
// appear without a reload. Re-render only when something changed and the
// user isn't mid-action.
let watch = { id: null, timer: null, snapshot: '' }

function watchPool(id, snapshot) {
  clearInterval(watch.timer)
  watch = { id, snapshot, timer: null }
  watch.timer = setInterval(async () => {
    const root = ctx.root()
    if (ctx.state.view !== 'pools' || !root?.querySelector(`[data-pool-id="${id}"]`)) {
      clearInterval(watch.timer)
      return
    }
    if (root.querySelector('[data-op]:disabled')) return
    try {
      const d = await node.pool(id)
      const snap = JSON.stringify(d)
      if (snap !== watch.snapshot) renderPool(id, d)
    } catch {}
  }, 3000)
}

export async function renderPool(id, preloaded) {
  const root = ctx.root()
  if (!preloaded) root.innerHTML = '<section class="card"><p class="muted">Loading pool…</p></section>'
  let data = preloaded
  try {
    data ??= await node.pool(id)
  } catch (err) {
    root.innerHTML = `<section class="card"><button class="link back" id="back">← Pools</button><p class="error">${ctx.esc(err.message)}</p></section>`
    ctx.$('#back').onclick = renderPools
    return
  }
  const { pool: p, pot, history } = data
  await ctx.resolveNames(p.members)
  const n = p.members.length
  const me = p.members.indexOf(ctx.state.address)
  const paidCount = p.paid.filter(Boolean).length
  const receiver = p.status === 'active' ? p.members[p.round] : null
  const nextUp = p.status === 'active' && p.round + 1 < n ? p.members[p.round + 1] : null
  const events = annotate(history).reverse()

  const rows = p.members
    .map((m, i) => {
      let state
      if (p.claimed[i]) state = `<span class="chip-s">Received · round ${i + 1}</span>`
      else if (p.status === 'active' && i === p.round) state = '<span class="chip-s ok">Receiving now</span>'
      else if (p.status === 'active' && i === p.round + 1) state = '<span class="chip-s info">Next</span>'
      else state = `<span class="muted small-text">Round ${i + 1}</span>`
      let pay = ''
      if (p.status === 'forming') pay = p.joined[i] ? '<span class="tick">Joined</span>' : '<span class="pending">Not joined</span>'
      else if (p.status === 'active') pay = p.paid[i] ? '<span class="tick">Paid</span>' : '<span class="pending">Not paid</span>'
      return `
        <li class="${i === me ? 'me' : ''}">
          <span class="pos">${i + 1}</span>
          <span class="who"><strong>${label(m)}</strong>${state}</span>
          ${pay}
        </li>`
    })
    .join('')

  let action = ''
  if (me < 0) action = '<p class="muted small-text">You are viewing this pool. Only members can take part.</p>'
  else if (p.status === 'forming' && !p.joined[me]) action = `<button class="primary" data-op="join">Join pool</button>`
  else if (p.status === 'forming') action = `<p class="muted small-text">Waiting for ${n - p.joined.filter(Boolean).length} member(s) to join.</p>`
  else if (p.status === 'active') {
    const btns = []
    if (!p.paid[me]) btns.push(`<button class="primary" data-op="contribute">Pay my ${formatAmount(p.contribution)} ORP</button>`)
    if (me === p.round) {
      btns.push(
        paidCount === n
          ? `<button class="primary" data-op="claim">Take my ${formatAmount(pot)} ORP</button>`
          : `<p class="muted small-text">It's your turn. You can take the pot once all ${n} members have paid (${paidCount}/${n} so far).</p>`,
      )
    }
    if (!btns.length) btns.push(`<p class="muted small-text">You've paid this round. ${receiver === ctx.state.address ? '' : `${label(receiver)} receives the pot once everyone has paid.`}</p>`)
    action = btns.join('')
  } else action = '<p class="muted small-text">This pool has finished. Every member received their pot.</p>'

  root.innerHTML = `
    <section class="card pool-hero" data-pool-id="${p.id}">
      <button class="link back light" id="back">← Pools</button>
      <div class="pool-title"><h2>${ctx.esc(p.name)}</h2>${statusChip(p)}</div>
      <p class="label">Pool balance</p>
      <p class="amount">${formatAmount(p.balance)} <span class="unit">ORP</span></p>
      <div class="pool-stats">
        <div><span>Pot</span><strong>${formatAmount(pot)} ORP</strong></div>
        <div><span>Each pays</span><strong>${formatAmount(p.contribution)} ORP</strong></div>
        <div><span>Round</span><strong>${p.status === 'done' ? `${n} of ${n}` : p.status === 'active' ? `${p.round + 1} of ${n}` : '—'}</strong></div>
      </div>
      ${p.status === 'active' ? `
      <div class="progress" role="progressbar" aria-valuenow="${paidCount}" aria-valuemax="${n}"><span data-w="${(paidCount / n) * 100}"></span></div>
      <p class="progress-label">${paidCount} of ${n} paid this round · ${label(receiver)} receives${nextUp ? ` · next: ${label(nextUp)}` : ''}</p>` : ''}
    </section>
    <section class="card">
      <div class="stack" id="actions">${action}</div>
      <p class="error" id="err"></p>
    </section>
    <section class="card">
      <h2>Members & payout order</h2>
      <ul class="members">${rows}</ul>
    </section>
    <section class="card">
      <div class="section-head"><h2>On-chain record</h2><button class="link" id="share">Share pool</button></div>
      <ul class="timeline">${events
        .map((e) => {
          const who = label(e.tx.from)
          const text = {
            create: `${who} created the pool`,
            join: `${who} joined`,
            contribute: `${who} paid ${formatAmount(p.contribution)} ORP · round ${e.round + 1}`,
            claim: `${who} took the ${formatAmount(pot)} ORP pot · round ${e.round + 1}`,
          }[e.op]
          const when = new Date(e.time).toLocaleString([], { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' })
          return `<li class="ev-${e.op}"><span class="ev-dot"></span><span class="who"><strong>${text}</strong><small>${when} · block ${e.height} · tx ${ctx.short(e.id)}</small></span></li>`
        })
        .join('')}</ul>
    </section>`

  watchPool(p.id, JSON.stringify(data))
  // Set via the CSSOM: the production CSP forbids inline style attributes.
  root.querySelectorAll('[data-w]').forEach((el) => (el.style.width = `${el.dataset.w}%`))
  ctx.$('#back').onclick = renderPools
  ctx.$('#share').onclick = () => {
    const link = `${location.origin}/?pool=${p.id}`
    navigator.clipboard.writeText(link).then(() => ctx.toast('Pool link copied'))
  }
  root.querySelectorAll('[data-op]').forEach(
    (b) =>
      (b.onclick = async () => {
        const op = b.dataset.op
        const labelText = b.textContent
        b.disabled = true
        b.textContent = 'Confirming…'
        try {
          const txid = await poolOp({ seed: ctx.state.seed, op, id: p.id })
          await waitForCommit(txid)
          ctx.toast({ join: 'Joined the pool', contribute: 'Contribution paid', claim: `You received ${formatAmount(pot)} ORP` }[op])
          renderPool(p.id)
        } catch (err) {
          ctx.$('#err').textContent = err.message
          b.disabled = false
          b.textContent = labelText
        }
      }),
  )
}

