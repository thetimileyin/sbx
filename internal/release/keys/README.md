# Bundled release-signing keys

Public keys used by `sbx fetch` to verify the signatures on each release's `SHA256SUMS`
(via `gpgv`, no keyring database or daemon). A release is trusted only when at least
three of these keys produced a valid signature.

| File | Source | Keys |
|---|---|---|
| `core.gpg` | https://github.com/bitcoin-core/guix.sigs/tree/main/builder-keys | 40 |
| `knots.gpg` | https://github.com/bitcoinknots/guix.sigs/tree/knots/builder-keys | 20 |

Fetched on 2026-10-17 and converted from ASCII armour to a binary keyring with
`gpg --dearmor`. Refresh them when the upstream builder lists change.
