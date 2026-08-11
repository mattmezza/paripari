// Package templates embeds the HTML templates (go:embed cannot reach outside
// its own package directory, hence this one-file package).
package templates

import "embed"

//go:embed layouts/*.html partials/*.html *.html
var FS embed.FS
