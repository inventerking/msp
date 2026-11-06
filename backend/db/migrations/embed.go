// Package migrations embeds the central, ordered SQL migrations.
package migrations

import "embed"

// FS holds every migration file.
//
//go:embed *.sql
var FS embed.FS
