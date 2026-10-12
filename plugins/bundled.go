// Package plugins contains the built-in pack, also available without filesystem assets.
package plugins

import "embed"

//go:embed builtin/*.json
var Builtin embed.FS
