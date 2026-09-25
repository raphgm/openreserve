// Savings pools (ajo/esusu). Everything shown here is read from the chain:
// the pool's balance, who has paid this round, who has received their pot,
// who is next, and a timeline of every join, contribution and claim.
import { api, nairaEq, assetLabel, formatMoney, node, parseAmount, poolOp, waitForCommit } from './orp.js'
import { renderSVG } from 'uqr'

let ctx // { state, $, esc, short, toast, nameOf, resolveRecipient, root }

export function initPools(c) {
  ctx = c
}

const label = (addr) => {
  const n = ctx.nameOf(addr)
  if (addr === ctx.state.address) return 'You'
  return n ? `@${ctx.esc(n)}` : `<span class="mono">${ctx.short(addr)}</span>`
}

const DAY = 86_400_000
const dueIn = (ms) => {
  const d = ms - Date.now()
  if (d <= 0) return 'overdue'
  const days = Math.floor(d / DAY)
  if (days >= 1) return `due in ${days} day${days === 1 ? '' : 's'}`
  const hours = Math.max(1, Math.floor(d / 3_600_000))
  return `due in ${hours} hour${hours === 1 ? '' : 's'}`
}
const dueAt = (p) => (p.round_secs && p.status === 'active' ? p.started_at + (p.round + 1) * p.round_secs * 1000 : 0)
const cadence = (secs) => ({ 604800: 'weekly', 1209600: 'every 2 weeks', 2592000: 'monthly' })[secs] ?? `every ${Math.round(secs / 86400)} days`

