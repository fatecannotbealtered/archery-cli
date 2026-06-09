package archerycli

import _ "embed"

// ChangelogMarkdown is the root CHANGELOG.md embedded for the runtime changelog command.
//
//go:embed CHANGELOG.md
var ChangelogMarkdown string
