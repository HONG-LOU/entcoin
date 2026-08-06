# EntPay desktop security contract

This document freezes the EntPay v1 desktop trust boundaries and transport vectors. It complements the protocol description in `entpay.md`; it does not expand what a merchant is trusted to do.

## Trust boundaries

- The merchant controls its HTTPS endpoint, product metadata, signed Invoice, Delivery payload, and artifact bytes. It never receives wallet keys and cannot authorize a payment.
- The `entcoin://` URL is untrusted bootstrap data. It contains only a canonical merchant endpoint. The handoff code remains in the merchant HTTPS response and is sent to the desktop loopback relay in an HTTP body after launch; no capability enters process arguments.
- The desktop backend is the authorization boundary. The WebView can request an action only by session ID and expected revision. It cannot provide a transaction, destination, amount, fee, claim token, or artifact path.
- The existing `node.Service` is the only wallet and ledger owner. Preview, prepare, wallet mutation, and commit share its mutation boundary and require the reviewed wallet address.
- The client SQLite database is untrusted persistent bytes. Capability data is authenticated encryption bound to network, schema, and session ID. A newer schema disables Agent Pay without preventing node startup.
- A valid merchant signature proves continuity with the signing key returned by that endpoint. It does not prove a legal identity or reputation. First use is labelled, and any merchant address or key change blocks payment until a separate explicit trust action.

## Required invariants

1. No transaction is prepared before a revision-matched approval of the exact displayed amount.
2. A prepared transaction is journaled before broadcast. Recovery commits that same transaction and never creates a replacement implicitly.
3. Every post-broadcast failure says that payment was sent. Delivery failure never changes a session to complete.
4. Handoff redemption binds the first client nonce. Same-nonce retry is bounded and idempotent; another nonce receives the same not-found response as an unknown or expired code.
5. Handoff code and claim token are erased after completion, rejection, expiry, or a pre-payment terminal invalid result. They never enter logs or Wails DTOs.
6. Artifacts are streamed to a same-directory temporary file, bounded, hashed, media-checked, synced, and installed without overwrite. Merchant names never select a directory.
7. System URI registration passes only a secret-free bootstrap URL as one quoted argument. Parsing is complete before networking and rejects capabilities, extra fields, fragments, userinfo, paths, noncanonical encoding, and unsafe public ports.
8. The loopback relay accepts a handoff only during a short merchant-specific bootstrap window and only when the browser Origin exactly matches that merchant endpoint. Acceptance consumes the window.

## Canonical launch vectors

The system vectors contain no token. A full handoff URL is accepted only inside the already-running desktop paste control and is rejected when supplied as a process argument.

| Vector | Expected result |
|---|---|
| `entcoin://pay?v=1&merchant=https%3A%2F%2Fmerchant.example%2Fentpay%2F` | Accept as system bootstrap |
| `entcoin://pay?v=1&merchant=http%3A%2F%2F127.0.0.1%3A47832%2F` | Accept as development bootstrap |
| `entcoin://pay?v=1&merchant=http%3A%2F%2Fmerchant.example%2F` | Reject non-loopback HTTP |
| any system URI containing `handoff` | Reject before Wails startup |
| `entcoin://pay/extra?...` | Reject path |

The merchant response CSP keeps scripts same-origin and extends `connect-src`
only to `http://127.0.0.1:47833`, the fixed desktop relay origin. The relay still
requires the exact bootstrapped merchant `Origin`; CSP permission alone does not
authorize a handoff.
| `entcoin://user@pay?...` | Reject userinfo |
| any fragment, duplicate `v`, duplicate `merchant`, duplicate `handoff`, or unknown parameter | Reject |
| padded, short, overlong, or noncanonical base64url handoff | Reject |
| public HTTPS endpoint with a non-default port | Reject |
| endpoint containing control characters, query, fragment, or credentials | Reject |

## Threats and mitigations

| Threat | Control |
|---|---|
| Web page launches a crafted URI | Secret-free strict bootstrap parser, merchant-bound relay window, independent `/v1/info` and Invoice revalidation |
| Malicious site posts to loopback | Exact pending merchant Origin, no wildcard CORS, strict body, one-use authorization, bounded queue, no payment without approval |
| Another process steals a handoff | 256-bit code, digest-only merchant storage, short expiry, nonce binding, rate limits, generic 404 |
| Renderer is compromised | Structured revision APIs; no secret DTOs; backend reconstructs and validates all payment terms |
| Double click, concurrent approval, or restart | Revision CAS, per-session operation lock, unique transaction ID, journal-before-commit, idempotent ledger commit |
| Merchant changes key or address | Endpoint continuity record and explicit re-trust before a separate payment approval |
| Malicious artifact | Verified Receipt metadata, strict filename, no symlink path, size/hash/media limits, no overwrite, CSP blocks frames and executable inline content |
| Database copied or modified | OS-bound key protection plus AEAD; capsule AAD binds network/schema/session |
| Secret disclosure through diagnostics | Fixed public DTOs and fixed launch errors; no raw URL, input, handoff, claim token, or capsule in events/errors/logs |
| Installer hijacks another URI owner | Per-user registration; uninstall must remove the association only when its command still points to that installation |

## Verification evidence

Automated evidence lives in `entpay/handoff_test.go`, `entpay/client_secure_test.go`, `entpay/client_store_test.go`, `entpay/client_manager_test.go`, `entpay/delivery_preview_test.go`, and `internal/node/payment_test.go`. Release evidence must additionally include Windows and Ubuntu install/uninstall URI tests, Wails screenshots at both supported sizes and languages, a restart-after-broadcast exercise, dependency scans, signed artifacts, checksums, and a real low-value mainnet purchase. Those external release checks cannot be replaced by unit tests.
