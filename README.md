# sbx — will it relay?

Checks a Bitcoin transaction, a package, or a fee bump against **Core 28.4, 29.4, 30.3, 31.1 and Knots 29.4.2 at once**. It shows which versions accept it, which reject it, and why in plain language. Everything runs locally on a private regtest network, and nothing is broadcast.

It's a single Go binary that uses only Go's standard library. It runs the official release binaries directly, so there's no Docker.

## Install

From a release (macOS or Linux, Intel or ARM):

```bash
curl -fsSL https://raw.githubusercontent.com/thetimileyin/sbx/main/scripts/install.sh | sh
```

It checks the archive against the release's `SHA256SUMS` before installing to `~/.local/bin`, and uses the GitHub CLI instead of plain downloads when you're logged in.

From source (Go 1.24+):

```bash
go install github.com/thetimileyin/sbx/cmd/sbx@latest
```

## Quick start

```bash
sbx fetch          # official releases; each SHA256SUMS must be signed by 3+ known builders
sbx up             # 5 matrix nodes + 1 builder node on regtest
sbx serve          # http://127.0.0.1:8338
```

`sbx fetch` needs `gpgv` (macOS: `brew install gnupg`; Debian/Ubuntu: `apt install gpgv`). The builder public keys are bundled in `internal/release/keys/`; see the README there for where they come from.

Data lives in `$SBX_HOME` (default `~/.sbx`).

## Commands

| Command | What it does |
|---|---|
| `sbx fetch` | Downloads each release after verifying builder signatures on its `SHA256SUMS` (3+ required) and its SHA-256, retrying corrupted downloads. `--skip-signatures` opts out |
| `sbx up` / `down` / `status` | Starts, stops or lists the nodes. `up` funds the sandbox wallet (140 blocks) |
| `sbx check HEX` | Checks one transaction |
| `sbx check PARENT CHILD` | Checks a package: parents first, child last |
| `sbx check --mempool FILE HEX` | Loads FILE's transactions into the mempool first, for fee bumps, conflicts and chain limits |
| `sbx check --file FILE` / `--file -` | Reads transactions from a file or from stdin |
| `sbx check --json …` | Prints the result as JSON, the same shape the web page uses |
| `sbx demo [basic\|package\|mempool]` | Runs the 21 built-in examples |
| `sbx serve [--addr 127.0.0.1:8338]` | Local web page and JSON API |

`check` exit codes: **0** every version accepts, **3** at least one rejects, **1** error, **2** usage. That lets a CI step fail when a wallet builds a transaction that some versions won't relay.

## How a check works

1. **Decode** each transaction and work out its fee from inputs found in the request or in the sandbox's coins.
2. **Start one throwaway probe node per version, in parallel.** Each probe gets its own temporary data folder and a free port. It syncs the chain from the builder, then its networking is switched off.
3. **Load the mempool setup transactions**, if given, in order. If a version refuses one of them, the result says so, because that version isn't seeing the same mempool.
4. **Submit** with `sendrawtransaction` for a single transaction, or `submitpackage` for a package. `testmempoolaccept` isn't used, because the spike showed it can't evaluate package fee rates.
5. **Explain.** The tool separates root causes from knock-on rejections (a child failing only because its parent did). It translates each reject reason into plain language, and names the known policy difference when the versions split.
6. **Clean up.** Each probe is stopped and its data deleted. The persistent nodes never change, so checks can't affect each other. A check takes about 0.7 seconds.

**Only sandbox coins can be checked.** A real mainnet transaction spends coins that don't exist on regtest. Checking those needs pruned mainnet nodes, which is phase 3 in the outline.

## GitHub Action

Add relay checks to CI. The action installs a verified sbx release, starts every node
version on regtest (cached between runs) and exposes the sandbox wallet's RPC so your
code can build transactions. With `check`, the step fails if any version rejects them.

```yaml
- uses: thetimileyin/sbx@v0
  id: sbx

# Build transactions with your own code against the sandbox wallet:
#   ${{ steps.sbx.outputs.rpc-url }}  user: sbx  password: ${{ steps.sbx.outputs.rpc-password }}
# or with the bundled helper:
- run: |
    addr=$(sbx-sandbox-cli getnewaddress)
    sbx-sandbox-cli -named send outputs="{\"$addr\": 0.001}" fee_rate=1 \
      options='{"add_to_wallet": false}' | jq -r .hex > txs.txt

- run: sbx check --file txs.txt   # exits 3 if any version rejects
```

Inputs: `version` (default `latest`), `check` (file of transaction hex), `mempool`
(file of setup transactions), `skip-signatures`. Outputs: `version`, `rpc-url`,
`rpc-user`, `rpc-password`. Linux and macOS runners.

## Web page and API

The page is served from the binary itself, with no CDN and no external requests.

| Endpoint | Purpose |
|---|---|
| `GET /api/status` | Node versions, whether each is running, block height |
| `POST /api/check` | `{"txs": [...], "mempool": [...]}` returns the result JSON |
| `GET /api/examples` | The built-in examples |
| `POST /api/examples/{id}` | Builds a fresh copy of an example |

The server is limited to this machine:

- It only listens on loopback addresses.
- It rejects requests whose `Host` isn't `127.0.0.1`, `localhost` or `[::1]`, which blocks DNS rebinding.
- It requires `application/json` on POST requests.
- It sends a strict Content-Security-Policy.
- There are no accounts, no cookies and no logs of transactions.

## Tests

```bash
go test ./...                                                       # unit tests
SBX_INTEGRATION=1 SBX_HOME=../.sbx go test ./internal/engine -run Golden -v
```

The **golden test set** (`internal/engine/engine_test.go`) records each version's verdict for all 21 examples, as measured by the spike on 2026-10-15. Every case passes. When a new release is added, this is where behaviour changes show up first.

## How this differs from the outline

- **Native release binaries instead of Docker.** Docker isn't needed: macOS and Linux release tarballs run directly, and each probe gets an isolated data folder. Containers may still be worth adding later for the Lightning, Ark and ecash layers.
- **No daemon.** The CLI and web server call the same engine directly. Node state lives in the data folder, so the two stay consistent.

## Not done yet

- An explanation dictionary generated from each release's source. It's hand-written for now.
- An MCP server.
- A full-mempool fee floor, and packages with mempool setup packages.
- Core 32.0, once it leaves release candidate.

## Keeping up with new node releases

`.github/workflows/watch.yml` runs daily. `cmd/sbx-watch` looks for new **final** Core and Knots releases (release candidates are ignored), verifies their builder signatures, and updates `internal/release/matrix.json`: patch releases replace their line's entry, new Core majors are added, Knots keeps its newest release. The workflow then runs every example against the new matrix and opens a pull request with a table of results. Patch releases are held to their line's recorded behaviour in `internal/engine/testdata/golden.json`; a new major line is reported against the previous one.

Try it locally: `go run ./cmd/sbx-watch` (or `--from internal/release/testdata/older-matrix.json` to see what an update looks like).

## Releases

`scripts/release.sh v0.1.0` builds `dist/sbx_<os>_<arch>.tar.gz` for darwin/linux × arm64/amd64 plus `SHA256SUMS`. Pushing a `v*` tag runs the same script in GitHub Actions and publishes a release. Every push runs unit tests on Linux and macOS, plus a Linux integration job that fetches all releases (with signature checks), starts the network and runs the golden corpus.

## License

MIT. See [LICENSE](LICENSE).
