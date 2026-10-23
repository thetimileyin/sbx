#!/usr/bin/env bash
# Used by action.yml: install an sbx release into the runner and make sure gpgv exists.
# Reads SBX_VERSION ("latest" or a tag like v0.1.1). Writes to GITHUB_PATH, GITHUB_ENV
# and GITHUB_OUTPUT.
set -euo pipefail
repo="thetimileyin/sbx"

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) echo "::error::sbx supports Linux and macOS runners only"; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) echo "::error::unsupported CPU $(uname -m)"; exit 1 ;;
esac

version="${SBX_VERSION:-latest}"
if [ "$version" = "latest" ]; then
  version="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest")"
  version="${version##*/}"
fi
asset="sbx_${os}_${arch}.tar.gz"
base="https://github.com/$repo/releases/download/$version"

dir="$RUNNER_TEMP/sbx-install"
rm -rf "$dir"
mkdir -p "$dir"
curl -fsSL -o "$dir/$asset" "$base/$asset"
curl -fsSL -o "$dir/SHA256SUMS" "$base/SHA256SUMS"
expected="$(grep " $asset\$" "$dir/SHA256SUMS" | cut -d' ' -f1)"
if command -v sha256sum >/dev/null; then
  actual="$(sha256sum "$dir/$asset" | cut -d' ' -f1)"
else
  actual="$(shasum -a 256 "$dir/$asset" | cut -d' ' -f1)"
fi
if [ -z "$expected" ] || [ "$expected" != "$actual" ]; then
  echo "::error::checksum mismatch for $asset from $version"
  exit 1
fi

tar -xzf "$dir/$asset" -C "$dir"
bin="$RUNNER_TEMP/sbx-bin"
mkdir -p "$bin"
install -m 0755 "$dir/sbx_${os}_${arch}/sbx" "$bin/sbx"
install -m 0755 "$dir/sbx_${os}_${arch}/sandbox-cli" "$bin/sbx-sandbox-cli"
echo "$bin" >> "$GITHUB_PATH"
echo "SBX_HOME=$RUNNER_TEMP/sbx" >> "$GITHUB_ENV"
echo "version=$version" >> "$GITHUB_OUTPUT"

# sbx fetch verifies builder signatures with gpgv.
if ! command -v gpgv >/dev/null; then
  if [ "$os" = darwin ]; then
    brew install gnupg >/dev/null
  else
    sudo apt-get update -qq && sudo apt-get install -y -qq gpgv >/dev/null
  fi
fi
echo "Installed $("$bin/sbx" version)"
