#!/bin/bash
# Generate TypeScript API client from the Composer OpenAPI spec.
#
# Unlike the old flow, this does NOT need a running server — the spec is
# produced offline via cmd/dumpopenapi, which registers every handler with
# nil deps for schema generation only.
#
# Prerequisites:
#   - Go toolchain
#   - bun installed
#
# Usage:
#   ./scripts/generate-client.sh

set -euo pipefail

cd "$(dirname "$0")/.."

SPEC_FILE="web/src/lib/api/openapi.json"
TYPES_FILE="web/src/lib/api/types.ts"

echo "Dumping OpenAPI spec offline via cmd/dumpopenapi..."
go run ./cmd/dumpopenapi/ > "${SPEC_FILE}"

echo "Generating TypeScript types..."
cd web && bunx openapi-typescript "src/lib/api/openapi.json" -o "src/lib/api/types.ts"

echo ""
echo "Generated:"
echo "  ${SPEC_FILE}"
echo "  ${TYPES_FILE}"
echo ""
echo "Usage in frontend:"
echo '  import type { paths } from "@/lib/api/types";'
echo '  import createClient from "openapi-fetch";'
echo '  const api = createClient<paths>({ baseUrl: "/", credentials: "include" });'
