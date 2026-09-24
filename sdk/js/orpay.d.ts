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

export interface EscrowRequest {
  id: string
  app_id: string
  seller: string
  arbiter: string
  currency: string
  milestones: { label: string; amount: number; amount_display: string }[]
  total: number
  description: string
  reference?: string
  status: 'awaiting_funding' | 'linked' | 'expired'
  funding_url: string
  escrow_id?: string
  escrow_url?: string
  /** Live on-chain state once funded. */
  escrow?: {
    status: 'funded' | 'dispatched' | 'disputed' | 'completed' | 'refunded' | 'resolved'
    released: number
    balance: number
    tracking?: string
    paid_seller: number
    paid_buyer: number
  }
}

export type WebhookEvent =
  | { type: 'invoice.paid' | 'invoice.expired'; created: number; data: Checkout }
  | {
      type:
        | 'escrow.funded' | 'escrow.dispatched' | 'escrow.milestone_released' | 'escrow.disputed'
        | 'escrow.completed' | 'escrow.refunded' | 'escrow.resolved' | 'escrow.expired'
      created: number
      data: EscrowRequest
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
  createEscrow(p: {
    seller: string
    milestones: { label?: string; amount: string }[]
    currency?: string
    arbiter?: string
    description?: string
    reference?: string
    returnUrl?: string
    shipByDays?: number
    reviewDays?: number
    fundWithinHours?: number
  }): Promise<EscrowRequest>
  getEscrow(id: string): Promise<EscrowRequest>
  listEscrows(): Promise<EscrowRequest[]>
}

export function verifyWebhook(
  rawBody: string | Buffer,
  signatureHeader: string,
  secret: string,
  opts?: { toleranceSeconds?: number; now?: number },
): WebhookEvent
