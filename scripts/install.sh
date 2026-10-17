#!/bin/sh
# Install sbx from GitHub releases.
#
#   sh install.sh                     # latest release into ~/.local/bin
#   SBX_PREFIX=/usr/local/bin sh install.sh
#
# Uses the GitHub CLI when available (needed while the repository is private),
# otherwise plain HTTPS downloads. The archive is checked against the release's
# SHA256SUMS before anything is installed.
set -eu

REPO="${SBX_REPO:-thetimileyin/sbx}"
PREFIX="${SBX_PREFIX:-$HOME/.local/bin}"

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) echo "sbx: unsupported OS $(uname -s)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) echo "sbx: unsupported CPU $(uname -m)" >&2; exit 1 ;;
esac
asset="sbx_${os}_${arch}.tar.gz"

tmp="$(mktemp -d)"
cleanup() { rm -rf -- "$tmp"; }
trap cleanup EXIT

echo "Downloading $asset from $REPO"
if command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
  gh release download --repo "$REPO" --pattern "$asset" --pattern SHA256SUMS --dir "$tmp"
else
  base="https://github.com/$REPO/releases/latest/download"
  curl -fsSL -o "$tmp/$asset" "$base/$asset"
  curl -fsSL -o "$tmp/SHA256SUMS" "$base/SHA256SUMS"
fi

expected="$(grep " $asset\$" "$tmp/SHA256SUMS" | cut -d' ' -f1)"
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$tmp/$asset" | cut -d' ' -f1)"
else
  actual="$(shasum -a 256 "$tmp/$asset" | cut -d' ' -f1)"
fi
if [ -z "$expected" ] || [ "$expected" != "$actual" ]; then
  echo "sbx: checksum mismatch for $asset; nothing installed" >&2
  exit 1
fi

tar -xzf "$tmp/$asset" -C "$tmp"
mkdir -p "$PREFIX"
install -m 0755 "$tmp/sbx_${os}_${arch}/sbx" "$PREFIX/sbx"
install -m 0755 "$tmp/sbx_${os}_${arch}/sandbox-cli" "$PREFIX/sbx-sandbox-cli"

echo "Installed $("$PREFIX/sbx" version) to $PREFIX/sbx"
case ":$PATH:" in
  *":$PREFIX:"*) ;;
  *) echo "Add $PREFIX to your PATH to run sbx from anywhere." ;;
esac
cat <<EOF

Next:
  sbx fetch    # downloads Bitcoin Core 28-31 and Knots, verifying builder signatures (needs gpgv)
  sbx up       # starts the private regtest network
  sbx serve    # opens http://127.0.0.1:8338
EOF
