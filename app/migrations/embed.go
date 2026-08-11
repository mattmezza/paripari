// Package migrations embeds the SQL migration files (go:embed cannot reach
// outside its own package directory, hence this one-file package).
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
