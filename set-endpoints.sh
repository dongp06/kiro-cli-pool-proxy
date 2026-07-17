#!/bin/bash
# Point kiro-cli at the pool proxy (no MITM, no cert needed).
# Uses kiro-cli's built-in endpoint settings, verified from binary 2.12.2:
#   api.krs.service → runtime (chat/GenerateAssistantResponse)
#   api.cps.service → management (profiles/usage limits)
#   api.codewhisperer.service → legacy telemetry endpoint
#
# Usage:   ./set-endpoints.sh http://PROXY_IP:9999 [region]
# Reset:   ./set-endpoints.sh --reset
set -e

KIRO="${KIRO_CLI:-kiro-cli}"
REGION="${2:-us-east-1}"

if ! command -v "$KIRO" >/dev/null 2>&1; then
    echo "❌ kiro-cli not found. Set KIRO_CLI=/path/to/kiro-cli"
    exit 1
fi

if [ "$1" == "--reset" ]; then
    echo "Resetting kiro-cli endpoints to default..."
    "$KIRO" settings -d api.krs.service 2>/dev/null || true
    "$KIRO" settings -d api.cps.service 2>/dev/null || true
    "$KIRO" settings -d api.codewhisperer.service 2>/dev/null || true
    echo "✅ Endpoints reset. kiro-cli now talks directly to Kiro."
    exit 0
fi

PROXY="$1"
if [ -z "$PROXY" ]; then
    echo "Usage: $0 http://PROXY_IP:9999 [region]"
    echo "       $0 --reset"
    exit 1
fi

VAL="{\"endpoint\":\"$PROXY\",\"region\":\"$REGION\"}"

echo "Pointing kiro-cli at proxy: $PROXY (region=$REGION)"
"$KIRO" settings api.krs.service "$VAL"           # chat / assistant
"$KIRO" settings api.cps.service "$VAL"           # profiles / usage limits
"$KIRO" settings api.codewhisperer.service "$VAL" # telemetry (optional)

echo ""
echo "✅ Done. Verify:"
echo "   $KIRO settings api.krs.service"
echo ""
echo "Now run: $KIRO chat"
echo "All chat traffic routes through the pool proxy (account rotation + credit accounting)."
echo "To revert: $0 --reset"
