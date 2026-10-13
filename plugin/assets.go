// Package pluginassets is the single source of the bundled local MCP App.
package pluginassets

import "embed"

//go:embed ui/index.html ui/app.css ui/app.mjs plugin.json .codex-plugin/plugin.json skills hooks
var Files embed.FS
