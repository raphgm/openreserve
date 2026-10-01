// Savings pools (ajo/esusu). Everything shown here is read from the chain:
// the pool's balance, who has paid this round, who has received their pot,
// who is next, and a timeline of every join, contribution and claim.
import { api, assetLabel, formatAmount, formatMoney, node, parseAmount, poolOp, waitForCommit } from './orp.js'
import { renderSVG } from 'uqr'
import { fillTrust } from './trust.js'

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
  ({ forming: '<span class="chip-s warn">Waiting for members</span>', active: '<span class="chip-s ok">Active</span>', done: '<span class="chip-s">Completed</span>', cancelled: '<span class="chip-s bad">Deleted</span>' })[p.status]

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

const MODES = [
  { id: '', title: 'Rotation', sub: 'Fixed order, each takes a turn' },
  { id: 'lottery', title: 'Lottery', sub: 'Order drawn fairly at the start' },
  { id: 'bidding', title: 'Bidding', sub: 'Need it sooner? Bid a discount' },
  { id: 'goal', title: 'Savings goal', sub: 'Save toward a target together' },
]
// Ready-made circles people already run, filled in with one tap.
const TEMPLATES = [
  { name: 'Market women weekly', sub: '₦5,000 a week · 10 people', mode: '', amount: '5,000', cadence: 604800, slots: 10, deposit: true, insurance: 200 },
  { name: 'Office monthly', sub: '₦50,000 a month · 6 people', mode: '', amount: '50,000', cadence: 2592000, slots: 6, deposit: true, insurance: 100 },
  { name: 'Lucky draw weekly', sub: 'Order drawn fairly · ₦10,000', mode: 'lottery', amount: '10,000', cadence: 604800, slots: 8, deposit: true, insurance: 200 },
  { name: 'Business bidding circle', sub: 'Need it sooner? Bid · ₦100,000', mode: 'bidding', amount: '100,000', cadence: 2592000, slots: 6, deposit: true, insurance: 200 },
  { name: 'Family land fund', sub: 'Save ₦2,000,000 together', mode: 'goal', amount: '20,000', cadence: 31536000, slots: 5, target: '2,000,000' },
]

export const modeName = (m) => MODES.find((x) => x.id === (m ?? ''))?.title ?? 'Rotation'

