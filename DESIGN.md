# shtick design

shtick tracks which of your zsh aliases and functions you actually use, nudges you when you type the long form of one, and reports what you use most, what you never touch, and what you keep typing by hand.

## Goals

- Cover functions, not just aliases. No existing tool does.
- Zero network, local files only.
- Record names, never arguments. The log should be safe to share.
- Fast enough to run on every command (target: a few ms per prompt).
- Useful on day one: backfill from existing history.

## Non-goals (v1)

- bash and fish. The core is shell-agnostic, but only the zsh plugin ships.
- A TUI. Plain table output first; a Bubbletea view can come later.
- Suffix aliases (`alias -s`).

## Architecture

```
zsh plugin (shtick.plugin.zsh)          Go binary (shtick)
  precmd: dump defs when they change -->  defs file (per shell)
  preexec: shtick hook -- "$1"       -->  match line, print nudge, append usage log
  shtick() wrapper passes --defs     -->  reports, import, suggest
```

The binary can't see shell state, so the plugin exports it. On each prompt it checks a cheap signature (counts of aliases, global aliases, and functions). When the signature changes it writes every alias, global alias, and function (with body and source file) to `$state/defs/<pid>.defs`, NUL separated. Each shell has its own file because sessions can define different things. Files from dead shells are pruned by PID.

### What counts as "your" function

zsh carries hundreds of framework and completion functions. Defaults skip:

- names starting with `_`, `.`, `+`, or `-`, and `prompt_*`
- hook names (`precmd`, `preexec`, `chpwd`, `TRAP*`, and the rest)
- unloaded autoload stubs
- functions whose source file matches a framework path (oh-my-zsh, zinit, antidote, zim, zplug, prezto, site-functions, /usr/share)

`SHTICK_IGNORE` adds name globs and `SHTICK_IGNORE_SOURCES` replaces the source globs.

### Matching a typed line

The line is split on `|`, `||`, `&&`, `;`, `&`, and newlines (quote, `$( )`, and backtick aware) into simple commands. Leading `VAR=value` words and precommand modifiers (`noglob`, `nocorrect`, `command`, `builtin`, `exec`, `time`, `sudo`, `doas`) are skipped.

- **Alias use:** the command word is an alias. Global aliases count anywhere in the line.
- **Function use:** the command word is a tracked function.
- **Missed alias:** the command's words start with an alias's value and the alias name is shorter. Longest value wins.
- **Missed function:** the function body (as zsh reformats it) is a plain template. Each body line is a statement. If the last statement ends in `"$@"`, `$@`, `$*`, or `"$*"`, the typed statements must match the body with the last one as a prefix; otherwise they must match exactly. Bodies with positional parameters (`$1`) or control flow are skipped in v1.

### Nudge

`preexec` runs `shtick hook`, which prints to stderr before the command runs:

```
shtick: gst = git status
```

`SHTICK_NUDGE=0` keeps recording but stays quiet.

## Storage

`$XDG_DATA_HOME/shtick` (default `~/.local/share/shtick`):

- `usage.tsv`: `unix_ts  kind  name  source`, kind is `alias`, `function`, or `missed`, source is `hook` or `history`. Appended with O_APPEND; lines stay well under PIPE_BUF.

`$XDG_STATE_HOME/shtick` (default `~/.local/state/shtick`):

- `defs/<pid>.defs`: per shell definition dumps.

## Commands

| command | does |
| --- | --- |
| `shtick report` | top used, never used, and missed sections |
| `shtick top [-n N]` | most used aliases and functions |
| `shtick unused` | defined but never used |
| `shtick missed` | times you typed the long form |
| `shtick suggest` | long commands from history you type often with no alias or function |
| `shtick import` | one-time backfill from `$HISTFILE` |
| `shtick defs` | list what shtick is tracking and why something is skipped |
| `shtick hook` | internal, called from preexec |

Reports only count names that exist in the current shell's definitions, so a renamed or deleted alias drops out.

### Import

Parses zsh history (extended `: ts:dur;cmd` or plain, with backslash continuations and zsh metafied bytes). To avoid double counting, it only imports entries older than the first hook event, and only once (`--force` replaces an earlier import).

### Suggest

Scans history, counts the first two to three words of each simple command that no alias or function covers, and prints candidates above a threshold with a proposed name (initials, checked against existing aliases, functions, and `$PATH`).

## Why Go

Matches his other CLIs (Ferry, scute, mmtool). A static binary starts in about a millisecond, which matters for a hook that runs on every command, and a single binary is easy to ship through Homebrew and `go install`. Standard library only (no Cobra) to keep the hook path lean and the module dependency free.

## Later

- bash (`PROMPT_COMMAND` + `DEBUG` trap) and fish (`fish_preexec`, abbreviations)
- `--json` output, a Bubbletea view
- log compaction
- functions with positional parameters (`$1`) as templates
