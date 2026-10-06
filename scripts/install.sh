#!/usr/bin/env bash
# Installs tofu-plan-review for the action into $RUNNER_TEMP.
#
# When the action is used at a released version (`@vX.Y.Z`, or the commit
# that tag points to), the release binary is downloaded and verified
# against the release checksums. Otherwise (`@main`, a branch, a local
# `uses: ./`) there is no matching binary and the step outputs build=true,
# so the action builds from source.
#
# Inputs (environment): ACTION_REF, ACTION_REPO, GITHUB_ACTION_PATH,
# GITHUB_SERVER_URL, RUNNER_ARCH, RUNNER_OS, RUNNER_TEMP, GITHUB_OUTPUT.
set -euo pipefail

if [[ "$RUNNER_OS" != "Linux" ]]; then
  echo "::error::tofu-plan-review supports Linux runners only (this runner is $RUNNER_OS)"
  exit 1
fi

case "$RUNNER_ARCH" in
  X64) system=x86_64-linux ;;
  ARM64) system=aarch64-linux ;;
  *)
    echo "::error::unsupported runner architecture: $RUNNER_ARCH"
    exit 1
    ;;
esac

manifest="$GITHUB_ACTION_PATH/.github/config/release-please-manifest.json"
version=$(sed -n 's/.*"\.": *"\([^"]*\)".*/\1/p' "$manifest")
tag="v$version"

ref=${ACTION_REF:-}
released=false
if [[ -n "${ACTION_REPO:-}" && "$version" != "0.0.0" ]]; then
  if [[ "$ref" == "$tag" ]]; then
    released=true
  elif [[ "$ref" =~ ^[0-9a-f]{40}$ ]]; then
    # Pinned by SHA: use the release only if the tag points at this commit.
    # For annotated tags, the peeled ref (^{}) is the commit.
    refs=$(git ls-remote "$GITHUB_SERVER_URL/$ACTION_REPO" "refs/tags/$tag" "refs/tags/$tag^{}")
    tag_sha=$(awk '/\^\{\}$/ {print $1}' <<<"$refs")
    [[ -z "$tag_sha" ]] && tag_sha=$(awk 'NR == 1 {print $1}' <<<"$refs")
    [[ "$tag_sha" == "$ref" ]] && released=true
  fi
fi

if [[ "$released" != true ]]; then
  echo "No release binary for ref '${ref:-local}'; building from source."
  echo "build=true" >>"$GITHUB_OUTPUT"
  exit 0
fi

asset="tofu-plan-review-$system"
base="$GITHUB_SERVER_URL/$ACTION_REPO/releases/download/$tag"
dest="$RUNNER_TEMP/tofu-plan-review"
sums="$RUNNER_TEMP/tofu-plan-review-checksums.txt"

curl -fsSL --retry 3 -o "$dest" "$base/$asset"
curl -fsSL --retry 3 -o "$sums" "$base/checksums.txt"

expected=$(awk -v f="$asset" '$2 == f {print $1}' "$sums")
actual=$(sha256sum "$dest" | awk '{print $1}')
if [[ -z "$expected" || "$expected" != "$actual" ]]; then
  echo "::error::checksum mismatch for $asset $tag (expected '$expected', got '$actual')"
  exit 1
fi
chmod +x "$dest"
echo "Installed tofu-plan-review $tag ($system)."
echo "build=false" >>"$GITHUB_OUTPUT"
