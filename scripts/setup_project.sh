#!/usr/bin/env bash
set -euo pipefail

ORG="mohsanabbas"
NUM="2"  # from https://github.com/users/mohsanabbas/projects/2

# Create fields (idempotent)
gh project field-create "$NUM" --owner "$ORG" --name "Status"   --data-type SINGLE_SELECT \
  --single-select-options "Todo" --single-select-options "In Progress" --single-select-options "Blocked" --single-select-options "Done" || true

gh project field-create "$NUM" --owner "$ORG" --name "Priority" --data-type SINGLE_SELECT \
  --single-select-options "P0" --single-select-options "P1" --single-select-options "P2" --single-select-options "P3" || true

gh project field-create "$NUM" --owner "$ORG" --name "Epic" --data-type TEXT || true

# Optional
gh project field-create "$NUM" --owner "$ORG" --name "Area" --data-type SINGLE_SELECT \
  --single-select-options "control-plane" --single-select-options "runner" --single-select-options "transform" \
  --single-select-options "source" --single-select-options "sink" --single-select-options "telemetry" --single-select-options "docs" || true

gh project field-create "$NUM" --owner "$ORG" --name "Estimate" --data-type NUMBER || true
gh project field-create "$NUM" --owner "$ORG" --name "Target Release" --data-type TEXT || true
