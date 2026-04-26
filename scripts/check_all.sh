#!/usr/bin/env bash
# scripts/check_all.sh — Local mirror of the GitHub Actions CI pipeline.
#
# Runs Go, Python SDK, JS SDK and OpenRPC checks in sequence so contributors
# can validate a change before pushing. Each section prints a header so a
# failure is easy to attribute. Set SKIP_<SECTION>=1 to skip individual
# sections (e.g. SKIP_JS=1 to skip the npm-based checks on a slow network).

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${REPO_ROOT}"

# --- helpers ---------------------------------------------------------------

if [ -t 1 ]; then
  BOLD="$(printf '\033[1m')"
  GREEN="$(printf '\033[32m')"
  YELLOW="$(printf '\033[33m')"
  RED="$(printf '\033[31m')"
  RESET="$(printf '\033[0m')"
else
  BOLD=""; GREEN=""; YELLOW=""; RED=""; RESET=""
fi

section() {
  printf '\n%s==> [%s]%s\n' "${BOLD}" "$1" "${RESET}"
}

ok() {
  printf '%s[OK]%s %s\n' "${GREEN}" "${RESET}" "$1"
}

warn() {
  printf '%s[WARN]%s %s\n' "${YELLOW}" "${RESET}" "$1"
}

skip_section() {
  local var="SKIP_$1"
  if [ "${!var:-0}" = "1" ]; then
    warn "skipping $1 (\$$var=1)"
    return 0
  fi
  return 1
}

# --- Go --------------------------------------------------------------------

if ! skip_section GO; then
  section "go"
  go version
  go mod download
  go vet ./...
  # gofmt: any diff is a failure.
  GOFMT_OUT="$(gofmt -l . || true)"
  if [ -n "${GOFMT_OUT}" ]; then
    printf '%sgofmt diff detected:%s\n%s\n' "${RED}" "${RESET}" "${GOFMT_OUT}"
    exit 1
  fi
  ok "gofmt clean"
  go test ./... -race -count=1
  go build -o "${REPO_ROOT}/agent" ./cmd/agent/
  ok "agent binary built at ./agent"
fi

# --- Python SDK ------------------------------------------------------------

if ! skip_section PYTHON; then
  section "python sdk"
  if command -v python3 >/dev/null 2>&1; then
    PY=python3
  else
    PY=python
  fi
  (
    cd sdk/python
    # Install (editable) only if the package is missing or test extras absent.
    if ! "${PY}" -c "import ai_agent" >/dev/null 2>&1; then
      "${PY}" -m pip install -e ".[test]"
    fi
    "${PY}" -m pytest -v --color=yes
  )
fi

# --- JS SDK ----------------------------------------------------------------

if ! skip_section JS; then
  section "js sdk"
  (
    cd sdk/js
    if [ ! -d node_modules ]; then
      npm install --no-audit --no-fund
    fi
    npm run lint
    npm run build
    npm test -- --reporter=verbose
  )
fi

# --- OpenRPC / JSON Schema -------------------------------------------------

if ! skip_section OPENRPC; then
  section "openrpc"
  python3 -m json.tool < docs/openrpc.json > /dev/null
  ok "docs/openrpc.json"
  shopt -s nullglob
  for f in docs/schemas/*.json; do
    python3 -m json.tool < "$f" > /dev/null
    ok "$f"
  done
  shopt -u nullglob
fi

# --- Markdown links (warning only) ----------------------------------------

if ! skip_section MD_LINKS; then
  section "markdown links (warning only)"
  if [ -x scripts/check_md_links.py ] || [ -f scripts/check_md_links.py ]; then
    python3 scripts/check_md_links.py || warn "broken markdown links detected"
  else
    warn "scripts/check_md_links.py not found, skipping"
  fi
fi

printf '\n%s%sAll checks passed.%s\n' "${GREEN}" "${BOLD}" "${RESET}"
