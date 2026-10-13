// Package plugins contains the built-in pack, also available without filesystem assets.
package plugins

import "embed"

//go:embed builtin/1/*.json
var Builtin embed.FS
