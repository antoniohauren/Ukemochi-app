// Package ukemochi embeds the static files served by the app.
package ukemochi

import "embed"

//go:embed all:static
var Static embed.FS
