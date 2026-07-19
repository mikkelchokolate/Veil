// Package webui embeds the built React SPA (dist/) so the panel serves it as a
// single static binary. Run `make web` (or `cd web && pnpm build`) before
// `go build` to bake in the current frontend.
package webui

import "embed"

// Dist holds the production SPA build output (index.html + hashed assets).
//
//go:embed all:dist
var Dist embed.FS
