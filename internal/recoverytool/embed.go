package recoverytool

import "embed"

// tool contains the complete wp-clean-rebuild source, tests, scripts,
// documentation and trusted theme package. Runtime data and Python virtual
// environments are deliberately excluded from the executable bundle.
//
//go:embed all:tool
var embeddedTool embed.FS
