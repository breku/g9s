#!/usr/bin/env bash
# release.sh — fetch latest tag, bump patch, create and push new tag.
#
# Usage: ./relese.sh [patch|minor|major]
#   Default: patch
#
# Requires: git, curl (for GitHub API fallback), jq (optional — used when
# parsing the GitHub API response; falls back to grep/sed if absent).

set -euo pipefail

BUMP="${1:-patch}"

# ---------------------------------------------------------------------------
# 1. Determine the latest version tag.
#    Try `git describe` first (works offline, reflects local clone).
#    Fall back to the GitHub Releases API so a shallow clone or a repo with
#    no local tags still produces the right baseline.
# ---------------------------------------------------------------------------

get_latest_tag_git() {
  # List all tags that look like vX.Y.Z, sort by version, take the highest.
  git tag --list 'v[0-9]*.[0-9]*.[0-9]*' \
    | sort -V \
    | tail -n1
}

get_latest_tag_github() {
  local repo
  # Extract "owner/repo" from the remote URL (supports both https and ssh).
  repo=$(git remote get-url origin 2>/dev/null \
    | sed -E 's|.*github\.com[:/]||;s|\.git$||')

  if [[ -z "$repo" ]]; then
    return 1
  fi

  local api_url="https://api.github.com/repos/${repo}/releases/latest"
  local response
  response=$(curl -fsSL \
    -H "Accept: application/vnd.github+json" \
    "$api_url" 2>/dev/null) || return 1

  # Parse tag_name with jq if available, otherwise grep+sed.
  if command -v jq &>/dev/null; then
    echo "$response" | jq -r '.tag_name // empty'
  else
    echo "$response" \
      | grep -o '"tag_name":"[^"]*"' \
      | sed 's/"tag_name":"//;s/"//'
  fi
}

echo "Fetching latest tag…"

LATEST=$(get_latest_tag_git)

if [[ -z "$LATEST" ]]; then
  echo "No local tags found — trying GitHub API…"
  LATEST=$(get_latest_tag_github) || true
fi

if [[ -z "$LATEST" ]]; then
  echo "No existing tags found. Starting from v0.0.0."
  LATEST="v0.0.0"
fi

echo "Latest tag: ${LATEST}"

# ---------------------------------------------------------------------------
# 2. Parse semver components (strip leading 'v').
# ---------------------------------------------------------------------------

VERSION="${LATEST#v}"
IFS='.' read -r MAJOR MINOR PATCH <<< "$VERSION"

MAJOR="${MAJOR:-0}"
MINOR="${MINOR:-0}"
PATCH="${PATCH:-0}"

# ---------------------------------------------------------------------------
# 3. Increment the requested segment.
# ---------------------------------------------------------------------------

case "$BUMP" in
  major)
    MAJOR=$((MAJOR + 1))
    MINOR=0
    PATCH=0
    ;;
  minor)
    MINOR=$((MINOR + 1))
    PATCH=0
    ;;
  patch)
    PATCH=$((PATCH + 1))
    ;;
  *)
    echo "Unknown bump type '${BUMP}'. Use patch, minor, or major." >&2
    exit 1
    ;;
esac

NEW_TAG="v${MAJOR}.${MINOR}.${PATCH}"
echo "New tag:    ${NEW_TAG}"

# ---------------------------------------------------------------------------
# 4. Create and push the tag.
# ---------------------------------------------------------------------------

git tag "$NEW_TAG"
echo "Created tag ${NEW_TAG}."

git push origin "$NEW_TAG"
echo "Pushed tag ${NEW_TAG} to origin."
