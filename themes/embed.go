package themes

import "embed"

// FS embeds all JSON theme definitions.
//
//go:embed *.json
var FS embed.FS
