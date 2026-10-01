// Scam radar: plain-language warnings before money moves, built from the
// other person's on-chain record and from what they write in escrow chats.

const DAY = 86_400_000

// riskOf turns a trust profile into a warning level and reasons.
export function riskOf(t) {
  if (!t) return null
  const reasons = []
  let high = false
  if (!t.since) reasons.push('This account has no payment history yet')
  else if (Date.now() - t.since < 7 * DAY) reasons.push('This account is less than a week old')
  if (t.escrow?.disputes) {
    reasons.push(`${t.escrow.disputes} escrow dispute${t.escrow.disputes === 1 ? '' : 's'} on record`)
    high ||= t.escrow.disputes >= 2
  }
  if (t.escrow?.refunded_as_seller) {
    reasons.push(`${t.escrow.refunded_as_seller} sale${t.escrow.refunded_as_seller === 1 ? '' : 's'} refunded to buyers`)
    high ||= t.escrow.refunded_as_seller >= 2
  }
  if (t.ajo?.rounds_missed) {
    reasons.push(`Missed ${t.ajo.rounds_missed} ajo payment${t.ajo.rounds_missed === 1 ? '' : 's'}`)
    high ||= t.ajo.rounds_missed >= 2
  }
  if (!reasons.length) return null
  return { level: high ? 'high' : 'caution', reasons }
}

const esc = (s) => String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c])

// radarBanner: a warning box with reasons and what to do.
export function radarBanner(risk, advice) {
  if (!risk) return ''
  return `<div class="radar ${risk.level}">
    <strong>${risk.level === 'high' ? '⚠ Be careful' : '⚠ Check before you pay'}</strong>
    <ul>${risk.reasons.map((r) => `<li>${esc(r)}</li>`).join('')}</ul>
    <p>${advice}</p>
  </div>`
}

// Messages asking to pay outside ORPay escrow are the commonest scam.
const OFF_PLATFORM = /\b\d{10}\b|account (number|no\.?)|acct (no|number)|transfer (it |the money |directly)|pay (me )?(directly|outside|cash)|outside (the )?(app|escrow|orpay)|cancel (the )?escrow|send (it )?to my (bank|account)|\b(opay|palmpay|moniepoint|kuda|gtb|zenith|access bank|first bank|uba)\b|gift ?card|crypto wallet|usdt/i

export const asksToPayOutside = (text) => OFF_PLATFORM.test(String(text ?? ''))
