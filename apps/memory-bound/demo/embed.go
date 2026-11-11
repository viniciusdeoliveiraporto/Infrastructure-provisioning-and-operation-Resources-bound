// Package demo embeds the manual demo page served by the KV server at /demo/.
package demo

import _ "embed"

// IndexHTML is the demo page (store/retrieve a key from the browser).
//
//go:embed index.html
var IndexHTML []byte
