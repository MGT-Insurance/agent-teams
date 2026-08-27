
## 2. Capture the human's framing

The only judgment here is whether the handoff inputs are present. Do not investigate or analyze the repository to fill perceived gaps.

- **Problem statement:** take the one-line statement verbatim from the invocation. If none exists, ask the human for it. Do not rephrase or embellish it.
- **Context:** copy the human's constraints, background, decisions, and unanswered questions verbatim into a temporary body file outside the repository. Do not add analysis, mechanism opinions, or design assumptions. Pass open questions through unanswered. Omit the file and `--body-file` only when the human supplied no additional context.
- **Target repository:** the initiative can target a different repository from the dispatcher's current directory. Default to the single unambiguous current Git repository. Pass `--repo <absolute-path>` only when the human named another target, cwd is not inside a repository, the problem clearly refers to a different project you cannot locate, or more than one repository plausibly fits. Do not explore code to choose.
- **Base branch:** let dispatch detect the repository's default branch. Pass `--base-branch <branch>` only when the human named or clearly implied a non-default base, or when there is genuine base ambiguity.
- **Standby:** pass `--standby` only when the invocation contains that token or an explicit request to park or wait. This is mechanical passthrough, not a judgment about whether standby is warranted. Standby never cancels the launch.
