# Getting Started

This is a Claude Code plugin that lets you hand off a coding task and have it done for you in the background, while you keep working on other things. You describe what you want built in one line; one agent takes charge of it and pulls in others as it needs them — to plan, write the code, test it — and opens a GitHub pull request at the end. Below, "the agent" means the one in charge of your task. You check in whenever you like — it only interrupts you when it genuinely needs a decision only you can make. You review and merge the pull request yourself when it's ready.

This guide covers the one path most people take first: install, one-time setup, hand off a task, follow along, and finish up.

## Before you start

You need three things installed:

- **git** — you probably already have this.
- **[beads](https://github.com/gastownhall/beads)** (the `bd` command) — this system's task-tracking tool. Install it with:
  ```
  brew install beads
  ```
  (npm and a curl-based installer also exist — see the beads repo if you don't use Homebrew.) Confirm it worked:
  ```
  bd --version
  ```
- **[GitHub CLI](https://cli.github.com/)** (`gh`), signed in — run `gh auth login` if you haven't. The background agent uses it to open your pull request at the end.

You'll also need Claude Code itself, obviously.

## Install the plugin

In Claude Code:

```
/plugin marketplace add mgt-insurance/agent-teams
/plugin install agent-teams@agent-teams
/setup-agent-teams
```

If `/setup-agent-teams` isn't recognized right after installing, restart Claude Code and try again.

## One-time setup

`/setup-agent-teams` walks you through setting up your machine. You only do this once, ever — not once per project. It will:

1. Check that `bd` is installed.
2. Ask you the one real question in this whole process: **do you already have a private backup repo from a previous machine** (for the notes agents accumulate over time), or is this your first machine? Based on your answer it either clones your existing one or creates a fresh private one for you. If you don't want to set up a backup repo at all, that's fine — everything still works locally; you'll just see a harmless sync error printed at the end of each task, and you won't have a backup.
3. Ask you to hand-edit `~/.claude/settings.json` in two places (it will tell you exactly what to add):
   - Add this to the `"env"` block:
     ```json
     "CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS": "1"
     ```
     **This is the step people miss.** It's what allows a background team to form at all. It only takes effect in a **new** Claude Code session — and if you skip it, or forget to restart, nothing tells you. Later, when you hand off a task, it will look like it worked, but no team will ever actually form.
   - Add this to the `"permissions.allow"` list:
     ```json
     "Bash(ateam:*)"
     ```
     This just stops this system's own command-line tool from prompting you every time it runs.
4. Make that command-line tool (called `ateam`) available in your terminal. Nothing gets downloaded or built — `ateam` already came with the plugin. Setup just links it into `~/.local/bin`, which is a folder your terminal normally looks in for commands. Safe to re-run.
5. Run a quick self-check and report back that everything is ready.

When it finishes, it will confirm your setup is ready. If step 4's self-check says `ateam: command not found`, see "When things go wrong" below — setup will stop there rather than continue.

## Get your own project ready

This step is easy to skip, and skipping it fails silently — so do it now, before your first task.

Go into the project you actually want work done in (not this repo — *your* project) and set up its task database. The background agents need one in order to track and coordinate their work inside your project.

There are two ways to do it, and which one you want depends on whose repo it is.

**If it's your repo** (a personal project, or one where you can decide this for everyone):

```
cd /path/to/your-project
bd init
```

This adds a `.beads/` folder to the project. You commit it like any other file, and it becomes shared: the task history travels with the repo, and beads syncs its data through your normal git remote. That's usually what you want on your own project.

**If it's a shared repo** — work, open source, anything where adding a folder and pushing data to the shared remote isn't purely your call:

```
cd /path/to/your-project
bd init --stealth
```

Stealth mode sets things up so beads stays invisible to everyone else. It adds the beads files to your clone's private ignore list (`.git/info/exclude`), which itself never gets committed — so nothing beads-related can end up in a commit or a pull request by accident. Everything works the same for you; it's just local.

Two things worth agreeing with your team before using the non-stealth version on a repo you share: it puts a new folder in the project, and beads pushes its data to the same git remote everyone else uses. Neither is destructive, but neither is invisible either. When in doubt, use stealth — you can always switch later.

**If you skip this:** handing off a task will still *appear* to succeed — you'll get back an id and a folder path — but the agent working on it will fail later, somewhere you can't see. The only hint you'll get is a warning line that scrolls by right after you hand off the task, something like:

```
dispatch: warning: could not create root epic (fail-soft): bd create epic: exit status 1
Error: cannot use -C directory "<repo>": no beads project found
```

It's easy to miss and nothing stops for it — so if a task never seems to make progress, this is the first thing to check.

One more thing before you hand off a task: this project also needs a small file that turns agent-teams on for it.

```
cd /path/to/your-project
touch .agent-teams
git add .agent-teams
git commit -m "Enable agent-teams"
```

The file's contents don't matter — empty is fine. What matters is that it sits at the top of your project and gets committed, so it travels with the repo instead of living only in your own copy.

That holds even on a shared repo where you picked the stealth option above: this is one empty file, not a folder of data, and it needs to be in the repo.

**If you skip this:** handing off a task fails immediately, with a message that includes `agent-teams is not enabled for`. Create and commit the file above, then try again.

## Hand over your first task

From inside your project's folder, in Claude Code, type:

```
/dispatch-dri <a sentence or two describing what you want built>
```

This returns in a few seconds with an id, a new folder path, and confirmation that a background agent has started. It does the work in its **own copy of your project**, in a new folder outside your project (not the folder you're sitting in), on its own new branch.

That means **your own project's `git status` will show nothing the whole time it's working** — that's expected, not a sign anything is wrong.

The background agent works without pausing to ask permission for each step, since nobody is there to answer prompts. On some machines the first time this happens you may see a one-time confirmation to allow it — accept it if so.

## Watch it work

Nothing notifies you by default. Check in whenever you want by running:

```
/initiatives
```

("Initiative" is this system's word for one task you've handed off.) This lists every task you have running, grouped by how much attention each one needs — anything waiting on a decision from you appears first, then anything ready for you to review, then everything still in progress.

## When it asks you something

Sometimes the agent hits a decision only you can make, and it pauses and waits. You'll see it under the "needs a decision" group in `/initiatives`.

To answer it, first list what's running in your terminal:

```
claude agents
```

Find your task in the list by its name (the same name you saw when you handed it off) and note its **id** — a short string of letters and numbers next to it. Then:

```
claude attach <id>
```

It has to be the id. The name won't work here — passing the name gives you `No job matching '...'`. Worth knowing because the message printed right after you hand off a task suggests the name; use the id instead. The same goes for `claude logs <id>`, which shows recent output without opening the session.

This opens the session in your terminal. Type your answer and press enter, then press **Ctrl+Z** to drop back to your shell. The task keeps running either way — don't close the window, just detach.

(`attach` doesn't show up if you run `claude --help` — it's a real, working command, just not listed there.)

## When it's done

When the agent finishes, it:

- pushes its work to a new branch on GitHub, and
- opens a pull request for you.

You'll see this in `/initiatives` as ready for your review, with a link to the pull request.

Two things that catch people out:

- **The agent does not stop itself when it's done.** This is by design — it stays running (idle) until you tell it to stop.
- **A pull request can already exist while `/initiatives` still shows the task as "in progress."** That's normal — it flips to "ready to review" once the agent has fully finished tidying up, which can take a bit longer than the pull request itself.

You have two ways to finish, and the second one is less work.

**Option 1 — do it yourself.** Review and merge the pull request the normal way, on GitHub or with `gh`. Then tidy up on this side:

```
ateam close <id> --reason "merged"
```

The `<id>` is the one `/initiatives` shows next to the task. The background agent is also still sitting there idle — it never stops itself — so stop it with `claude stop <id>`, using the short id from `claude agents`.

**Option 2 — let the agent finish it.** Attach to the task (see the previous section) and tell it to **close out**. That phrase is the one it's listening for. Once you've told it the pull request is merged — or asked it to merge for you, which it will do, but only after you say so, never on its own — it will:

- mark the task closed, with the pull request link recorded against it,
- fast-forward your local `main` so your own checkout isn't left behind, and
- clean up the folder and branch it was working in.

Either way, nothing breaks if you don't bother. You'll just have a finished task still showing as open, an idle session still running, and a local `main` that's a few commits behind.

## Telling the reviewer what you care about

Before a task opens its pull request, another agent reviews the work. If you find yourself wanting that reviewer to always check something — a habit your projects have, a mistake you keep seeing — you can write it down once and have it read every time.

Put it in a file at:

```
~/.agent-teams/instructions/reviewer.md
```

Plain text, whatever you want to say. For example: *"Flag any new environment variable that isn't also added to the example env file."*

Things worth knowing:

- **This is entirely optional.** If the file isn't there, nothing happens and nothing complains — that's the normal state, and you can ignore this section completely.
- **It stays on this computer.** You don't commit it anywhere, and it doesn't follow you to another machine. If you later decide you *do* want it everywhere, the folder it lives in is a git repository, so you can commit it there yourself.
- **It adds to what the reviewer already does — it can't switch anything off.** You can ask for extra checks; you can't use it to tell the reviewer to stop reviewing, or to push and merge things itself.
- **Keep it short.** The limit is 4,096 bytes. A file over that is refused outright, with a message saying so — it won't quietly use half of what you wrote.
- **Only the reviewer reads its file today.** The other agents don't yet.

## When things go wrong

- **A task seems stuck from the very start, or teammates never join.** You likely skipped the `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS` setting from one-time setup, or set it without starting a new Claude Code session. Check `~/.claude/settings.json`, add it if missing, and start a fresh session.
- **A task hands off fine but nothing ever seems to happen in it.** You probably never set up the task database in your project — see "Get your own project ready" above for `bd init` and the stealth variant. Look for the "could not create root epic" warning right after you dispatched.
- **Handing off a task fails right away, with `agent-teams is not enabled for ...`.** You skipped the `.agent-teams` file — see "Get your own project ready" above.
- **Setup stops with `ateam: command not found`.** The `~/.local/bin` folder it linked the tool into isn't on your terminal's search path. Add this line to your `~/.zshrc` (or `~/.bashrc`):
  ```
  export PATH="$HOME/.local/bin:$PATH"
  ```
  Then open a new terminal and run `/setup-agent-teams` again — it's safe to re-run. (If the error says "unsupported platform" instead, the link worked but your machine's build of the tool is missing; that's a bug worth reporting.)
- **`/setup-agent-teams` isn't recognized right after installing the plugin.** Restart Claude Code and try again.
- **Your project's `git status` shows nothing while a task is supposedly running.** Expected — the work happens in a separate folder, not your own checkout. Use `/initiatives` to check on it instead.
