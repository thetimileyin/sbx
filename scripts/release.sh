#!/usr/bin/env bash
# Build release archives for every supported platform into dist/, plus SHA256SUMS.
#
#   scripts/release.sh v0.1.0
#
# Archives are named sbx_<os>_<arch>.tar.gz (no version) so the install script can use
# GitHub's /releases/latest/download/ URLs.
set -euo pipefail
version="${1:?usage: scripts/release.sh <version, e.g. v0.1.0>}"
cd "$(dirname "$0")/.."

rm -rf dist
mkdir -p dist
for target in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64; do
  os="${target%/*}"
  arch="${target#*/}"
  name="sbx_${os}_${arch}"
  stage="dist/$name"
  mkdir -p "$stage"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
    -ldflags "-s -w -X main.version=${version}" -o "$stage/sbx" ./cmd/sbx
  cp README.md LICENSE "$stage/"
  cp scripts/sandbox-cli "$stage/"
  tar -C dist -czf "dist/$name.tar.gz" "$name"
  rm -rf "$stage"
  echo "built dist/$name.tar.gz"
done

cd dist
if command -v sha256sum >/dev/null; then
  sha256sum ./*.tar.gz | sed 's#\./##' > SHA256SUMS
else
  shasum -a 256 ./*.tar.gz | sed 's#\./##' > SHA256SUMS
fi
cat SHA256SUMS