// Pools where this member still owes the current round's contribution.
export function duePools(pools, me) {
  return pools.filter((p) => {
    const i = p.members.indexOf(me)
    return i >= 0 && p.status === 'active' && !p.paid[i]
  })
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
  api.invites(ctx.state.seed).then((list) => {
    const ul = ctx.$('#pool-list')
    if (!ul || !list.length) return
    const rows = list.map((d) => `<li><button class="pool-row" data-invite="${d.id}">
      <span class="pool-avatar">✉</span>
      <span class="who"><strong>${ctx.esc(d.name)}</strong><small>Gathering members · ${d.members.length}/${d.slots} joined</small></span>
      <span class="chip-s info">Invite</span></button></li>`).join('')
    ul.insertAdjacentHTML('afterbegin', rows)
    ul.querySelector('.empty')?.remove()
    ul.querySelectorAll('[data-invite]').forEach((b) => (b.onclick = () => renderInvite(b.dataset.invite)))
  }).catch(() => {})
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
            <small>${formatMoney(p.contribution, p.asset ?? '')} ${p.round_secs ? cadence(p.round_secs) : 'each'} · ${p.members.length} members · ${
              p.status === 'active' ? `round ${p.round + 1} of ${p.members.length}` : p.status
            }</small>
          </span>
          ${needsMe && dueAt(p) ? `<span class="chip-s ${dueAt(p) < Date.now() ? 'bad' : 'warn'}">Pay · ${dueIn(dueAt(p))}</span>` : myTurn ? '<span class="chip-s ok">Your turn</span>' : needsMe ? '<span class="chip-s warn">Action needed</span>' : statusChip(p)}
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
        ${ctx.currencies().length > 1 ? `<div class="seg" role="radiogroup" aria-label="Currency">${ctx.currencies().map((c) => `<button type="button" role="radio" data-cur="${c}">${assetLabel(c)}</button>`).join('')}</div>` : ''}
        <label>Contribution per round<input id="amount" inputmode="decimal" placeholder="5,000" required></label>
        <div class="row">
          <label>Rounds<select id="cadence">
            <option value="604800">Weekly</option>
            <option value="1209600">Every 2 weeks</option>
            <option value="2592000" selected>Monthly</option>
            <option value="0">No due dates</option>
          </select></label>
          <label class="check deposit-check"><input type="checkbox" id="deposit" checked> Members pay one contribution as a deposit</label>
        </div>
        <p class="hint">With due dates, the member whose turn it is can collect once the round is due, even if someone hasn't paid. A missed payment is covered from that member's deposit and recorded for everyone to see. Unused deposits are returned at the end.</p>
        <label class="check"><input type="checkbox" id="by-link" checked> Invite people with a link (they join themselves)</label>
        <label id="slots-row">How many members, including you?<input id="slots" type="number" min="2" max="50" value="5"></label>
        <label id="members-row" hidden>Members in payout order
          <textarea id="members" rows="5" placeholder="One per line: @username or address&#10;The first person receives the first pot."></textarea>
        </label>
        <p class="hint">You are added as the first member unless you list yourself. Each member must join before the pool starts.</p>
        <p class="error" id="err"></p>
        <button class="primary" id="go">Create pool</button>
      </form>
    </section>`
  ctx.$('#back').onclick = renderPools
  ctx.$('#by-link').onchange = (e) => {
    ctx.$('#slots-row').hidden = !e.target.checked
    ctx.$('#members-row').hidden = e.target.checked
  }
  let asset = ctx.currencies()[0]
  const setAsset = (a) => {
    asset = a
    root.querySelectorAll('[data-cur]').forEach((b) => b.setAttribute('aria-checked', b.dataset.cur === a))
  }
  root.querySelectorAll('[data-cur]').forEach((b) => (b.onclick = () => setAsset(b.dataset.cur)))
  setAsset(asset)
  ctx.$('#f').onsubmit = async (e) => {
    e.preventDefault()
    const err = ctx.$('#err')
    const btn = ctx.$('#go')
    err.textContent = ''
    try {
      const contribution = parseAmount(ctx.$('#amount').value.replace(/,/g, ''))
      if (asset === 'NGN' && contribution % 10_000n !== 0n) throw new Error('Naira amounts can have at most 2 decimals.')
      if (contribution === 0n) throw new Error('Contribution must be more than 0.')
      const roundSecsPick = Number(ctx.$('#cadence').value)
      if (ctx.$('#by-link').checked) {
        btn.disabled = true
        btn.textContent = 'Creating invite…'
        const d = await api.createInvite(ctx.state.seed, {
          name: ctx.$('#name').value.trim(), asset, contribution: Number(contribution), round_secs: roundSecsPick,
          deposit: roundSecsPick && ctx.$('#deposit').checked ? Number(contribution) : 0, slots: Number(ctx.$('#slots').value),
        })
        return renderInvite(d.id)
      }
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
      const roundSecs = Number(ctx.$('#cadence').value)
      const deposit = roundSecs && ctx.$('#deposit').checked ? contribution : 0n
      const id = await poolOp({
        seed: ctx.state.seed, op: 'create', name: ctx.$('#name').value.trim(), members, contribution, asset, roundSecs, deposit,
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
  const money = (x) => formatMoney(x, p.asset ?? '')
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
      const extra = []
      if (p.defaults?.[i]) extra.push(`<span class="chip-s bad">Missed ${p.defaults[i]} payment${p.defaults[i] === 1 ? '' : 's'}</span>`)
      if (p.deposit) extra.push(`<span class="small-text muted">Deposit held ${money(p.deposits?.[i] ?? 0)}</span>`)
      if (p.paid_out?.[i]) extra.push(`<span class="small-text muted">Received ${money(p.paid_out[i])}</span>`)
      return `
        <li class="${i === me ? 'me' : ''}">
          <span class="pos">${i + 1}</span>
          <span class="who"><strong>${label(m)}</strong>${state}${extra.length ? `<span class="member-extra">${extra.join('')}</span>` : ''}</span>
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
    if (!p.paid[me]) btns.push(`<button class="primary" data-op="contribute">Pay my ${money(p.contribution)}</button>`)
    if (me === p.round) {
      const due = dueAt(p)
      const coverable = p.members.reduce((s, _, j) => s + (!p.paid[j] && BigInt(p.deposits?.[j] ?? 0) >= BigInt(p.contribution) ? BigInt(p.contribution) : 0n), 0n)
      if (paidCount === n) btns.push(`<button class="primary" data-op="claim">Take my ${money(pot)}</button>`)
      else if (due && Date.now() >= due)
        btns.push(`<button class="primary" data-op="claim">Collect now · ${money(BigInt(p.balance) + coverable)}</button>
          <p class="muted small-text">The round is past due. ${n - paidCount} member(s) haven't paid; their deposits cover what they can, and the missed payments are recorded.</p>`)
      else
        btns.push(`<p class="muted small-text">It's your turn. You can take the pot once all ${n} members have paid (${paidCount}/${n} so far)${due ? `, or collect what's in on ${new Date(due).toLocaleDateString()} when the round is due` : ''}.</p>`)
    }
    if (!btns.length) btns.push(`<p class="muted small-text">You've paid this round. ${receiver === ctx.state.address ? '' : `${label(receiver)} receives the pot once everyone has paid.`}</p>`)
    action = btns.join('')
  } else action = '<p class="muted small-text">This pool has finished. Every member received their pot.</p>'

  root.innerHTML = `
    <section class="card pool-hero" data-pool-id="${p.id}">
      <button class="link back light" id="back">← Pools</button>
      <div class="pool-title"><h2>${ctx.esc(p.name)}</h2>${statusChip(p)}</div>
      <p class="label">Pool balance</p>
      <p class="amount">${money(p.balance)}</p>${nairaEq(p.balance, p.asset ?? '')}
      <div class="pool-stats">
        <div><span>Pot</span><strong>${money(pot)}</strong></div>
        <div><span>Each pays</span><strong>${money(p.contribution)}</strong></div>
        <div><span>Round</span><strong>${p.status === 'done' ? `${n} of ${n}` : p.status === 'active' ? `${p.round + 1} of ${n}` : '—'}</strong></div>
      </div>
      ${p.status === 'active' ? `
      <div class="progress" role="progressbar" aria-valuenow="${paidCount}" aria-valuemax="${n}"><span data-w="${(paidCount / n) * 100}"></span></div>
      <p class="progress-label">${paidCount} of ${n} paid this round · ${label(receiver)} receives${nextUp ? ` · next: ${label(nextUp)}` : ''}${dueAt(p) ? ` · ${dueIn(dueAt(p))} (${new Date(dueAt(p)).toLocaleDateString()})` : ''}</p>` : ''}
      ${p.round_secs ? `<p class="progress-label">Rounds ${cadence(p.round_secs)}${p.deposit ? ` · deposit ${money(p.deposit)} per member` : ''}</p>` : ''}
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
            contribute: `${who} paid ${money(p.contribution)} · round ${e.round + 1}`,
            claim: `${who} took the ${money(pot)} pot · round ${e.round + 1}`,
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
          const txid = await poolOp({ seed: ctx.state.seed, op, id: p.id, asset: p.asset ?? '' })
          await waitForCommit(txid)
          ctx.toast({ join: 'Joined the pool', contribute: 'Contribution paid', claim: `You received ${money(pot)}` }[op])
          renderPool(p.id)
        } catch (err) {
          ctx.$('#err').textContent = err.message
          b.disabled = false
          b.textContent = labelText
        }
      }),
  )
}


// Ajo invite: share a link/QR; people join; the organiser starts the pool.
export async function renderInvite(id) {
  const root = ctx.root()
  let d
  try {
    d = await api.invite(id)
  } catch (err) {
    root.innerHTML = `<section class="card"><h2>Invite not found</h2><p class="error">${ctx.esc(err.message)}</p></section>`
    return
  }
  if (d.status === 'started' && d.pool_id) return renderPool(d.pool_id)
  await ctx.resolveNames(d.members)
  const me = ctx.state.address
  const joined = d.members.includes(me)
  const organizer = d.organizer === me
  const money = (x) => formatMoney(x, d.asset ?? '')
  root.innerHTML = `
    <section class="card pool-hero">
      <button class="link back light" id="back">← Pools</button>
      <div class="pool-title"><h2>${ctx.esc(d.name)}</h2><span class="chip-s info">Invite</span></div>
      <p class="label">Each member pays</p>
      <p class="amount">${money(d.contribution)}</p>${nairaEq(d.contribution, d.asset ?? '')}
      <div class="pool-stats">
        <div><span>Members</span><strong>${d.members.length} of ${d.slots}</strong></div>
        <div><span>Rounds</span><strong>${d.round_secs ? cadence(d.round_secs) : 'no due dates'}</strong></div>
        <div><span>Deposit</span><strong>${d.deposit ? money(d.deposit) : 'none'}</strong></div>
      </div>
    </section>
    <section class="card">
      ${organizer || joined ? `
        <div class="qr-wrap"><div class="qr" id="qr" role="img" aria-label="Invite QR code"></div>
        <p class="muted small-text">Share this so people can join with their ORPay wallet.</p></div>
        <div class="row"><button class="primary" id="share">Share invite</button><button class="ghost" id="copy">Copy link</button></div>` : ''}
      ${!joined ? `<p class="muted">${label(d.organizer)} invited you to a savings pool. You'll pay ${money(d.contribution)} each round${d.deposit ? ` plus a ${money(d.deposit)} deposit when it starts` : ''}, and receive the whole pot on your turn.</p>
        <button class="primary" id="join" ${d.members.length >= d.slots ? 'disabled' : ''}>${d.members.length >= d.slots ? 'This pool is full' : 'Join this ajo'}</button>` : ''}
      ${joined && !organizer ? `<p class="muted small-text">You're in. When ${label(d.organizer)} starts the pool you'll confirm your place${d.deposit ? ' and pay your deposit' : ''}.</p><button class="link" id="leave">Leave</button>` : ''}
      <p class="error" id="err"></p>
    </section>
    <section class="card">
      <h2>Payout order</h2>
      <ul class="members">${d.members.map((m, i) => `<li class="${m === me ? 'me' : ''}"><span class="pos">${i + 1}</span><span class="who"><strong>${label(m)}</strong>${m === d.organizer ? '<span class="small-text muted">Organiser</span>' : ''}</span>
        ${organizer && d.members.length > 1 ? `<span class="row-actions"><button class="ghost small" data-up="${i}" ${i === 0 ? 'disabled' : ''}>↑</button><button class="ghost small" data-down="${i}" ${i === d.members.length - 1 ? 'disabled' : ''}>↓</button></span>` : ''}</li>`).join('')}</ul>
      ${organizer ? `<button class="primary" id="start" ${d.members.length < 2 ? 'disabled' : ''}>Start the pool with ${d.members.length} member${d.members.length === 1 ? '' : 's'}</button>
        <p class="hint">Starting puts the pool on-chain with this order. Members then confirm${d.deposit ? ' and pay their deposit' : ''}.</p>` : ''}
    </section>`
  ctx.$('#back').onclick = renderPools
  const qr = ctx.$('#qr')
  if (qr) qr.innerHTML = renderSVG(d.invite_url, { border: 1 })
  const share = ctx.$('#share')
  if (share)
    share.onclick = async () => {
      if (navigator.share) await navigator.share({ title: `Join ${d.name} on ORPay`, url: d.invite_url }).catch(() => {})
      else navigator.clipboard.writeText(d.invite_url).then(() => ctx.toast('Invite link copied'))
    }
  const copy = ctx.$('#copy')
  if (copy) copy.onclick = () => navigator.clipboard.writeText(d.invite_url).then(() => ctx.toast('Invite link copied'))
  const act = async (btn, fn) => {
    btn.disabled = true
    try {
      await fn()
      renderInvite(id)
    } catch (err) {
      ctx.$('#err').textContent = err.message
      btn.disabled = false
    }
  }
  const join = ctx.$('#join')
  if (join) join.onclick = () => (ctx.state.seed ? act(join, () => api.inviteAction(ctx.state.seed, id, 'join')) : ctx.requireWallet())
  const leave = ctx.$('#leave')
  if (leave) leave.onclick = () => act(leave, () => api.inviteAction(ctx.state.seed, id, 'leave'))
  const move = (i, j) => {
    const order = d.members.slice()
    ;[order[i], order[j]] = [order[j], order[i]]
    return api.inviteAction(ctx.state.seed, id, 'order', { members: order })
  }
  root.querySelectorAll('[data-up]').forEach((b) => (b.onclick = () => act(b, () => move(+b.dataset.up, +b.dataset.up - 1))))
  root.querySelectorAll('[data-down]').forEach((b) => (b.onclick = () => act(b, () => move(+b.dataset.down, +b.dataset.down + 1))))
  const start = ctx.$('#start')
  if (start)
    start.onclick = () =>
      act(start, async () => {
        start.textContent = 'Starting…'
        const poolId = await poolOp({
          seed: ctx.state.seed, op: 'create', name: d.name, members: d.members, contribution: BigInt(d.contribution),
          asset: d.asset ?? '', roundSecs: d.round_secs ?? 0, deposit: BigInt(d.deposit ?? 0),
        })
        await waitForCommit(poolId)
        await api.inviteAction(ctx.state.seed, id, 'started', { pool_id: poolId })
        ctx.toast('Pool started. Members can now confirm their place.')
      })
}
