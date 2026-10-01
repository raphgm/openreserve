# Free test network: Oracle Cloud + Cloudflare Pages

- **Oracle Cloud "Always Free"** runs the chain node, ORPay server, naira gateway
  and HTTPS (one ARM VM, always on, $0).
- **Cloudflare Pages** hosts the wallet website ($0), e.g. https://orpay.pages.dev.

Test network only: Paystack test keys, faucet on, no real money.

## 1. Oracle VM (about 15 minutes)

1. Sign up at https://cloud.oracle.com (card needed for identity; Always Free is not charged).
2. Compute → Instances → **Create instance**
   - Image: **Ubuntu 24.04**
   - Shape: **Ampere (VM.Standard.A1.Flex)**, 2 OCPU, 12 GB (inside the free allowance)
   - Add your SSH public key (`~/.ssh/id_ed25519.pub`)
3. Networking → your VCN → Security list → **Add ingress rules**: TCP 80 and 443 from `0.0.0.0/0`.
4. Note the instance's **public IP**.

## 2. Start ORPay on it

```bash
ssh ubuntu@<PUBLIC_IP>
curl -fsSL https://raw.githubusercontent.com/raphgm/openreserve/main/deploy/oracle/setup.sh | sudo bash -s -- <YOUR_ADMIN_WALLET_ADDRESS> https://orpay.pages.dev
```

When it finishes: https://<PUBLIC_IP>.sslip.io/healthz should answer. The script
prints where your keys are: **back up `/opt/openreserve/deploy/keys` and `.env`**.

Add your Paystack **test** key to `/opt/openreserve/deploy/.env`, then
`cd /opt/openreserve/deploy && sudo docker compose up -d`.
In Paystack set the webhook URL to `https://<PUBLIC_IP>.sslip.io/pay/webhooks/paystack`.

## 3. Wallet on Cloudflare Pages (on your Mac)

```bash
cd apps/orpay/frontend
npx wrangler login
VITE_API_BASE=https://<PUBLIC_IP>.sslip.io npm run deploy:pages
```

The first run creates the `orpay` Pages project and prints its URL
(https://orpay.pages.dev, or orpay-xxxx.pages.dev if the name is taken; if so,
put that URL in WEB_URL in the server's `.env` and run `docker compose up -d`).

Later, a custom domain (e.g. pay.yourdomain.com) can point at Pages, and
api.yourdomain.com at the VM (set DOMAIN in `.env`).

## Updating

```bash
ssh ubuntu@<PUBLIC_IP> 'cd /opt/openreserve && sudo git pull && cd deploy && sudo docker compose up -d --build'
VITE_API_BASE=https://<PUBLIC_IP>.sslip.io npm run deploy:pages
```
