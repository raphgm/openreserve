// @openreserve/orpay: server-side SDK for apps that accept ORP through ORPay.
// Zero dependencies. Uses only fetch and Web Crypto, so it runs on Node 18+,
// Cloudflare Workers / Pages Functions, Deno and Bun without flags.

export class ORPayError extends Error {
  constructor(message, status) {
    super(message)
    this.name = 'ORPayError'
    this.status = status
  }
}

const ESCROW_ACTIONS = ['release', 'dispute', 'dispatch', 'refund', 'resolve', 'claim']

export class ORPay {
  /**
   * @param {{ apiKey: string, baseUrl?: string }} opts
   *   apiKey: your app's secret key (orp_sk_...). Keep it on your server.
   *   baseUrl: the ORPay deployment, e.g. https://pay.openreserve.app
   */
  constructor({ apiKey, baseUrl = 'http://localhost:4000' } = {}) {
    if (!apiKey?.startsWith('orp_sk_')) throw new Error('ORPay: apiKey must start with orp_sk_')
    this.apiKey = apiKey
    this.baseUrl = baseUrl.replace(/\/$/, '')
  }

  async #call(method, path, body) {
    const res = await fetch(this.baseUrl + path, {
      method,
      headers: { Authorization: `Bearer ${this.apiKey}`, 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
    const data = await res.json().catch(() => ({}))
    if (!res.ok) throw new ORPayError(data.error ?? `ORPay request failed (${res.status})`, res.status)
    return data
  }

  /**
   * Create a checkout. Redirect the customer to the returned checkout_url.
   * @param {{ amount: string, description?: string, reference?: string, returnUrl?: string, expiresInMinutes?: number }} p
   *   amount is a decimal string in ORP, e.g. "12.50" (never a float).
   */
  createCheckout({ amount, description, reference, returnUrl, expiresInMinutes } = {}) {
    if (typeof amount !== 'string') throw new TypeError('ORPay: amount must be a decimal string like "12.50"')
    return this.#call('POST', '/api/v1/checkout', {
      amount, description, reference, return_url: returnUrl, expires_in_minutes: expiresInMinutes,
    })
  }

  /** Fetch a checkout's current status. */
  getCheckout(id) {
    return this.#call('GET', `/api/v1/checkout/${encodeURIComponent(id)}`)
  }

  /**
   * Ask a buyer to lock funds in escrow, released to the seller milestone
   * by milestone (e.g. hold a developer's payout until work is approved).
   * Send the buyer to the returned funding_url.
   * @param {{ seller: string, milestones: {label?: string, amount: string}[], currency?: string,
   *   arbiter?: string, description?: string, reference?: string, returnUrl?: string,
   *   shipByDays?: number, reviewDays?: number, fundWithinHours?: number }} p
   *   seller/arbiter are @usernames or addresses; the arbiter defaults to your app's arbiter.
   */
  createEscrow({ seller, milestones, currency, arbiter, description, reference, returnUrl, shipByDays, reviewDays, fundWithinHours } = {}) {
    if (!Array.isArray(milestones) || milestones.some((m) => typeof m.amount !== 'string')) {
      throw new TypeError('ORPay: milestones must be [{ label, amount: "40000.00" }]')
    }
    return this.#call('POST', '/api/v1/escrows', {
      seller, milestones, currency, arbiter, description, reference, return_url: returnUrl,
      ship_by_days: shipByDays, review_days: reviewDays, fund_within_hours: fundWithinHours,
    })
  }

  /** Fetch an escrow request with its live on-chain state. */
  getEscrow(id) {
    return this.#call('GET', `/api/v1/escrows/${encodeURIComponent(id)}`)
  }

  listEscrows() {
    return this.#call('GET', '/api/v1/escrows')
  }

  /**
   * Withdraw an escrow request the buyer has not funded yet.
   * Funded escrows cannot be cancelled by your app: see escrowActionUrl.
   */
  cancelEscrow(id) {
    return this.#call('POST', `/api/v1/escrows/${encodeURIComponent(id)}/cancel`)
  }

  /**
   * Link that opens a funded escrow in ORPay at one step, for the person who
   * must sign it. Money in escrow only moves when the buyer, seller or
   * arbiter signs with their own wallet; your API key can never release or
   * refund it. Send the right person this link, then wait for the webhook
   * (escrow.milestone_released, escrow.refunded, escrow.completed, ...).
   *
   *   release  buyer approves the next milestone and pays the seller
   *   dispute  buyer or seller freezes the escrow for the arbiter
   *   dispatch seller marks the goods shipped or the work delivered
   *   refund   seller returns the funds (or buyer reclaims after ship-by)
   *   resolve  arbiter splits a disputed escrow
   *
   * @param {{ escrow_url?: string }} escrow  an escrow from getEscrow / a webhook
   * @param {'release'|'dispute'|'dispatch'|'refund'|'resolve'|'claim'} action
   */
  escrowActionUrl(escrow, action) {
    if (!escrow?.escrow_url) throw new ORPayError('Escrow is not funded yet: send the buyer to funding_url first', 409)
    if (!ESCROW_ACTIONS.includes(action)) throw new TypeError(`ORPay: action must be one of ${ESCROW_ACTIONS.join(', ')}`)
    return `${escrow.escrow_url}&action=${action}`
  }

  /** List recent checkouts, optionally filtered by status (pending | paid | expired). */
  listCheckouts({ status } = {}) {
    return this.#call('GET', `/api/v1/checkout${status ? `?status=${encodeURIComponent(status)}` : ''}`)
  }
}

/**
 * Verify an ORPay webhook and return the parsed event. Throws if the
 * signature is invalid or older than toleranceSeconds (replay protection).
 * Pass the raw request body exactly as received, before any JSON parsing.
 *
 * @param {string|ArrayBuffer|Uint8Array} rawBody  (a Node Buffer works too)
 * @param {string} signatureHeader  the ORPay-Signature header
 * @param {string} secret           your app's webhook secret (whsec_...)
 */
export async function verifyWebhook(rawBody, signatureHeader, secret, { toleranceSeconds = 300, now = Date.now() } = {}) {
  const parts = Object.fromEntries(
    String(signatureHeader ?? '').split(',').map((kv) => kv.trim().split('=', 2)),
  )
  const ts = Number(parts.t)
  if (!Number.isInteger(ts) || !/^[0-9a-f]{64}$/.test(parts.v1 ?? '')) throw new ORPayError('Malformed ORPay-Signature header', 400)
  if (Math.abs(now / 1000 - ts) > toleranceSeconds) throw new ORPayError('Webhook timestamp outside tolerance', 400)
  const enc = new TextEncoder()
  const body = typeof rawBody === 'string' ? enc.encode(rawBody) : new Uint8Array(rawBody)
  const key = await crypto.subtle.importKey('raw', enc.encode(secret), { name: 'HMAC', hash: 'SHA-256' }, false, ['verify'])
  const signed = new Uint8Array([...enc.encode(`${ts}.`), ...body])
  const given = new Uint8Array(parts.v1.match(/../g).map((h) => parseInt(h, 16)))
  // crypto.subtle.verify compares in constant time.
  if (!(await crypto.subtle.verify('HMAC', key, given, signed))) throw new ORPayError('Invalid webhook signature', 400)
  return JSON.parse(new TextDecoder().decode(body))
}
