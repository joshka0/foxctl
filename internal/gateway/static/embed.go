// Package static provides embedded frontend assets for the web terminal.
package static

import "embed"

// Assets contains the embedded static files for the xterm.js frontend.
//
//go:embed index.html xterm.css xterm.js xterm-addon-fit.js xterm-addon-fit.css
var Assets embed.FS
