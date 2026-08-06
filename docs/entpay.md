# EntPay Agent payment protocol and merchant SDK

EntPay is an application-layer commerce protocol for Entcoin. An Agent can
discover a merchant service, request a signed quote, approve a bounded purchase,
pay with an ordinary ENT transaction, wait for confirmations, and verify the
merchant's signed delivery. Wallet keys remain in the user's local Entcoin data
directory. EntPay does not change consensus, transaction encoding, addresses,
or the P2P protocol.

Entcoin publishes the protocol, generic Agent, and Go merchant SDK. A merchant
owns and deploys its products independently on a laptop, private server, cloud
service, or container platform. Product source code, model credentials, data,
and infrastructure do not belong in the Entcoin repository and do not need to
be public on GitHub.

## Who does what

| Party | Responsibility |
| --- | --- |
| Entcoin | Protocol types, deterministic checks, local Agent wallet payment, Gateway, confirmation tracking, idempotency, Receipt signing, artifact authorization |
| Merchant | Product description, price, input validation, business fulfillment, HTTPS deployment, receiving address, signing credential, operational data |
| User or Agent | Select a merchant and resource, supply input, set a hard maximum, approve intent, sign locally, verify delivery |

Common uses include an Agent purchasing an image generation, data lookup,
document conversion, compute job, API result, report, file, or one-time access
token. Those are examples, not built-in Entcoin products.

## End-to-end result

```text
merchant /v1/info
        ↓ discover products and terms
signed input-bound Invoice
        ↓ deterministic checks + semantic approval
local wallet signs a normal ENT transaction
        ↓ merchant verifies one exact output and confirmations
idempotent Product.Fulfill
        ↓
signed Receipt + JSON payload + optional authorized artifact
```

The final `Receipt` binds the invoice, transaction, resource, original input
hash, JSON payload hash, optional artifact hash, and delivery time. The Agent
verifies every binding before analysis or file storage.

## User and Agent usage

The merchant's web workspace lists products dynamically from `GET /v1/info`.
It can create and display an Invoice, but it never asks for a seed phrase or
private key. The default interactive flow uses the installed Entcoin desktop:
the merchant opens a secret-free `entcoin://pay` bootstrap URI, then POSTs the
separate short-lived handoff code to the desktop loopback relay. Entcoin opens
Agent Pay and performs all verification and payment locally.

The standalone local confirmation UI remains available as a developer and
compatibility path:

```bash
entpay agent-ui \
  --data /path/to/Entropy/mainnet-v1 \
  --wallet ent1... \
  --max-amount 0.01000000 \
  --artifacts ~/Downloads/EntPay
```

It listens only on `127.0.0.1:47831`. The compatibility UI accepts its legacy
URL-fragment envelope, removes it from the address bar immediately, and then
performs the same independent protocol, network, product, price, input hash,
signature, expiry, and local spending-limit checks. This fragment transport is
not used by the installed desktop flow.

The confirmation page shows the exact merchant, request, amount, expiry, and
required confirmations. Rejecting creates no transaction. Approving signs with
the local wallet, broadcasts the ordinary ENT transaction, waits for the
merchant's confirmation requirement, verifies the signed Receipt and content
hashes, and stores any artifact in the configured directory.

The browser merchant origin never receives wallet access. It also does not
display or copy the claim capability. Keep the local Agent bound to loopback;
the CLI rejects non-loopback listen addresses.

For automation and unattended semantic approval, the one-shot CLI remains
available:

```bash
entpay agent \
  --endpoint https://merchant.example/entpay/ \
  --data /path/to/Entropy/mainnet-v1 \
  --wallet ent1... \
  --resource example-resource \
  --input-json '{"request":"Produce the purchased result"}' \
  --max-amount 0.00100000 \
  --artifact-output ./delivery.bin \
  --output ./receipt.json
```

Use `--input-file request.json` instead of `--input-json` for larger input.
`--artifact-output` is optional for JSON-only products and required when the
user wants an artifact saved. Existing files are rejected before payment. The
Agent restores the previously active wallet profile after using a dedicated
wallet.

