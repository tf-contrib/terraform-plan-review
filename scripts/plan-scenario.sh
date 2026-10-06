#!/usr/bin/env bash
# Usage: plan-scenario.sh SCENARIO OUT.json
# Applies testdata/scenarios/SCENARIO/v1, then writes the JSON plan of v2.
set -euo pipefail
cd "$(dirname "$0")/.."
scenario=testdata/scenarios/$1
out=$2
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cp -R "$scenario/v1/." "$work/"
(cd "$work" && tofu init -input=false >/dev/null && tofu apply -auto-approve -input=false >/dev/null)
find "$work" \( -name '*.tf' -o -name '*.tofu' \) -delete
cp -R "$scenario/v2/." "$work/"
(cd "$work" && tofu plan -input=false -out=tfplan >/dev/null && tofu show -json tfplan) > "$out"
