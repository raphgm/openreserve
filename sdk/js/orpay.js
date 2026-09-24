// @openreserve/orpay: server-side SDK for apps that accept ORP through ORPay.
// Zero dependencies; runs on Node 18+ (uses global fetch and node:crypto).
import { createHmac, timingSafeEqual } from 'node:crypto'

export class ORPayError extends Error {
  constructor(message, status) {
    super(message)
    this.name = 'ORPayError'
    this.status = status
  }
}

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
 * @param {string|Buffer} rawBody
 * @param {string} signatureHeader  the ORPay-Signature header
 * @param {string} secret           your app's webhook secret (whsec_...)
 */
export function verifyWebhook(rawBody, signatureHeader, secret, { toleranceSeconds = 300, now = Date.now() } = {}) {
  const parts = Object.fromEntries(
    String(signatureHeader ?? '').split(',').map((kv) => kv.trim().split('=', 2)),
  )
  const ts = Number(parts.t)
  if (!Number.isInteger(ts) || !parts.v1) throw new ORPayError('Malformed ORPay-Signature header', 400)
  if (Math.abs(now / 1000 - ts) > toleranceSeconds) throw new ORPayError('Webhook timestamp outside tolerance', 400)
  const body = Buffer.isBuffer(rawBody) ? rawBody : Buffer.from(rawBody)
  const expected = createHmac('sha256', secret).update(`${ts}.`).update(body).digest()
  const given = Buffer.from(parts.v1, 'hex')
  if (given.length !== expected.length || !timingSafeEqual(given, expected)) {
    throw new ORPayError('Invalid webhook signature', 400)
  }
  return JSON.parse(body.toString('utf8'))
}
