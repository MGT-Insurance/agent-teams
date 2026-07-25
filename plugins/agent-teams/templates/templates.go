// Package templates embeds static template assets shipped alongside the
// agent-teams plugin, so the ateam binary carries them without reading from
// disk at runtime. There is no other embedded-asset precedent in this repo;
// this package exists because go:embed patterns cannot reach outside the
// embedding file's own directory subtree, so the embedding code has to live
// next to plugins/agent-teams/templates/global-prime.md rather than in
// internal/verbs where it's consumed.
package templates

import _ "embed"

// GlobalPrimeMD is the human-approved PRIME.md override installed into the
// global agent-teams workspace (`ateam steward init`, internal/verbs/steward.go)
// at $ATEAM_HOME/.beads/PRIME.md. Installed beads v1.1.0 (cmd/bd/prime.go)
// treats a custom PRIME.md as a total override of `bd prime`'s default
// output — this file replaces the "dump every role's entire memory store
// into every session" default with a short pointer to the role-scoped
// `ateam learnings`/`ateam recall` commands instead.
//
//go:embed global-prime.md
var GlobalPrimeMD string
