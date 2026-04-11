// Package static provides embedded frontend assets for the web terminal.
//
// Asset versions (update by running: npm install @xterm/xterm@<ver> @xterm/addon-fit@<ver>
// in a temp dir, then copying lib/xterm.js, css/xterm.css, lib/addon-fit.js here):
//
//	@xterm/xterm      6.0.0   → xterm.js, xterm.css
//	@xterm/addon-fit  0.11.0  → xterm-addon-fit.js
package static

import "embed"

// Assets contains the embedded static files for the xterm.js frontend.
//
//go:embed index.html xterm.css xterm.js xterm-addon-fit.js xterm-addon-fit.css
var Assets embed.FS