function renderCreate() {
  const root = ctx.root()
  root.innerHTML = `
    <section class="card">
      <button class="link back" id="back">← Pools</button>
      <h2>New savings pool</h2>
      <form id="f" class="stack" autocomplete="off">
        <label>Pool name<input id="name" maxlength="64" placeholder="Family ajo" required></label>
        <div class="suggest">
          <span class="suggest-label">Try one</span>
          <div class="templates" aria-label="Suggestions">
            ${TEMPLATES.map((t, i) => `<button type="button" class="tpl" data-tpl="${i}" title="${t.sub}">${t.name}</button>`).join('')}
          </div>
        </div>
        <div class="mode-pick" role="radiogroup" aria-label="Circle type">
          ${MODES.map((m) => `<button type="button" role="radio" data-mode="${m.id}"><b>${m.title}</b><small>${m.sub}</small></button>`).join('')}
        </div>
        ${ctx.currencies().length > 1 ? `<div class="seg" role="radiogroup" aria-label="Currency">${ctx.currencies().map((c) => `<button type="button" role="radio" data-cur="${c}">${assetLabel(c)}</button>`).join('')}</div>` : ''}
        <label id="target-row" hidden>Savings goal for the group<input id="target" inputmode="decimal" placeholder="2,000,000"></label>
        <label><span id="amount-label">Contribution per round</span><input id="amount" inputmode="decimal" placeholder="5,000" required></label>
        <div class="row" id="cadence-row">
          <label><span id="cadence-label">Rounds</span><select id="cadence">
            <option value="604800">Weekly</option>
            <option value="1209600">Every 2 weeks</option>
            <option value="2592000" selected>Monthly</option>
            <option value="0">No due dates</option>
          </select></label>
          <label class="check deposit-check"><input type="checkbox" id="deposit" checked> Members pay one contribution as a deposit</label>
        </div>
        <p class="hint" id="cadence-hint">With due dates, the member whose turn it is can collect once the round is due, even if someone hasn't paid. A missed payment is covered from that member's deposit and recorded for everyone to see. Unused deposits are returned at the end.</p>
        <label id="ins-row">Circle insurance<select id="insurance">
          <option value="0">None</option>
          <option value="100">1% of each pot</option>
          <option value="200" selected>2% of each pot</option>
          <option value="500">5% of each pot</option>
        </select></label>
        <p class="hint" id="ins-hint">A small cut of each pot is kept in the circle to cover anyone who misses a payment after their deposit runs out. What's left is shared back to everyone at the end.</p>
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
  let mode = ''
  const setMode = (m) => {
    mode = m
    root.querySelectorAll('[data-mode]').forEach((b) => b.setAttribute('aria-checked', b.dataset.mode === m))
    const goal = m === 'goal'
    ctx.$('#target-row').hidden = !goal
    ctx.$('#amount-label').textContent = goal ? 'Suggested saving each time' : 'Contribution per round'
    ctx.$('#cadence-label').textContent = goal ? 'Deadline' : 'Rounds'
    ctx.$('#ins-row').hidden = ctx.$('#ins-hint').hidden = goal
    ctx.$('.deposit-check').hidden = goal
    ctx.$('#cadence-hint').textContent = goal
      ? 'Savings unlock for everyone when the goal is reached, or at the deadline. Each member always gets back exactly what they saved.'
      : "With due dates, the member whose turn it is can collect once the round is due, even if someone hasn't paid. A missed payment is covered from that member's deposit and recorded for everyone to see."
    ctx.$('#cadence').innerHTML = goal
      ? '<option value="0">No deadline</option><option value="7776000">Deadline in 3 months</option><option value="15552000" selected>Deadline in 6 months</option><option value="31536000">Deadline in 1 year</option>'
      : '<option value="604800">Weekly</option><option value="1209600">Every 2 weeks</option><option value="2592000" selected>Monthly</option><option value="0">No due dates</option>'
  }
  root.querySelectorAll('[data-mode]').forEach((b) => (b.onclick = () => setMode(b.dataset.mode)))
  setMode('')
  root.querySelectorAll('[data-tpl]').forEach((b) => (b.onclick = () => {
    const t = TEMPLATES[b.dataset.tpl]
    root.querySelectorAll('[data-tpl]').forEach((x) => x.setAttribute('aria-pressed', x === b))
    setMode(t.mode)
    ctx.$('#name').value = t.name
    ctx.$('#amount').value = t.amount
    ctx.$('#cadence').value = String(t.cadence)
    if (t.target) ctx.$('#target').value = t.target
    if (ctx.$('#insurance')) ctx.$('#insurance').value = String(t.insurance ?? 0)
    ctx.$('#deposit').checked = !!t.deposit
    ctx.$('#slots').value = t.slots
  }))
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
      const circleOpts = () => {
        const o = { mode }
        if (mode === 'goal') {
          const target = parseAmount(ctx.$('#target').value.replace(/,/g, ''))
          if (target === 0n) throw new Error('Enter the group savings goal.')
          o.target = Number(target)
        } else o.insurance_bps = Number(ctx.$('#insurance').value)
        return o
      }
      if (ctx.$('#by-link').checked) {
        btn.disabled = true
        btn.textContent = 'Creating invite…'
        const d = await api.createInvite(ctx.state.seed, {
          name: ctx.$('#name').value.trim(), asset, contribution: Number(contribution), round_secs: roundSecsPick,
          deposit: mode !== 'goal' && roundSecsPick && ctx.$('#deposit').checked ? Number(contribution) : 0, slots: Number(ctx.$('#slots').value),
          ...circleOpts(),
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
      const deposit = mode !== 'goal' && roundSecs && ctx.$('#deposit').checked ? contribution : 0n
      const o = circleOpts()
      const id = await poolOp({
        seed: ctx.state.seed, op: 'create', name: ctx.$('#name').value.trim(), members, contribution, asset, roundSecs, deposit,
        mode: o.mode, insuranceBps: o.insurance_bps ?? 0, target: o.target ?? 0,
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

// Who collects this round. Bidding circles: the biggest bid among members
// who haven't collected (ties: earliest), else the next one who hasn't.
function recipientOf(p) {
  if (p.mode !== 'bidding') return p.round
  let best = -1, first = -1
  p.members.forEach((_, j) => {
    if (p.claimed[j]) return
    if (first < 0) first = j
    const b = BigInt(p.bids?.[j] ?? 0)
    if (b > 0n && (best < 0 || b > BigInt(p.bids[best]))) best = j
  })
  return best >= 0 ? best : first
}

// Savings-goal circle: everyone saves toward one target and keeps their own savings.
function renderGoal(p, history, money) {
  const root = ctx.root()
  const me = p.members.indexOf(ctx.state.address)
  const target = BigInt(p.target)
  const saved = BigInt(p.balance)
  const pct = Math.min(100, Number((saved * 100n) / (target || 1n)))
  const deadline = p.round_secs && p.started_at ? p.started_at + p.round_secs * 1000 : 0
  const unlocked = p.status === 'done' || (deadline && Date.now() >= deadline)
  const mine = me >= 0 ? BigInt(p.saved?.[me] ?? 0) : 0n
  const rows = p.members.map((m, i) => `
    <li class="${i === me ? 'me' : ''}"><span class="pos">${i + 1}</span>
      <span class="who"><strong>${label(m)}</strong>${p.status === 'forming' ? `<span class="small-text muted">${p.joined[i] ? 'Joined' : 'Not joined'}</span>` : `<span class="small-text muted">${p.withdrawn?.[i] ? 'Took their savings' : 'Saved'}</span>`}</span>
      <span class="tick">${money(p.saved?.[i] ?? 0)}</span></li>`).join('')
  let action = ''
  if (me < 0) action = '<p class="muted small-text">You are viewing this circle.</p>'
  else if (p.status === 'forming' && !p.joined[me]) action = '<button class="primary" data-op="join">Join circle</button>'
  else if (p.status === 'forming') action = `<p class="muted small-text">Waiting for ${p.joined.filter((j) => !j).length} member(s) to join.</p>`
  if (p.status === 'cancelled') action = '<p class="muted small-text">The organiser deleted this circle before it started.</p>'
  else if (p.status === 'forming' && p.creator === ctx.state.address) action += '<button class="link danger-link" id="cancel-circle">Delete this circle</button>'
  else {
    if (p.status === 'active') action += `<div class="part-pay"><input id="save-amt" inputmode="decimal" placeholder="Amount (suggested ${money(p.contribution)})"><button class="primary small" id="save-go">Save</button></div>`
    if (unlocked && mine > 0n) action += `<button class="primary" id="withdraw-go">Take my savings · ${money(mine)}</button>`
    else if (!unlocked) action += `<p class="muted small-text">Savings unlock for everyone when the goal is reached${deadline ? ` or on ${new Date(deadline).toLocaleDateString()}` : ''}.</p>`
  }
  root.innerHTML = `
    <section class="card pool-hero" data-pool-id="${p.id}">
      <button class="link back light" id="back">← Pools</button>
      <div class="pool-title"><h2>${ctx.esc(p.name)}</h2><span class="chip-s light">Savings goal</span></div>
      <p class="label">Saved together</p>
      <p class="amount">${money(saved)}</p>
      <div class="progress" role="progressbar" aria-valuenow="${pct}" aria-valuemax="100"><span data-w="${pct}"></span></div>
      <p class="progress-label">${pct}% of ${money(target)}${p.status === 'done' ? ' · goal reached' : deadline ? ` · deadline ${new Date(deadline).toLocaleDateString()}` : ''}</p>
    </section>
    <section class="card"><div class="stack">${action}</div><p class="error" id="err"></p></section>
    <section class="card"><h2>Members</h2><ul class="members">${rows}</ul></section>`
  root.querySelectorAll('[data-w]').forEach((el) => (el.style.width = `${el.dataset.w}%`))
  ctx.$('#back').onclick = renderPools
  const run = async (btn, args, done) => {
    btn.disabled = true
    try {
      await waitForCommit(await poolOp({ seed: ctx.state.seed, id: p.id, asset: p.asset ?? '', ...args }))
      ctx.toast(done)
      renderPool(p.id)
    } catch (err) {
      ctx.$('#err').textContent = err.message
      btn.disabled = false
    }
  }
  if (ctx.$('[data-op="join"]')) ctx.$('[data-op="join"]').onclick = (e) => run(e.target, { op: 'join' }, 'Joined')
  if (ctx.$('#save-go'))
    ctx.$('#save-go').onclick = (e) => {
      try {
        const v = parseAmount(ctx.$('#save-amt').value.replace(/[,₦\s]/g, ''))
        if (v === 0n) throw new Error('Enter an amount.')
        run(e.target, { op: 'contribute', contribution: v }, 'Saved')
      } catch (err) {
        ctx.$('#err').textContent = err.message
      }
    }
  if (ctx.$('#cancel-circle'))
    ctx.$('#cancel-circle').onclick = (e) => confirm('Delete this circle? This cannot be undone.') && run(e.target, { op: 'cancel' }, 'Circle deleted')
  if (ctx.$('#withdraw-go')) ctx.$('#withdraw-go').onclick = (e) => run(e.target, { op: 'withdraw' }, `${money(mine)} is back in your wallet`)
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
  if (p.mode === 'goal') return renderGoal(p, history, money)
  const n = p.members.length
  const me = p.members.indexOf(ctx.state.address)
  const paidCount = p.paid.filter(Boolean).length
  const bidding = p.mode === 'bidding'
  const recIdx = p.status === 'active' ? recipientOf(p) : -1
  const receiver = recIdx >= 0 ? p.members[recIdx] : null
  const nextUp = !bidding && p.status === 'active' && p.round + 1 < n ? p.members[p.round + 1] : null
  const each = BigInt(p.contribution)
  const paidSoFar = (j) => BigInt(p.paid_amt?.[j] ?? (p.paid[j] ? each : 0n))
  const canSwap = (j) => !bidding && p.status !== 'done' && me >= 0 && j !== me && !p.claimed[j] && !p.claimed[me] &&
    !(p.status === 'active' && (j === p.round || me === p.round))
  const askedMe = p.swap_with ? p.members.filter((m, j) => p.swap_with[j] === ctx.state.address) : []
  const events = annotate(history).reverse()

  const rows = p.members
    .map((m, i) => {
      let state
      if (p.claimed[i]) state = `<span class="chip-s">Received${bidding ? '' : ` · round ${i + 1}`}</span>`
      else if (p.status === 'active' && i === recIdx) state = `<span class="chip-s ok">Receiving now</span>`
      else if (!bidding && p.status === 'active' && i === p.round + 1) state = '<span class="chip-s info">Next</span>'
      else state = bidding ? '<span class="muted small-text">Waiting</span>' : `<span class="muted small-text">Round ${i + 1}</span>`
      let pay = ''
      if (p.status === 'forming') pay = p.joined[i] ? '<span class="tick">Joined</span>' : '<span class="pending">Not joined</span>'
      else if (p.status === 'active') pay = p.paid[i] ? '<span class="tick">Paid</span>' : paidSoFar(i) > 0n ? `<span class="pending">${money(paidSoFar(i))} of ${money(each)}</span>` : '<span class="pending">Not paid</span>'
      const extra = []
      if (p.defaults?.[i]) extra.push(`<span class="chip-s bad">Missed ${p.defaults[i]} payment${p.defaults[i] === 1 ? '' : 's'}</span>`)
      if (p.deposit) extra.push(`<span class="small-text muted">Deposit held ${money(p.deposits?.[i] ?? 0)}</span>`)
      if (p.paid_out?.[i]) extra.push(`<span class="small-text muted">Received ${money(p.paid_out[i])}</span>`)
      if (bidding && BigInt(p.bids?.[i] ?? 0) > 0n) extra.push(`<span class="chip-s info">Bid ${money(p.bids[i])}</span>`)
      if (p.swap_with?.[i] === ctx.state.address) extra.push('<span class="chip-s warn">Wants to swap with you</span>')
      if (me >= 0 && p.swap_with?.[me] === m) extra.push('<span class="chip-s">Swap requested</span>')
      if (canSwap(i) && p.swap_with?.[me] !== m) extra.push(`<button class="link small-link" data-swap="${m}">${p.swap_with?.[i] === ctx.state.address ? 'Accept swap' : 'Ask to swap turns'}</button>`)
      return `
        <li class="${i === me ? 'me' : ''}">
          <span class="pos">${i + 1}</span>
          <span class="who"><strong>${label(m)} <span data-trust="${m}"></span></strong>${state}${extra.length ? `<span class="member-extra">${extra.join('')}</span>` : ''}</span>
          ${pay}
        </li>`
    })
    .join('')

  let action = ''
  if (me < 0) action = '<p class="muted small-text">You are viewing this pool. Only members can take part.</p>'
  else if (p.status === 'forming' && !p.joined[me]) action = `<button class="primary" data-op="join">Join pool</button>`
  else if (p.status === 'forming') action = `<p class="muted small-text">Waiting for ${n - p.joined.filter(Boolean).length} member(s) to join.</p>`
  if (p.status === 'cancelled') action = '<p class="muted small-text">The organiser deleted this circle before it started. Any deposits were returned.</p>'
  if (p.status === 'forming' && p.creator === ctx.state.address) action += '<button class="link danger-link" id="cancel-circle">Delete this circle</button>'
  else if (p.status === 'active') {
    const btns = []
    if (!p.paid[me]) {
      const rem = each - paidSoFar(me)
      btns.push(`<button class="primary" data-op="contribute">Pay ${paidSoFar(me) > 0n ? 'the rest · ' : 'my '}${money(rem)}</button>
        <div class="part-pay"><input id="part" inputmode="decimal" placeholder="Pay part now (e.g. ${formatMoney(rem / 4n || rem, p.asset ?? '').replace(/\.00$/, '')})"><button class="ghost small" id="part-go">Pay part</button></div>`)
    }
    if (bidding && !p.claimed[me]) {
      btns.push(`<div class="part-pay"><input id="bid" inputmode="decimal" placeholder="Discount to collect next (max ${money(BigInt(pot) / 2n)})" value="${BigInt(p.bids?.[me] ?? 0) > 0n ? formatAmount(p.bids[me]) : ''}"><button class="ghost small" id="bid-go">${BigInt(p.bids?.[me] ?? 0) > 0n ? 'Change bid' : 'Bid'}</button></div>
        <p class="muted small-text">Need the pot sooner? Offer a discount. The biggest bid collects this round, and the discount is shared by everyone else.</p>`)
    }
    if (me === recIdx) {
      const due = dueAt(p)
      const coverable = p.members.reduce((s, _, j) => s + (!p.paid[j] && BigInt(p.deposits?.[j] ?? 0) >= BigInt(p.contribution) ? BigInt(p.contribution) : 0n), 0n)
      if (paidCount === n) btns.push(`<button class="primary" data-op="claim">Take my ${money(pot)}</button>`)
      else if (due && Date.now() >= due)
        btns.push(`<button class="primary" data-op="claim">Collect now · ${money(BigInt(p.balance) + coverable)}</button>
          <p class="muted small-text">The round is past due. ${n - paidCount} member(s) haven't paid; their deposits cover what they can, and the missed payments are recorded.</p>`)
      else
        btns.push(`<p class="muted small-text">It's your turn. You can take the pot once all ${n} members have paid (${paidCount}/${n} so far)${due ? `, or collect what's in on ${new Date(due).toLocaleDateString()} when the round is due` : ''}.</p>`)
    }
    if (p.creator === ctx.state.address && paidCount < n)
      btns.push(`<button class="ghost small" id="nudge">Remind ${n - paidCount} member${n - paidCount === 1 ? '' : 's'} who haven't paid</button>`)
    if (!btns.length) btns.push(`<p class="muted small-text">You've paid this round. ${receiver === ctx.state.address ? '' : `${label(receiver)} receives the pot once everyone has paid.`}</p>`)
    action = btns.join('')
  } else action = '<p class="muted small-text">This pool has finished. Every member received their pot.</p>'

  // Autopay: money set aside in the pool pays each round automatically.
  const prepaid = me >= 0 ? BigInt(p.prepaid?.[me] ?? 0) : 0n
  const covered = Number(prepaid / each)
  const left = me < 0 || p.status === 'done' ? 0 : n - (p.status === 'active' ? p.round : 0) - (p.status === 'active' && p.paid[me] ? 1 : 0)
  const canAdd = Math.max(0, left - covered)
  const autopay = me < 0 || p.status === 'done' ? '' : `
    <section class="card autopay">
      <div class="section-head"><h2>Autopay</h2>${covered ? `<span class="chip-s ok">On · ${covered} round${covered === 1 ? '' : 's'}</span>` : '<span class="chip-s">Off</span>'}</div>
      <p class="muted small-text">Set money aside in the pool and each round is paid for you automatically, even if you forget to open ORPay. It stays in the pool, not with anyone else, and you can stop any time to get back what hasn't been used.</p>
      ${canAdd ? `<div class="autopay-row">
        <label>Rounds to cover<select id="ap-rounds">${Array.from({ length: canAdd }, (_, k) => `<option value="${k + 1}" ${k + 1 === canAdd ? 'selected' : ''}>${k + 1} round${k ? 's' : ''} · ${money(each * BigInt(k + 1))}</option>`).join('')}</select></label>
        <button class="primary" id="ap-on">${covered ? 'Add rounds' : 'Turn on autopay'}</button>
      </div>` : covered ? '<p class="small-text">Every remaining round is covered.</p>' : ''}
      ${covered ? `<button class="ghost small" id="ap-off">Stop autopay · return ${money(prepaid)}</button>` : ''}
      <p class="error" id="ap-err"></p>
    </section>`

  root.innerHTML = `
    <section class="card pool-hero" data-pool-id="${p.id}">
      <button class="link back light" id="back">← Pools</button>
      <div class="pool-title"><h2>${ctx.esc(p.name)}</h2>${statusChip(p)}<span class="chip-s light">${modeName(p.mode)}</span></div>
      <p class="label">Pool balance</p>
      <p class="amount">${money(p.balance)}</p>
      <div class="pool-stats">
        <div><span>Pot</span><strong>${money(pot)}</strong></div>
        <div><span>Each pays</span><strong>${money(p.contribution)}</strong></div>
        <div><span>Round</span><strong>${p.status === 'done' ? `${n} of ${n}` : p.status === 'active' ? `${p.round + 1} of ${n}` : '—'}</strong></div>
        ${p.insurance_bps ? `<div><span>Insurance</span><strong>${money(p.insurance ?? 0)}</strong></div>` : ''}
      </div>
      ${p.status === 'active' ? `
      <div class="progress" role="progressbar" aria-valuenow="${paidCount}" aria-valuemax="${n}"><span data-w="${(paidCount / n) * 100}"></span></div>
      <p class="progress-label">${paidCount} of ${n} paid this round · ${label(receiver)} receives${nextUp ? ` · next: ${label(nextUp)}` : ''}${dueAt(p) ? ` · ${dueIn(dueAt(p))} (${new Date(dueAt(p)).toLocaleDateString()})` : ''}</p>` : ''}
      ${p.round_secs ? `<p class="progress-label">Rounds ${cadence(p.round_secs)}${p.deposit ? ` · deposit ${money(p.deposit)} per member` : ''}</p>` : ''}
    </section>
    ${askedMe.length ? `<p class="banner warn">${askedMe.map(label).join(', ')} asked to swap payout turns with you. Tap "Accept swap" next to their name to trade places.</p>` : ''}
    <section class="card">
      <div class="stack" id="actions">${action}</div>
      <p class="error" id="err"></p>
    </section>
    ${autopay}
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
  fillTrust(root)
  ctx.$('#share').onclick = () => {
    const link = `${location.origin}/?pool=${p.id}`
    navigator.clipboard.writeText(link).then(() => ctx.toast('Pool link copied'))
  }
  const apRun = async (btn, args, done) => {
    const t = btn.textContent
    btn.disabled = true
    btn.textContent = 'Confirming…'
    try {
      await waitForCommit(await poolOp({ seed: ctx.state.seed, id: p.id, asset: p.asset ?? '', ...args }))
      ctx.toast(done)
      renderPool(p.id)
    } catch (err) {
      ctx.$('#ap-err').textContent = err.message
      btn.disabled = false
      btn.textContent = t
    }
  }
  const run = async (btn, args, done) => {
    const t = btn.textContent
    btn.disabled = true
    btn.textContent = 'Confirming…'
    try {
      await waitForCommit(await poolOp({ seed: ctx.state.seed, id: p.id, asset: p.asset ?? '', ...args }))
      ctx.toast(done)
      renderPool(p.id)
    } catch (err) {
      ctx.$('#err').textContent = err.message
      btn.disabled = false
      btn.textContent = t
    }
  }
  const amountOf = (sel) => {
    const v = parseAmount(ctx.$(sel).value.replace(/[,₦\s]/g, ''))
    if (v === 0n) throw new Error('Enter an amount.')
    return v
  }
  if (ctx.$('#cancel-circle'))
    ctx.$('#cancel-circle').onclick = (e) =>
      confirm('Delete this circle? Anyone who joined gets their deposit back. This cannot be undone.') && run(e.target, { op: 'cancel' }, 'Circle deleted')
  if (ctx.$('#nudge'))
    ctx.$('#nudge').onclick = async (e) => {
      e.target.disabled = true
      try {
        const r = await api.nudgePool(ctx.state.seed, p.id)
        ctx.toast(`Reminded ${r.reminded} member${r.reminded === 1 ? '' : 's'}${r.sms ? ` (${r.sms} by SMS)` : ''}`)
      } catch (err) {
        ctx.$('#err').textContent = err.message
      }
    }
  if (ctx.$('#part-go'))
    ctx.$('#part-go').onclick = (e) => {
      try {
        run(e.target, { op: 'contribute', contribution: amountOf('#part') }, 'Part payment received')
      } catch (err) {
        ctx.$('#err').textContent = err.message
      }
    }
  if (ctx.$('#bid-go'))
    ctx.$('#bid-go').onclick = (e) => {
      try {
        run(e.target, { op: 'bid', contribution: amountOf('#bid') }, 'Bid placed')
      } catch (err) {
        ctx.$('#err').textContent = err.message
      }
    }
  root.querySelectorAll('[data-swap]').forEach((b) => (b.onclick = () => {
    const accepting = p.swap_with?.[p.members.indexOf(b.dataset.swap)] === ctx.state.address
    if (!confirm(accepting ? `Trade payout turns with ${label(b.dataset.swap)}?` : `Ask ${label(b.dataset.swap)} to trade payout turns? It happens when they accept.`)) return
    run(b, { op: 'swap', other: b.dataset.swap }, accepting ? 'Turns swapped' : 'Swap requested')
  }))
  if (ctx.$('#ap-on'))
    ctx.$('#ap-on').onclick = (e) =>
      apRun(e.target, { op: 'autopay', contribution: each * BigInt(ctx.$('#ap-rounds').value) }, 'Autopay is on')
  if (ctx.$('#ap-off'))
    ctx.$('#ap-off').onclick = (e) =>
      confirm(`Stop autopay and return ${money(prepaid)} to your wallet?`) && apRun(e.target, { op: 'stop_autopay' }, 'Autopay stopped')
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
      <p class="amount">${money(d.contribution)}</p>
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
        ${organizer && d.members.length > 1 ? `<span class="row-actions"><button class="ghost small" data-up="${i}" ${i === 0 ? 'disabled' : ''}>↑</button><button class="ghost small" data-down="${i}" ${i === d.members.length - 1 ? 'disabled' : ''}>↓</button>${m !== d.organizer ? `<button class="ghost small" data-remove="${m}" aria-label="Remove">✕</button>` : ''}</span>` : ''}</li>`).join('')}</ul>
      ${organizer && d.status === 'open' ? `<button class="primary" id="start" ${d.members.length < 2 ? 'disabled' : ''}>Start the pool with ${d.members.length} member${d.members.length === 1 ? '' : 's'}</button>
        <p class="hint">Starting puts the pool on-chain with this order. Members then confirm${d.deposit ? ' and pay their deposit' : ''}.</p>
        <button class="link danger-link" id="delete-invite">Delete this circle</button>` : ''}
      ${d.status === 'cancelled' ? '<p class="banner">The organiser deleted this circle.</p>' : ''}
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
  if (ctx.$('#delete-invite'))
    ctx.$('#delete-invite').onclick = (e) =>
      confirm('Delete this circle? Everyone who joined the invite will see it was deleted.') &&
      act(e.target, () => api.inviteAction(ctx.state.seed, id, 'delete'))
  ctx.root().querySelectorAll('[data-remove]').forEach((b) => (b.onclick = () =>
    confirm(`Remove ${label(b.dataset.remove)} from this circle?`) && act(b, () => api.inviteAction(ctx.state.seed, id, 'remove', { member: b.dataset.remove }))))
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
          mode: d.mode ?? '', insuranceBps: d.insurance_bps ?? 0, target: BigInt(d.target ?? 0),
        })
        await waitForCommit(poolId)
        await api.inviteAction(ctx.state.seed, id, 'started', { pool_id: poolId })
        ctx.toast('Pool started. Members can now confirm their place.')
      })
}
