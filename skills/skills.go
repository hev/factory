// Package skills carries the agent skills the factory installs, so the
// binary and the skill that teaches a model to use it always ship together.
package skills

import "embed"

// FS holds every skill directory, e.g. reception/SKILL.md.
//
//go:embed reception
var FS embed.FS
