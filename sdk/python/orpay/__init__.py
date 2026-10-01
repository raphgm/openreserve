"""ORPay server SDK for Python: checkouts, milestone escrow and webhooks.

Zero dependencies (standard library only), Python 3.9+. Keep your API key
on your server.

    from orpay import ORPay, verify_webhook
    orpay = ORPay(api_key=os.environ["ORPAY_KEY"], base_url="https://pay.example")
    escrow = orpay.create_escrow(seller="@gadgethub", currency="NGN",
                                 milestones=[{"label": "iPhone 14", "amount": "420000"}])
    # send the buyer to escrow["funding_url"]
"""

from __future__ import annotations

import hashlib
import hmac
import json
import time
import urllib.error
import urllib.parse
import urllib.request
from typing import Any, Optional

__all__ = ["ORPay", "ORPayError", "verify_webhook", "ESCROW_ACTIONS"]
__version__ = "0.2.0"

ESCROW_ACTIONS = ("release", "dispute", "dispatch", "refund", "resolve", "claim")


class ORPayError(Exception):
    def __init__(self, message: str, status: Optional[int] = None):
        super().__init__(message)
        self.status = status


class ORPay:
    def __init__(self, api_key: str, base_url: str = "http://localhost:4000", timeout: float = 15):
        if not api_key or not api_key.startswith("orp_sk_"):
            raise ValueError("ORPay: api_key must start with orp_sk_")
        self.api_key = api_key
        self.base_url = base_url.rstrip("/")
        self.timeout = timeout

    def _call(self, method: str, path: str, body: Any = None) -> Any:
        data = None if body is None else json.dumps({k: v for k, v in body.items() if v is not None}).encode()
        req = urllib.request.Request(self.base_url + path, data=data, method=method, headers={
            "Authorization": f"Bearer {self.api_key}",
            "Content-Type": "application/json",
            "User-Agent": f"orpay-python/{__version__}",
        })
        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as resp:
                return json.loads(resp.read() or b"{}")
        except urllib.error.HTTPError as e:
            try:
                msg = json.loads(e.read()).get("error")
            except Exception:
                msg = None
            raise ORPayError(msg or f"ORPay request failed ({e.code})", e.code) from None

    # Checkouts --------------------------------------------------------------

    def create_checkout(self, amount: str, description: str = None, reference: str = None,
                        return_url: str = None, expires_in_minutes: int = None) -> dict:
        """Create a checkout; redirect the customer to checkout_url. amount is a decimal string."""
        if not isinstance(amount, str):
            raise TypeError('ORPay: amount must be a decimal string like "12.50"')
        return self._call("POST", "/api/v1/checkout", {"amount": amount, "description": description,
                          "reference": reference, "return_url": return_url, "expires_in_minutes": expires_in_minutes})

    def get_checkout(self, checkout_id: str) -> dict:
        return self._call("GET", f"/api/v1/checkout/{urllib.parse.quote(checkout_id)}")

    def list_checkouts(self, status: str = None) -> list:
        q = f"?status={urllib.parse.quote(status)}" if status else ""
        return self._call("GET", f"/api/v1/checkout{q}")

    # Escrow -----------------------------------------------------------------

    def create_escrow(self, seller: str, milestones: list, currency: str = None, arbiter: str = None,
                      description: str = None, reference: str = None, return_url: str = None,
                      ship_by_days: int = None, review_days: int = None, fund_within_hours: int = None,
                      policy: str = None, kind: str = None, checklist: list = None) -> dict:
        """Ask a buyer to lock funds; send them to funding_url. Milestone amounts are decimal strings."""
        if not milestones or any(not isinstance(m.get("amount"), str) for m in milestones):
            raise TypeError('ORPay: milestones must be [{"label": ..., "amount": "40000.00"}]')
        return self._call("POST", "/api/v1/escrows", {"seller": seller, "milestones": milestones,
                          "currency": currency, "arbiter": arbiter, "description": description,
                          "reference": reference, "return_url": return_url, "ship_by_days": ship_by_days,
                          "review_days": review_days, "fund_within_hours": fund_within_hours,
                          "policy": policy, "kind": kind, "checklist": checklist})

    def get_escrow(self, escrow_id: str) -> dict:
        return self._call("GET", f"/api/v1/escrows/{urllib.parse.quote(escrow_id)}")

    def list_escrows(self) -> list:
        return self._call("GET", "/api/v1/escrows")

    def cancel_escrow(self, escrow_id: str) -> dict:
        """Withdraw an escrow request nobody has funded yet."""
        return self._call("POST", f"/api/v1/escrows/{urllib.parse.quote(escrow_id)}/cancel")

    @staticmethod
    def escrow_action_url(escrow: dict, action: str) -> str:
        """Link for the buyer, seller or arbiter to sign one step of a funded escrow.

        Your API key can never move escrowed money; send the right person this
        link and wait for the webhook (escrow.milestone_released, escrow.refunded...).
        """
        if not escrow.get("escrow_url"):
            raise ORPayError("Escrow is not funded yet: send the buyer to funding_url first", 409)
        if action not in ESCROW_ACTIONS:
            raise ValueError(f"ORPay: action must be one of {', '.join(ESCROW_ACTIONS)}")
        return f"{escrow['escrow_url']}&action={action}"


def verify_webhook(raw_body, signature_header: str, secret: str, tolerance_seconds: int = 300,
                   now: Optional[float] = None) -> dict:
    """Verify an ORPay webhook and return the event. Pass the raw request body."""
    parts = dict(p.strip().split("=", 1) for p in (signature_header or "").split(",") if "=" in p)
    try:
        ts = int(parts.get("t", ""))
    except ValueError:
        raise ORPayError("Malformed ORPay-Signature header", 400) from None
    sig = parts.get("v1", "")
    if len(sig) != 64:
        raise ORPayError("Malformed ORPay-Signature header", 400)
    if abs((time.time() if now is None else now) - ts) > tolerance_seconds:
        raise ORPayError("Webhook timestamp outside tolerance", 400)
    body = raw_body.encode() if isinstance(raw_body, str) else bytes(raw_body)
    expected = hmac.new(secret.encode(), f"{ts}.".encode() + body, hashlib.sha256).hexdigest()
    if not hmac.compare_digest(expected, sig):
        raise ORPayError("Invalid webhook signature", 400)
    return json.loads(body)
