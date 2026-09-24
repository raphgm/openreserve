export class ORPayError extends Error {
  status?: number
}

export interface Checkout {
  id: string
  app_id: string
  merchant: string
  amount: number
  amount_orp: string
  description: string
  reference?: string
  memo: string
  status: 'pending' | 'paid' | 'expired'
  checkout_url: string
  return_url?: string
  created_at: string
  expires_at: string
  paid_at?: string
  paid_tx?: string
  paid_by?: string
  height?: number
}

export interface WebhookEvent {
  type: 'invoice.paid' | 'invoice.expired'
  created: number
  data: Checkout
}

export class ORPay {
  constructor(opts: { apiKey: string; baseUrl?: string })
  createCheckout(p: {
    amount: string
    description?: string
    reference?: string
    returnUrl?: string
    expiresInMinutes?: number
  }): Promise<Checkout>
  getCheckout(id: string): Promise<Checkout>
  listCheckouts(p?: { status?: Checkout['status'] }): Promise<Checkout[]>
}

export function verifyWebhook(
  rawBody: string | Buffer,
  signatureHeader: string,
  secret: string,
  opts?: { toleranceSeconds?: number; now?: number },
): WebhookEvent
