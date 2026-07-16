#!/usr/bin/env bash
# test-incident-mcp-setup.sh — best-effort local smoke test for the
# incident.io MCP server registration performed by
# docker/engine-claude-code/setup-claude.sh.
#
# Pins contract pin 13: when INCIDENT_IO_API_KEY is present in the agent
# pod's environment, setup-claude.sh merges an "incident-io" remote MCP
# server (url https://mcp.incident.io/mcp, Bearer-token Authorization
# header) into /workspace/.mcp.json; when the variable is absent,
# /workspace/.mcp.json is left untouched.
#
# setup-claude.sh hardcodes the absolute paths /etc/claude-code/*.json and
# /workspace/.mcp.json to match the real agent container's image layout, so
# the only way to exercise the script unmodified is inside a container with
# that layout. This script runs it in a throwaway `alpine` container via
# Docker, bind-mounting the real docker/engine-claude-code fixtures
# read-only at /etc/claude-code.
#
# Requires: docker (to reproduce the container's absolute-path layout) and
# jq (to parse and assert on the resulting mcp.json, on the host running
# this script). This is a local developer convenience, not a CI gate — see
# the PR that introduced it for why it isn't wired into CI. It skips
# cleanly (exit 0) when either dependency is unavailable, rather than
# failing the build.
#
# Usage:
#   ./hack/test-incident-mcp-setup.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
FIXTURE_DIR="${ROOT_DIR}/docker/engine-claude-code"

if ! command -v docker >/dev/null 2>&1; then
  echo "SKIP: docker not found on PATH — this test runs setup-claude.sh inside a throwaway container so its hardcoded /etc/claude-code and /workspace paths behave as they do in production."
  exit 0
fi

if ! docker info >/dev/null 2>&1; then
  echo "SKIP: docker CLI found but the daemon is not reachable (e.g. Docker Desktop is not running)."
  exit 0
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "SKIP: jq not found on PATH — needed here to parse and assert on the resulting mcp.json."
  exit 0
fi

FAILURES=0

# run_case executes setup-claude.sh inside a throwaway alpine container with
# INCIDENT_IO_API_KEY set to $1 (an empty string means "unset"), and prints
# the resulting /workspace/.mcp.json to stdout.
run_case() {
  local api_key="$1"
  local workspace
  workspace="$(mktemp -d)"

  local -a env_args=()
  if [ -n "${api_key}" ]; then
    env_args=(-e "INCIDENT_IO_API_KEY=${api_key}")
  fi

  docker run --rm \
    -v "${FIXTURE_DIR}/mcp.json:/etc/claude-code/mcp.json:ro" \
    -v "${FIXTURE_DIR}/settings.json:/etc/claude-code/settings.json:ro" \
    -v "${FIXTURE_DIR}/setup-claude.sh:/usr/local/bin/setup-claude.sh:ro" \
    -v "${workspace}:/workspace" \
    -w /workspace \
    -e HOME=/home/osmia \
    "${env_args[@]}" \
    --entrypoint /bin/sh \
    alpine:3.20 \
    -c '
      set -e
      apk add --no-cache -q jq >/dev/null
      mkdir -p /home/osmia
      # Stub `claude`: setup-claude.sh execs it as its final step.
      printf "#!/bin/sh\n" > /usr/local/bin/claude
      chmod +x /usr/local/bin/claude
      cp /usr/local/bin/setup-claude.sh /tmp/setup-claude.sh
      chmod +x /tmp/setup-claude.sh
      /tmp/setup-claude.sh >/dev/null 2>&1
      cat /workspace/.mcp.json
    ' 2>/dev/null

  rm -rf "${workspace}"
}

echo "=== Case 1: INCIDENT_IO_API_KEY set ==="
WITH_KEY_MCP="$(run_case "test-incident-io-key-12345")"

if ! echo "${WITH_KEY_MCP}" | jq -e . >/dev/null 2>&1; then
  echo "FAIL: resulting /workspace/.mcp.json is not valid JSON"
  FAILURES=$((FAILURES + 1))
elif echo "${WITH_KEY_MCP}" | jq -e '.mcpServers."incident-io".url == "https://mcp.incident.io/mcp"' >/dev/null 2>&1; then
  echo "PASS: incident-io MCP server URL registered"
else
  echo "FAIL: incident-io MCP server URL missing or incorrect"
  FAILURES=$((FAILURES + 1))
fi

if echo "${WITH_KEY_MCP}" | jq -e '.mcpServers."incident-io".headers.Authorization == "Bearer test-incident-io-key-12345"' >/dev/null 2>&1; then
  echo "PASS: incident-io MCP server carries a Bearer Authorization header with the configured key"
else
  echo "FAIL: incident-io MCP server Authorization header missing or incorrect"
  FAILURES=$((FAILURES + 1))
fi

echo ""
echo "=== Case 2: INCIDENT_IO_API_KEY unset ==="
WITHOUT_KEY_MCP="$(run_case "")"
ORIGINAL_MCP="$(cat "${FIXTURE_DIR}/mcp.json")"

if diff <(echo "${ORIGINAL_MCP}" | jq -S .) <(echo "${WITHOUT_KEY_MCP}" | jq -S .) >/dev/null; then
  echo "PASS: /workspace/.mcp.json is untouched when INCIDENT_IO_API_KEY is unset"
else
  echo "FAIL: /workspace/.mcp.json was modified even though INCIDENT_IO_API_KEY was unset"
  FAILURES=$((FAILURES + 1))
fi

echo ""
if [ "${FAILURES}" -eq 0 ]; then
  echo "All incident.io MCP setup checks passed."
  exit 0
fi
echo "${FAILURES} check(s) failed."
exit 1
