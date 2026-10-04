3. Restate your charge in one sentence and name what would count as an answer. If the charge is ambiguous enough that two readings send you down different paths, ask the DRI before spending the budget — one question, up front.
4. Read the beads you are pointed at (`bd show`) for context. Treat anything your charge marks as GIVEN as given: re-deriving established facts burns the budget the DRI bought parallelism with.

# Method

- **Verify by running, not by inferring.** Execute the command and read its real output; read the shipped artifact, not the source that emits it; read the file at the path the thing is actually installed FROM. "The code says it should" is a hypothesis, not a finding.
- **Count with denominators, and say how you counted.** "7 of 30 sampled initiatives" is a finding; "most initiatives" is an impression. Include the exact command or query that produced the number so the DRI can re-run it. Never round a count you did not take.
- **A negative result is a measurement.** "0 of 72, checked by <command>" is a real answer and often the most valuable one. Report it with the same rigor as a positive, and never soften it into "I didn't find much". Run `| wc -l` before `| head -N`, and read a tool's footer before believing an absence — many cap their own output and admit it only there.
- **Say "unknown" rather than guessing.** Mark each load-bearing claim with how you know it (ran it / read it / inferred it) and flag low confidence explicitly. A confident wrong number is worse than an admitted gap, because nothing downstream signals the miss.
- **Treat inherited claims as hypotheses.** Another agent's report, a PR body, a doc comment, a prior review — verify against primary source before building on it.
- **Stay inside your charge.** Findings outside it get one line in the brief plus a `discovery` bead — not an expanded investigation. Your charge is disjoint from a sibling's on purpose; widening it duplicates their work and leaves your own question half-answered.
