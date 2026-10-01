// Trust badges: a reputation built only from on-chain history (ajo rounds
// paid or missed, escrows completed, refunded or disputed) plus ORPay
// verifications. Shown wherever you're about to trust someone with money.
import { api } from './orp.js'

const cache = new Map()
export function trustOf(who) {
  if (!who) return Promise.resolve(null)
  const hit = cache.get(who)
  if (hit && Date.now() - hit.at < 60_000) return hit.p
  const p = api.trust(who).catch(() => null)
  cache.set(who, { at: Date.now(), p })
  return p
}

const LEVEL = {
  new: { label: 'New', cls: 'lv-new' },
  building: { label: 'Building trust', cls: 'lv-building' },
  trusted: { label: 'Trusted', cls: 'lv-trusted' },
  excellent: { label: 'Excellent', cls: 'lv-excellent' },
}
const BADGE = {
  phone_verified: '📱 Phone verified',
  verified_business: '🏢 Verified business',
  ajo_perfect: '✓ Never missed an ajo payment',
  highly_rated: '★ Highly rated by buyers',
  top_seller: '★ Top seller',
}

const esc = (s) => String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c])

// Small inline chip: shield + score + level.
export function trustChip(t) {
  if (!t) return ''
  const lv = LEVEL[t.level] ?? LEVEL.new
  return `<span class="trust-chip ${lv.cls}" title="Trust score from on-chain history"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3 4.5 6v5.5c0 4.6 3.2 8.2 7.5 9.5 4.3-1.3 7.5-4.9 7.5-9.5V6z"/></svg>${t.level === 'new' ? 'New' : `${t.score} · ${lv.label}`}${t.rating?.count ? ` · ★ ${t.rating.average.toFixed(1)}` : ''}</span>`
}

// Full card: score ring, level, facts and badges.
export function trustCard(t, { title = 'Trust profile' } = {}) {
  if (!t) return ''
  const lv = LEVEL[t.level] ?? LEVEL.new
  const since = t.since ? new Date(t.since).toLocaleDateString([], { month: 'short', year: 'numeric' }) : null
  const facts = [
    since ? `On ORPay since ${since}` : 'No activity yet',
    t.ajo.circles ? `${t.ajo.rounds_paid} ajo payment${t.ajo.rounds_paid === 1 ? '' : 's'} made${t.ajo.rounds_missed ? `, <b class="bad">${t.ajo.rounds_missed} missed</b>` : ', none missed'}` : null,
    t.ajo.finished ? `${t.ajo.finished} circle${t.ajo.finished === 1 ? '' : 's'} completed` : null,
    t.escrow.sold_completed ? `${t.escrow.sold_completed} sale${t.escrow.sold_completed === 1 ? '' : 's'} completed through escrow` : null,
    t.escrow.bought_completed ? `${t.escrow.bought_completed} purchase${t.escrow.bought_completed === 1 ? '' : 's'} completed` : null,
    t.escrow.refunded_as_seller ? `<b class="bad">${t.escrow.refunded_as_seller} sale${t.escrow.refunded_as_seller === 1 ? '' : 's'} refunded</b>` : null,
    t.escrow.disputes ? `<b class="bad">${t.escrow.disputes} dispute${t.escrow.disputes === 1 ? '' : 's'}</b>` : null,
  ].filter(Boolean)
  const deg = Math.round((t.score / 100) * 360)
  return `
    <div class="trust-card ${lv.cls}">
      <div class="trust-top">
        <span class="trust-ring" data-deg="${deg}"><b>${t.level === 'new' ? '–' : t.score}</b></span>
        <span><small>${esc(title)}</small><strong>${lv.label}</strong>${t.username ? `<em>@${esc(t.username)}</em>` : ''}</span>
      </div>
      ${t.rating?.count ? `<p class="trust-rating"><b>★ ${t.rating.average.toFixed(1)}</b> from ${t.rating.count} verified buyer${t.rating.count === 1 ? '' : 's'}</p>
        ${t.rating.recent.filter((r) => r.text).slice(0, 3).map((r) => `<blockquote class="review">${'★'.repeat(r.stars)} “${esc(r.text)}”</blockquote>`).join('')}` : ''}
      ${t.badges?.length ? `<div class="trust-badges">${t.badges.map((b) => `<span>${BADGE[b] ?? esc(b)}</span>`).join('')}</div>` : ''}
      <ul class="trust-facts">${facts.map((f) => `<li>${f}</li>`).join('')}</ul>
      <p class="trust-note">Built only from payments recorded on OpenReserve. It can't be bought or edited.</p>
    </div>`
}

// Fill every <span data-trust="address"> under root with a trust chip, and
// size score rings (set via the CSSOM: the CSP forbids inline styles).
export function fillTrust(root) {
  root.querySelectorAll('[data-deg]').forEach((el) => el.style.setProperty('--deg', `${el.dataset.deg}deg`))
  root.querySelectorAll('[data-trust]').forEach(async (el) => {
    const t = await trustOf(el.dataset.trust)
    if (el.isConnected && t) el.innerHTML = trustChip(t)
  })
}

// Replace el's content with a full trust card for who.
export async function showTrustCard(el, who, opts) {
  const t = await trustOf(who)
  if (!el.isConnected || !t) return
  el.innerHTML = trustCard(t, opts)
  fillTrust(el)
}