Codex performs only semantic approval and delivery summarization. Deterministic
Go code independently enforces HTTPS (loopback HTTP is allowed for development),
protocol, network, product terms, merchant, Ed25519 signature, input hash,
expiry, capabilities, and the hard maximum. Codex never receives a wallet seed,
private key, claim token, or signing key.

## Merchant integration

Implement the public `entpay.Product` interface in the merchant's own project:

```go
type Product interface {
    Descriptor() ProductDescriptor
    Validate(context.Context, json.RawMessage) error
    Fulfill(context.Context, FulfillmentRequest) (Fulfillment, error)
}
```

`Descriptor` advertises the resource ID, display name, description, atom price,
required confirmations, visual accent, and input fields. `Validate` performs
cheap input checks before an Invoice exists. `Fulfill` runs only after the
Gateway has verified payment and confirmations.

Register one or more implementations:

```go
gateway, err := entpay.NewGateway(entpay.MerchantConfig{
    MerchantAddress:      merchantAddress,
    SigningKey:           signingKey,
    NodeURL:              localValidatingNode,
    DatabasePath:         dataDirectory + "/entpay.db",
    FulfillmentDirectory: dataDirectory + "/fulfillments",
    Products:             []entpay.Product{productA, productB},
    PublicEndpoint:       "https://merchant.example/entpay/",
})
```

Serve `gateway.Handler()` behind the merchant's HTTPS reverse proxy. The SDK
provides the complete web workspace and these routes:

```text
GET  /healthz
GET  /v1/info
POST /v1/invoices
POST /v1/handoffs/redeem
GET  /v1/invoices/{id}
POST /v1/invoices/{id}/submit
POST /v1/invoices/{id}/claim
GET  /v1/invoices/{id}/artifact
```

When `PublicEndpoint` is configured, invoice creation also returns a short-lived
`launch` object. Its `url` contains only the public merchant endpoint and is safe
to pass to the operating-system protocol handler. The merchant page sends the
separate `launch.handoff` value to Entcoin's `127.0.0.1:47833` relay after the
desktop app starts. Do not concatenate the handoff into a system URI or log it.

Call `gateway.Close(ctx)` during graceful shutdown. Product fulfillment should
honor context cancellation. Mark non-retryable product failures with
`entpay.PermanentFailure(err)`; other failures receive a bounded retry.

## Safety and idempotency

- Invoice input is canonicalized and SHA-256 bound before signing.
- Requests reject unknown outer fields, duplicate keys, trailing data, more
  than 128 KiB, and nesting deeper than 32 levels.
- A transaction ID is unique across all invoices. Submission is idempotent.
- Delivery starts only after the configured confirmation count.
- SQLite serializes fulfillment jobs. Stale jobs can recover after a crash.
- A fulfillment payload and artifact are staged atomically before the signed
  delivery is committed, avoiding a second paid provider call after a crash.
- Claim tokens are 256-bit bearer capabilities; only their SHA-256 digests are
  stored. Unauthorized invoice and artifact lookups return `404`.
- Artifact bytes, size, media type, and SHA-256 are verified before download and
  again by the Agent before atomic storage. HTTP Range is supported.

## Private deployment boundary

The merchant service is not part of an Entcoin node deployment. It may live in
a separate private repository or only on the merchant's server. Even a private
repository must not contain:

- model API keys or private model base URLs;
- wallet private keys, recovery words, or wallet-control tokens;
- EntPay Ed25519 private signing keys or claim tokens;
- production node URLs, server addresses, inventory, or access credentials;
- SQLite databases, logs, prompts, provider responses, or generated artifacts.

Inject sensitive values at runtime with systemd credentials, a secret manager,
or root-owned restricted files. Avoid command-line secrets because process
arguments are observable. The merchant service needs only a receiving address,
an EntPay signing key, and access to a validating Entcoin node; it never needs a
receiving-wallet private key.

## Protocol limitations

- Confirmation depth is merchant policy. Low depth can deliver before a chain
  reorganization; high-value products should require more confirmations.
- The current verifier reads bounded merchant wallet history and targets modest
  commerce volume, not high-frequency settlement.
- ENT is not a stable unit of account. EntPay does not provide escrow, refunds,
  subscriptions, payment channels, bridges, custody, or legal dispute handling.
- Receipt integrity proves what a merchant delivered for a payment. It does not
  prove that an external model, data source, or business claim was correct.
