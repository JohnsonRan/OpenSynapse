#!/usr/bin/env bash
# The repository's checks. CI runs this same script (.github/workflows/ci.yml).
#
#     scripts/check.sh --fast   pre-commit: requirements, typecheck, go vet
#     scripts/check.sh          merge gate: also the checker's self-test, the page build and every Go test
#
# Exit code: 0 clean, otherwise the code of the first failing step (the requirements
# check exits 1 on a violation and 2 when it cannot run).
set -euo pipefail
cd "$(dirname "$0")/.."

# Python comes from uv, which picks or downloads an interpreter; Windows' own
# "python" is often only the Store stub.
py() { uv run --no-project python "$@"; }

py scripts/check-requirements.py
bun run typecheck
go vet ./...
[[ ${1-} == --fast ]] && exit 0

py scripts/test_check_requirements.py
bun run build:web # the app embeds the built page
go test ./...
