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
// at $ATEAM_HOME/.beads/PRIME.md. This file replaces bd's own workflow-text
// preamble with a short pointer to the role-scoped `ateam learnings`/
// `ateam recall` commands instead.
//
// On bd v1.1.0 that was a TOTAL override — installing this file was the whole
// fix, since bd emitted nothing else. Upstream reversed that (GH#3941): on bd
// v1.3.0+ a custom PRIME.md replaces only the workflow text, and every
// persistent memory is still re-appended after it, unbounded. Suppressing
// that section is a separate mechanism — see installPrimeMemoryCaps in
// internal/verbs/steward.go, which sets the prime.max-memories/
// prime.max-memory-chars config keys alongside this file.
//
//go:embed global-prime.md
var GlobalPrimeMD string
