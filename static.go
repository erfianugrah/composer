// Package composer provides the embedded frontend assets.
package composer

import "embed"

// FrontendDist contains the built Astro frontend (web/dist/).
// Populated by `bun run build` in web/ before `go build`.
// If web/dist doesn't exist, this will be empty and the binary serves API-only.
//
//go:embed all:web/dist
var FrontendDist embed.FS
