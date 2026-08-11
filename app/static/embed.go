// Package static embeds the built CSS/JS/icon assets (go:embed cannot reach
// outside its own package directory, hence this one-file package).
// The design agent owns everything else in this directory.
package static

import "embed"

//go:embed all:*
var FS embed.FS
