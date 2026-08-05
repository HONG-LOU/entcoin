# EntPay Agent payment protocol

EntPay is an application-layer payment protocol for Entcoin. It lets an AI
agent purchase a bounded resource with a normal signed ENT transaction while
the private key remains inside the local Entcoin wallet. EntPay does not change
consensus, transaction encoding, addresses, the P2P protocol, or the public
wallet read API.

The first production resource is `network-report`: after one confirmation it
returns a signed receipt and a live comparison of the two public Entcoin archive
nodes. The local Codex agent evaluates the invoice before payment and summarizes
the paid report after delivery.

## Trust boundary

```text
local Codex agent                  public EntPay merchant
  policy decision                   issue signed invoice
        |                                  |
  enforced max amount                       |
        |                                  |
  local wallet signs ---- Entcoin tx ----> local validating node
        |                                  |
  no seed leaves host              wait for confirmation
                                           |
                               signed receipt + report
```

- Codex may approve or reject an invoice, but hard checks independently enforce
  the HTTPS endpoint, network, merchant, resource, expiry, signature, and maximum
  amount.
- The merchant never receives a seed phrase or private key. It receives only the
  signed transaction already intended for public broadcast.
- Invoice and receipt signatures use a per-deployment Ed25519 key. The public key
  is advertised by `GET /v1/info`; HTTPS remains the discovery trust root.
- The claim token is a random bearer capability. Only its SHA-256 digest is
  stored. It must not be logged, placed in a URL, or shared with another agent.
- A transaction ID is unique across invoices. SQLite atomically rejects replay.
  Submission and delivery are idempotent, so a lost HTTP response can be retried.

## Flow

1. `POST /v1/invoices` creates a 15-minute invoice.
2. The agent validates the service metadata and Ed25519 invoice signature.
3. Codex evaluates the invoice; the deterministic policy still enforces the
   configured maximum amount.
4. The local wallet selects UTXOs, signs, stores, and broadcasts a standard ENT
   transaction.
5. `POST /v1/invoices/{id}/submit` checks one exact merchant output and forwards
   the transaction through the merchant's validating node.
6. `POST /v1/invoices/{id}/claim` returns `202` until the required confirmation,
   then returns the report and signed receipt. Repeating the claim returns the
   same persisted delivery.

All private invoice operations require:

```http
Authorization: Bearer <claim_token>
```

The public service rejects unknown JSON fields, duplicate keys, trailing data,
more than 128 KiB, and nesting deeper than 32 levels. Creation and payment
requests are rate-limited per proxy-verified client IP.

## API

Create an invoice:

```http
POST /v1/invoices
Content-Type: application/json

{
  "resource": "network-report",
  "query": "Compare both public nodes and summarize the network state."
}
```

The response contains an `entpay-v1` invoice and a separate `claim_token`.
Amounts are unsigned integer Entcoin atoms. The initial production price is
`10000` atoms (`0.00010000 ENT`) and delivery requires one confirmation.

Service discovery and health:

```text
GET /v1/info
GET /healthz
```

Production endpoints:

```text
https://entcoin.xyz/entpay/
https://template-chat.xyz/entpay/
```

## Agent client

The Agent client must exclusively lock the selected Entcoin data directory. Stop
the desktop wallet before invoking it. A dedicated wallet profile can be selected
with `--wallet`; the previous active profile is restored before the database is
closed.

```bash
entpay-linux-amd64 agent \
  --endpoint https://entcoin.xyz/entpay/ \
  --data /path/to/Entropy/mainnet-v1 \
  --wallet ent1... \
  --max-amount 0.00010000 \
  --query "Compare both public nodes and summarize the network state."
```

The client uses the locally configured `codex` executable by default. `--model`
may select another configured model. Codex credentials are never copied to the
merchant server.

## Merchant deployment

Build the static Linux service:

```bash
go build -trimpath -ldflags="-s -w" -o entpay ./cmd/entpay
```

Generate a signing key on each merchant host:

```bash
install -d -m 0700 /etc/entpay
entpay generate-key --output /etc/entpay/signing.key
```

Install `deploy/linux-seed/entpay.service`, create an environment file from
`entpay.env.example`, and proxy `/entpay/*` to `127.0.0.1:47841` while stripping
the prefix. The service account needs write access only to `/var/lib/entpay` and
read access to its environment and signing key.

## Limitations

- Version 1 delivers only after one confirmation. A one-block reorganization can
  still reverse payment after delivery; do not use it for high-value resources.
- The verifier uses the merchant address's bounded wallet history. The current
  deployment is intended for low-volume demonstration and community services,
  not high-frequency settlement.
- ENT is not a stable unit of account and does not have broad merchant liquidity.
- There is no subscription debit, escrow, refund transaction, payment channel,
  stablecoin, bridge, or custody service.
- The paid report proves what the configured nodes returned at delivery time; it
  is not an independent security audit or investment signal.
