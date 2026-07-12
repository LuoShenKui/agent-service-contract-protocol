#!/usr/bin/env bash
# Publish the current repository to the canonical GitHub repository through the
# official GitHub CLI. Existing non-canonical remotes are never overwritten
# silently; set ASCP_REPLACE_ORIGIN=1 only when the operator has reviewed them.
set -euo pipefail

OWNER="${1:-LuoShenKui}"
REPOSITORY="${2:-agent-service-contract-protocol}"
VISIBILITY="${3:-public}"
TARGET_REPOSITORY="$OWNER/$REPOSITORY"
TARGET_REMOTE="https://github.com/$TARGET_REPOSITORY.git"

if ! command -v gh >/dev/null 2>&1; then
  echo "GitHub CLI (gh) is required: https://cli.github.com/" >&2
  exit 1
fi
if ! gh auth status >/dev/null 2>&1; then
  echo "authenticate first with: gh auth login" >&2
  exit 1
fi
if [[ "$VISIBILITY" != "public" && "$VISIBILITY" != "private" ]]; then
  echo "visibility must be public or private" >&2
  exit 2
fi
if [[ -n "$(git status --porcelain)" ]]; then
  echo "working tree is not clean; commit or stash changes before publishing" >&2
  exit 1
fi

# Refuse to publish a repository that has not passed the same gate as a release
# candidate. The script intentionally runs the gate before touching the remote.
make check

VISIBILITY_FLAG="--$VISIBILITY"
if gh repo view "$TARGET_REPOSITORY" >/dev/null 2>&1; then
  : # The user or organization already created the destination repository.
else
  gh repo create "$TARGET_REPOSITORY" \
    "$VISIBILITY_FLAG" \
    --description "Compact direct calls and signed contracts for platform-owned agents"
fi

if git remote get-url origin >/dev/null 2>&1; then
  CURRENT_ORIGIN="$(git remote get-url origin)"
  case "$CURRENT_ORIGIN" in
    "$TARGET_REMOTE"|"git@github.com:$TARGET_REPOSITORY.git"|"https://github.com/$TARGET_REPOSITORY")
      ;;
    *)
      if [[ "${ASCP_REPLACE_ORIGIN:-0}" != "1" ]]; then
        echo "origin points to '$CURRENT_ORIGIN', not '$TARGET_REMOTE'." >&2
        echo "review the remote, then rerun with ASCP_REPLACE_ORIGIN=1 to replace it." >&2
        exit 1
      fi
      git remote set-url origin "$TARGET_REMOTE"
      ;;
  esac
else
  git remote add origin "$TARGET_REMOTE"
fi

git push -u origin "$(git branch --show-current)"

# Release tags use a separate ref namespace. Push them explicitly so GitHub
# Actions can create releases and downstream users can verify immutable drafts.
git push origin --tags

printf "published https://github.com/%s/%s with all local tags\n" "$OWNER" "$REPOSITORY"
