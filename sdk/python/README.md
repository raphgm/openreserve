# orpay (Python)

Server SDK for apps that take payments through ORPay on OpenReserve: checkouts,
milestone escrow (cancel, action links) and webhook verification. Standard
library only, Python 3.9+.

```python
import os
from orpay import ORPay, verify_webhook

orpay = ORPay(api_key=os.environ["ORPAY_KEY"], base_url="https://pay.example")

escrow = orpay.create_escrow(
    seller="@gadgethub", currency="NGN", description="iPhone 14",
    milestones=[{"label": "Item", "amount": "420000"}],
    checklist=["IMEI matches", "Battery health as described"],
)
# send the buyer to escrow["funding_url"]

# Later, ask the buyer to release (your key can never move escrowed money):
link = orpay.escrow_action_url(orpay.get_escrow(escrow["id"]), "release")
```

Webhooks (Flask example; pass the raw body):

```python
@app.post("/orpay/webhook")
def hook():
    try:
        event = verify_webhook(request.get_data(), request.headers["ORPay-Signature"], os.environ["ORPAY_WEBHOOK_SECRET"])
    except Exception:
        return "", 400
    if event["type"] == "escrow.completed":
        mark_order_paid(event["data"]["reference"])
    return "", 200
```

Run the tests: `python3 -m unittest discover -s tests`
