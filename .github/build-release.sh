#!/bin/bash
# Build the release tarballs into dist/.
#
# Kept out of the workflow so it can be run locally before tagging:
#
#   GITHUB_REF_NAME=v0.1.0 ./.github/build-release.sh
#
# Two Macs, one binary each. No Linux build: this rig is launchd, tmux and the
# login keychain, and a Linux tarball would be a download that cannot work.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

VERSION="${GITHUB_REF_NAME:-}"
[[ -n "$VERSION" ]] || { echo "build-release: set GITHUB_REF_NAME to the tag (v0.1.0)" >&2; exit 1; }
VERSION="${VERSION#v}"

rm -rf dist && mkdir -p dist

for arch in arm64 amd64; do
    CGO_ENABLED=0 GOOS=darwin GOARCH="$arch" \
        go build -trimpath -ldflags "-s -w" -o dist/factory ./cmd/factory
    tar -czf "dist/factory_${VERSION}_darwin_${arch}.tar.gz" -C dist factory
    rm dist/factory
done

# `shasum` on macOS, `sha256sum` on the Linux runner. Same output shape.
if command -v sha256sum >/dev/null; then
    (cd dist && sha256sum ./*.tar.gz > checksums.txt)
else
    (cd dist && shasum -a 256 ./*.tar.gz > checksums.txt)
fi

cat dist/checksums.txt
