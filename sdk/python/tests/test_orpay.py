import unittest

from orpay import ORPay, ORPayError, verify_webhook

BODY = '{"type":"invoice.paid","created":1,"data":{"id":"abc","status":"paid"}}'
# Produced by the Go server: SignWebhook("whsec_test", 1700000000, BODY).
GO_HEADER = "t=1700000000,v1=556489ff3b74ef0ccdfeba8df715a5b540e49d7dcbadd3a1aa2f9965bcade456"


class WebhookTests(unittest.TestCase):
    def test_matches_go_server(self):
        ev = verify_webhook(BODY, GO_HEADER, "whsec_test", now=1700000000)
        self.assertEqual(ev["type"], "invoice.paid")

    def test_rejects_tampering_wrong_secret_and_replay(self):
        for body, secret, now in [(BODY.replace("paid", "expired"), "whsec_test", 1700000000),
                                  (BODY, "whsec_other", 1700000000), (BODY, "whsec_test", 1700003600)]:
            with self.assertRaises(ORPayError):
                verify_webhook(body, GO_HEADER, secret, now=now)
        with self.assertRaises(ORPayError):
            verify_webhook(BODY, "garbage", "whsec_test", now=1700000000)


class ClientTests(unittest.TestCase):
    def test_key_and_amounts(self):
        with self.assertRaises(ValueError):
            ORPay(api_key="pk_live")
        c = ORPay(api_key="orp_sk_x")
        with self.assertRaises(TypeError):
            c.create_checkout(12.5)

    def test_escrow_action_url(self):
        e = {"escrow_url": "https://pay.example/?escrow=abc"}
        self.assertEqual(ORPay.escrow_action_url(e, "release"), "https://pay.example/?escrow=abc&action=release")
        with self.assertRaises(ORPayError):
            ORPay.escrow_action_url({}, "release")
        with self.assertRaises(ValueError):
            ORPay.escrow_action_url(e, "steal")


if __name__ == "__main__":
    unittest.main()
