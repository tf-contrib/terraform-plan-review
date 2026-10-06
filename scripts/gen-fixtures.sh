#!/usr/bin/env bash
# Regenerates testdata/plans/<scenario>.json for every directory in
# testdata/scenarios. Scenarios use only the built-in terraform_data
# resource, so no providers are downloaded.
set -euo pipefail
cd "$(dirname "$0")/.."
for scenario in testdata/scenarios/*/; do
  name=$(basename "$scenario")
  ./scripts/plan-scenario.sh "$name" "testdata/plans/$name.json"
  echo "testdata/plans/$name.json"
done
