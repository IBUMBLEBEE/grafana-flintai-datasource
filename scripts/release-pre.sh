#!/usr/bin/env bash
# Prepare release files, then create the annotated tag after those files are committed.
# Usage:
#   ./scripts/release-pre.sh <X.Y.Z>
#   ./scripts/release-pre.sh <X.Y.Z> --force
#   ./scripts/release-pre.sh <X.Y.Z> --tag

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

usage() {
  cat <<EOF
Usage:
  $0 <version>         Update package versions and regenerate CHANGELOG.md
  $0 <version> --force Regenerate an existing version's CHANGELOG.md section
  $0 <version> --tag   Create the local annotated tag after committing the release files

Examples:
  $0 0.2.0
  git add package.json package-lock.json CHANGELOG.md
  git commit -m 'chore(release): v0.2.0'
  $0 0.2.0 --tag

The version may be written as 0.2.0 or v0.2.0. This script never commits or pushes.
EOF
}

die() {
  echo "Error: $*" >&2
  exit 1
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

[[ -n "${1:-}" ]] || {
  usage
  exit 1
}

VERSION="${1#v}"
MODE="${2:-prepare}"

[[ "$VERSION" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || \
  die "version must be stable SemVer, for example 0.2.0"

if [[ "$MODE" != "prepare" && "$MODE" != "--force" && "$MODE" != "--tag" ]]; then
  usage
  die "unknown option: $MODE"
fi

if [[ -n "${3:-}" ]]; then
  usage
  die "too many arguments"
fi

command -v git >/dev/null 2>&1 || die "git is required"
command -v node >/dev/null 2>&1 || die "Node.js is required"

cd "$REPO_ROOT"
git rev-parse --is-inside-work-tree >/dev/null 2>&1 || die "not inside a Git repository"

TAG="v${VERSION}"

if [[ "$MODE" == "--tag" ]]; then
  PACKAGE_VERSION="$(node -p "require('./package.json').version")"
  [[ "$PACKAGE_VERSION" == "$VERSION" ]] || \
    die "package.json is ${PACKAGE_VERSION}; prepare and commit ${VERSION} first"

  LOCK_VERSION="$(node -p "require('./package-lock.json').version")"
  [[ "$LOCK_VERSION" == "$VERSION" ]] || \
    die "package-lock.json is ${LOCK_VERSION}; prepare and commit ${VERSION} first"

  grep -Eq "^## ${VERSION}( |$)" CHANGELOG.md || \
    die "CHANGELOG.md has no ${VERSION} release section"

  [[ -z "$(git status --porcelain)" ]] || \
    die "the working tree must be clean before creating ${TAG}"

  if git rev-parse -q --verify "refs/tags/${TAG}" >/dev/null; then
    TAG_COMMIT="$(git rev-list -n 1 "$TAG")"
    HEAD_COMMIT="$(git rev-parse HEAD)"
    if [[ "$TAG_COMMIT" == "$HEAD_COMMIT" ]]; then
      echo "${TAG} already points to HEAD; nothing to do."
      exit 0
    fi
    die "tag ${TAG} already exists on another commit"
  fi

  git tag -a "$TAG" -m "Release ${TAG}"
  echo "Created local tag ${TAG} at $(git rev-parse --short HEAD)."
  echo
  echo "Review and publish it with:"
  echo "  git show ${TAG}"
  echo "  git push origin ${TAG}"
  echo
  echo "After pushing, monitor the gated release workflow with:"
  echo "  gh run list --workflow release.yml --branch ${TAG}"
  echo "  gh run watch"
  exit 0
fi

command -v npm >/dev/null 2>&1 || die "npm is required"
command -v git-cliff >/dev/null 2>&1 || {
  echo "Error: git-cliff is required" >&2
  echo "Install it with: cargo install git-cliff" >&2
  echo "             or: npm install -g @git-cliff/git-cliff" >&2
  exit 1
}

if git rev-parse -q --verify "refs/tags/${TAG}" >/dev/null; then
  die "tag ${TAG} already exists"
fi

CURRENT_VERSION="$(node -p "require('./package.json').version")"
CHANGELOG_VERSION="$(awk '/^## [0-9]+\.[0-9]+\.[0-9]+( |$)/ { print $2; exit }' CHANGELOG.md)"

if [[ "$MODE" != "--force" && "$CURRENT_VERSION" == "$VERSION" && "$CHANGELOG_VERSION" == "$VERSION" ]]; then
  echo "Release files are already prepared for ${TAG}; CHANGELOG.md was left unchanged."
  echo "Use --force only if you intentionally want to regenerate it from Git history."
  echo
  echo "After committing the release files, create the local tag with:"
  echo "  $0 ${VERSION} --tag"
  exit 0
fi

if [[ "$CURRENT_VERSION" == "$VERSION" ]]; then
  echo "package.json is already at ${VERSION}."
else
  echo "Updating version: ${CURRENT_VERSION} -> ${VERSION}"
  npm version --no-git-tag-version "$VERSION" >/dev/null
  echo "Updated package.json and package-lock.json."
fi

if [[ "$MODE" == "--force" ]]; then
  TEMP_CHANGELOG="$(mktemp)"
  trap 'rm -f "$TEMP_CHANGELOG"' EXIT
  awk -v version="$VERSION" '
    $0 == "## " version || index($0, "## " version " ") == 1 {
      skipping = 1
      next
    }
    skipping && /^## [0-9]+\.[0-9]+\.[0-9]+( |$)/ {
      skipping = 0
    }
    !skipping { print }
  ' CHANGELOG.md > "$TEMP_CHANGELOG"
  mv "$TEMP_CHANGELOG" CHANGELOG.md
  trap - EXIT
fi

echo "Generating CHANGELOG.md for ${TAG}..."
git cliff --unreleased --tag "$TAG" --prepend CHANGELOG.md
npx prettier --write CHANGELOG.md >/dev/null
echo "Updated CHANGELOG.md."

echo
echo "Release ${TAG} is prepared. Review and commit the generated files:"
echo "  git diff -- package.json package-lock.json CHANGELOG.md"
echo "  git add package.json package-lock.json CHANGELOG.md"
echo "  git commit -m 'chore(release): ${TAG}'"
echo "  $0 ${VERSION} --tag"
echo
echo "After reviewing the tag, push it to trigger the gated release workflow:"
echo "  git push origin ${TAG}"
