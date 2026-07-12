#!/usr/bin/env bash
# Build a deterministic local release candidate from a clean Git commit. The
# script never pushes, uploads, rewrites tags, or modifies protocol source files.
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <tag, for example v0.2.0-draft.1>" >&2
  exit 2
fi

TAG="$1"
if [[ ! "$TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][A-Za-z0-9.-]+)?$ ]]; then
  echo "invalid semantic release tag: $TAG" >&2
  exit 2
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

if [[ -n "$(git status --porcelain)" ]]; then
  echo "working tree is not clean; commit or stash changes before release" >&2
  exit 1
fi
if git rev-parse "$TAG" >/dev/null 2>&1; then
  echo "tag already exists: $TAG" >&2
  exit 1
fi

make check

DIST="$ROOT/dist/$TAG"
rm -rf "$DIST"
mkdir -p "$DIST"

# Source archive is generated from Git, so ignored build output and uncommitted
# local files can never leak into a release.
git archive --format=tar.gz --prefix="agent-service-contract-protocol-$TAG/" \
  -o "$DIST/agent-service-contract-protocol-$TAG.tar.gz" HEAD
git archive --format=zip --prefix="agent-service-contract-protocol-$TAG/" \
  -o "$DIST/agent-service-contract-protocol-$TAG.zip" HEAD

(
  cd "$DIST"
  sha256sum ./* > SHA256SUMS
)

git tag -a "$TAG" -m "ASCP $TAG"
echo "created local release tag $TAG and assets under $DIST"
echo "review with: git show $TAG"
