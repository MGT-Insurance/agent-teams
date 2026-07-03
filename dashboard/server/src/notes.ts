// Shared notes-block splitting (agent-teams-rybk.5.3): both parse.ts's
// lastNotesBlock (nextAction fallback) and index.ts's DrillInDetail.notesHistory
// split a raw notes string the same way — on the lookahead before a line
// starting "session " — so "a block" means the same thing everywhere in the
// dashboard.

const SESSION_BLOCK_RE = /\n(?=session )/i;

// Split raw notes into trimmed, non-empty blocks. Each "session N, …" line
// starts a new entry while preserving multi-line content within an entry.
export function splitNotesBlocks(notes: string): string[] {
  return notes
    .split(SESSION_BLOCK_RE)
    .map((s) => s.trim())
    .filter(Boolean);
}
