#!/usr/bin/env bash
# Points action.yml at an image built from this checkout instead of the
# released image, so CI exercises the action as a Docker action with the
# code under test. Expects the x86_64 binary from the build job in dist/.
set -euo pipefail
cd "$(dirname "$0")/.."
install -m 0755 dist/terraform-plan-review-x86_64-linux dist/terraform-plan-review-amd64
sed -i 's|^  image: .*|  image: Dockerfile|' action.yml
grep -q '^  image: Dockerfile$' action.yml
