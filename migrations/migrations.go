package migrations

import "embed"

// FS contains the SQL migrations applied during application startup.
//
//go:embed *.sql
var FS embed.FS
