#!/usr/bin/env bash
# Registers this bridge's webhook URL as a callback subscription on messaging-core and prints
# the signing secret it returns. The secret is only ever shown once by messaging-core — copy
# it into this project's CALLBACK_SIGNING_SECRET immediately, this script never persists it.
set -euo pipefail
umask 077

CORE_URL="${MESSAGING_CORE_URL:-http://127.0.0.1:8090}"
BRIDGE_URL="${BRIDGE_URL:?set BRIDGE_URL to this service's publicly reachable base URL, e.g. http://hermes-messaging-bridge:8095}"

for command in curl jq; do
  command -v "$command" >/dev/null || { echo "missing required command: $command" >&2; exit 1; }
done

: "${CLIENT_ID:?set CLIENT_ID to the messaging-core tenant clientId this bridge will act as}"
: "${CLIENT_SECRET:?set CLIENT_SECRET to that tenant's clientSecret}"

TOKEN="$(curl --fail-with-body --silent --show-error \
  -X POST "$CORE_URL/oauth/token" \
  -u "$CLIENT_ID:$CLIENT_SECRET" \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d 'grant_type=client_credentials' | jq -er '.access_token')"

response="$(curl --fail-with-body --silent --show-error \
  -X POST "$CORE_URL/api/v1/callbacks" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"url\":\"${BRIDGE_URL%/}/webhooks/messaging-core\"}")"

echo "Callback registered: $(jq -r '.id' <<<"$response")"
echo "Signing secret (shown once — copy it now into CALLBACK_SIGNING_SECRET):"
jq -r '.secret' <<<"$response"
