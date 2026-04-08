#!/bin/bash
# Generate TypeScript API client from the OpenAPI spec.
#
# Prerequisites:
#   - composerd running on localhost:8080 (or set COMPOSER_URL)
#   - bun installed
#
# Usage:
#   ./scripts/generate-client.sh
#   COMPOSER_URL=http://localhost:9999 ./scripts/generate-client.sh

set -euo pipefail

COMPOSER_URL="${COMPOSER_URL:-http://localhost:8080}"
SPEC_URL="${COMPOSER_URL}/openapi.json"
OUTPUT_DIR="web/src/lib/api"
TYPES_FILE="${OUTPUT_DIR}/types.ts"

echo "Fetching OpenAPI spec from ${SPEC_URL}..."
if ! curl -sf "${SPEC_URL}" > /dev/null 2>&1; then
  echo "ERROR: Cannot reach ${SPEC_URL}"
  echo "Start the server first:"
  echo "  COMPOSER_PORT=8080 COMPOSER_DB_URL=... go run ./cmd/composerd/"
  exit 1
fi

echo "Generating TypeScript types..."
cd "$(dirname "$0")/.."
bunx openapi-typescript "${SPEC_URL}" -o "${TYPES_FILE}"

echo "Generated: ${TYPES_FILE}"
echo ""
echo "Usage in frontend:"
echo '  import type { paths } from "@/lib/api/types";'
echo '  import createClient from "openapi-fetch";'
echo '  const api = createClient<paths>({ baseUrl: "/", credentials: "include" });'
