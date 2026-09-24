// Package scaffold embeds the markdown templates vexillum writes into
// scaffolded projects.
package scaffold

import _ "embed"

//go:generate cp vexillum-commander-rules.md ../../.claude/rules/vexillum.md

// VexillumCommanderRules is the "Vexillum commander rules" product
// scaffold that `vexillum init` writes to .claude/rules/vexillum.md in a
// scaffolded project. It is also this repository's own dogfooded copy at
// .claude/rules/vexillum.md - re-run `go generate ./...` after editing
// this file to keep that copy in sync.
//
//go:embed vexillum-commander-rules.md
var VexillumCommanderRules string
