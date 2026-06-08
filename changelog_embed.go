package archerycli

import _ "embed"

// Changelog is the root CHANGELOG.md embedded for the runtime changelog command.
//
//go:embed CHANGELOG.md
var Changelog string
