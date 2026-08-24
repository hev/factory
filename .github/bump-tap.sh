#!/bin/bash
# Rewrite Formula/factory.rb in the tap to point at the release just published.
#
#   GITHUB_REF_NAME=v0.1.0 ./.github/bump-tap.sh --dry-run   # render, push nothing
#
# The formula is written through the GitHub Contents API rather than by cloning
# and pushing, so the token is never embedded in a git remote and never reaches
# a log line. HOMEBREW_TAP_TOKEN needs exactly one permission on exactly one
# repo: Contents, read and write, on hev/homebrew-tap.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TAP="${FACTORY_TAP_REPO:-hev/homebrew-tap}"
FORMULA_PATH="Formula/factory.rb"
DRY_RUN=""
[[ "${1:-}" == "--dry-run" ]] && DRY_RUN=1

VERSION="${GITHUB_REF_NAME:-}"
[[ -n "$VERSION" ]] || { echo "bump-tap: set GITHUB_REF_NAME to the tag (v0.1.0)" >&2; exit 1; }
VERSION="${VERSION#v}"

CHECKSUMS="$ROOT_DIR/dist/checksums.txt"
[[ -f "$CHECKSUMS" ]] || { echo "bump-tap: no $CHECKSUMS — run build-release.sh first" >&2; exit 1; }

# Pull each digest out by the filename it belongs to. Reading them positionally
# would silently swap the two architectures the day the build loop reorders,
# and the symptom is a formula that installs an Intel binary on Apple silicon.
sha_for() {
    local arch="$1" sha
    sha="$(awk -v want="factory_${VERSION}_darwin_${arch}.tar.gz" \
        '{ n = $2; sub(/^\.\//, "", n); if (n == want) print $1 }' "$CHECKSUMS")"
    [[ -n "$sha" ]] || { echo "bump-tap: no checksum for darwin_${arch} in $CHECKSUMS" >&2; exit 1; }
    printf '%s' "$sha"
}

RENDERED="$(sed \
    -e "s/@@VERSION@@/${VERSION}/g" \
    -e "s/@@SHA_ARM64@@/$(sha_for arm64)/g" \
    -e "s/@@SHA_AMD64@@/$(sha_for amd64)/g" \
    "$ROOT_DIR/.github/factory.rb.tmpl")"

# A placeholder that survived the substitution means the template grew a field
# this script does not fill, and pushing it would break `brew install` for
# everyone until the next release.
if grep -q '@@' <<<"$RENDERED"; then
    echo "bump-tap: unsubstituted placeholder in the rendered formula:" >&2
    grep -n '@@' <<<"$RENDERED" >&2
    exit 1
fi

if [[ -n "$DRY_RUN" ]]; then
    printf '%s\n' "$RENDERED"
    echo "bump-tap: dry run — $TAP not touched" >&2
    exit 0
fi

# The Contents API updates a file in place, but only if it is told which blob
# it is replacing. Absent means the tap has no formula yet, which is the first
# release and not an error.
EXISTING="$(gh api "repos/$TAP/contents/$FORMULA_PATH" --jq .sha 2>/dev/null || true)"

args=(--method PUT "repos/$TAP/contents/$FORMULA_PATH"
      -f "message=factory ${VERSION}"
      -f "content=$(printf '%s\n' "$RENDERED" | base64 | tr -d '\n')")
[[ -n "$EXISTING" ]] && args+=(-f "sha=$EXISTING")

gh api "${args[@]}" --jq '.commit.html_url'
echo "bump-tap: $TAP now serves factory ${VERSION}" >&2
