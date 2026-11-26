# shtick design

shtick tracks which of your zsh and bash aliases and functions you actually use, nudges you when you type the long form of one, and reports what you use most, what you never touch, and what you keep typing by hand.

## Goals

- Cover functions, not just aliases. No existing tool does.
- Zero network, local files only.
- Record names, never arguments. The log should be safe to share.
- Fast enough to run on every command (target: a few ms per prompt).
- Useful on day one: backfill from existing history.

## Non-goals (v1)

- fish. The core is shell-agnostic; zsh, bash, and PowerShell plugins ship.
- bash older than 4.4 (no `PS0`), including macOS's /bin/bash 3.2.
- A TUI. Plain table output first; a Bubbletea view can come later.
- Suffix aliases (`alias -s`).

## Architecture

```
zsh plugin (shtick.plugin.zsh)          Go binary (shtick)
  precmd: dump defs when they change -->  defs file (per shell)
  preexec: shtick hook -- "$1"       -->  match line, print nudge, append usage log
  shtick() wrapper passes --defs     -->  reports, import, suggest

bash plugin (shtick.bash)
  PROMPT_COMMAND: dump defs          -->  defs file (per shell)
  PS0: history 1 > file, shtick hook -->  same hook, line read from the file
```

The binary can't see shell state, so the plugin exports it. On each prompt it checks a cheap signature (counts of aliases, global aliases, and functions). When the signature changes it writes every alias, global alias, and function (with body and source file) to `$state/defs/<pid>.defs`, NUL separated, as `kind name source value` records. A dump may start with a `shell` record naming the shell (`shell\0bash\0\0\0`); dumps without one are zsh. Each shell has its own file because sessions can define different things. Files from dead shells are pruned by PID.

### What counts as "your" function

zsh and bash carry hundreds of framework and completion functions. Defaults skip:

- names starting with `_`, `.`, `+`, or `-`, and `prompt_*`
- hook names (`precmd`, `preexec`, `chpwd`, `TRAP*`, bash's `command_not_found_handle`, and the rest)
- unloaded autoload stubs
- functions whose source file matches a framework path (oh-my-zsh, zinit, antidote, zim, zplug, prezto, site-functions, bash-completion, oh-my-bash, bash-it, /etc/profile.d, /usr/share)

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

### bash

The bash plugin (`shell/shtick.bash`) feeds the same binary. The differences:

- **Dump.** Aliases come from `BASH_ALIASES`; bash has no global aliases. Functions come from `compgen -A function -X '_*'`, with `declare -F` (under `extdebug`, switched on only while dumping) as the source and `declare -f` as the value. The dump starts with a `shell bash` record. For bash dumps only, the defs loader unwraps `name () { ... }` so bodies match like zsh's, and maps the sources `main` and `environment` (typed at the prompt, inherited) to none. The per prompt signature is every alias name and value plus `declare -f` of every tracked function, not just counts, so redefining one at the prompt refreshes the dump (about 0.25 ms more per prompt than counts, with 120 definitions). The dump uses builtins with redirections and scratch files instead of `$( )`, so the check doesn't fork.
- **The typed line.** bash has no preexec that sees the raw line: `$BASH_COMMAND` holds one simple command after alias expansion, so an alias would look like its long form. The plugin reads the line from history. `PS0` (bash 4.4) is expanded after a line is read and before it runs; its `$( )` writes `HISTTIMEFORMAT= history 1` to `<pid>.defs.line` and execs `shtick hook --history-entry`, which strips the history number.
- **Lines not in history.** `PS0` runs in a subshell and can't keep state, so `PROMPT_COMMAND` notes `$HISTCMD` before each prompt. If it hasn't moved when `PS0` runs, no entry was added, and `history 1` would return the previous command. The hook is skipped, unless `ignoredups` or `erasedups` is the only thing that could have dropped the line, in which case it is a repeat and counts again. Under `ignoreboth` a repeat and a space-prefixed line look the same, so repeats count once.
- **bash-preexec.** If it is loaded first, the plugin adds itself to `preexec_functions` and `precmd_functions` and leaves `PS0` alone.
- **Shell hygiene.** The precmd keeps `$?` for `PS1`, runs last in `PROMPT_COMMAND` (so a `history -n` before it doesn't skew `$HISTCMD`), handles an array `PROMPT_COMMAND` (bash 5.1), loads under `set -u`, and chains onto an existing EXIT trap.
- **History.** bash files have optional `#<unix time>` lines; when present, the lines up to the next one are one (lithist) entry. bash doesn't metafy, so the zsh decoder isn't used. The format comes from the file name (so `--histfile ~/.zsh_history` reads as zsh), then the dump: a `shell bash` record means bash, and no record means zsh. `SHTICK_SHELL` (set by the plugin wrappers) and then `$SHELL` are used only for a shell name shtick doesn't know.

### PowerShell

The plugin (`shell/shtick.plugin.ps1`) follows the same shape, with these differences:

- **Hook.** PowerShell has no `preexec`. The plugin uses PSReadLine's `AddToHistoryHandler`, which runs after Enter and before the command runs. Only one handler can be registered, so the plugin calls the previous one (by default PSReadLine's, which keeps lines that look like secrets out of the history file) and returns its answer. The definitions dump happens in the same handler instead of on the prompt, so the plugin doesn't have to wrap `prompt` and fight with prompt themes over it.
- **Passing the line.** The line goes to `shtick hook` through `SHTICK_LINE`, since Windows PowerShell 5.1 mangles quotes in native command arguments. `--stdout` sends the nudge back on stdout, and the plugin prints it with `Write-Host`. The plugin saves and restores `$LASTEXITCODE` around the call.
- **Dump.** The shell record is `shell\0pwsh\0\0\0`. It tells the binary to fold case, skip PowerShell built-ins, and read PSReadLine history, which is always PSReadLine whatever the file is called. The wrapper also sets `SHTICK_SHELL=pwsh`. The plugin marks aliases and functions from `InitialSessionState.CreateDefault()` as `builtin`. Function sources are `ScriptBlock.File`, or the module's path, matched against `DefaultPwshIgnoreSources` with backslashes turned into forward slashes.
- **Parsing.** `shparse.SplitPwsh` covers PowerShell quoting: backtick escapes and line continuations, `''` and `""` doubling, smart quotes, groups like `( )`, `$( )`, `@( )`, `{ }` and `@{ }` kept as single words, and `#` and `<# #>` comments. `StripPwsh` drops the `&` and `.` operators, `sudo`, and `$x =` assignments. Command names match case-insensitively; arguments don't.
- **Templates.** A trailing `@args` or `$args` makes a template a prefix match, and any other use of `$args`, `$input`, `$_`, `$PSBoundParameters` or `$MyInvocation` disqualifies it. So do keywords, `param(...)`, attributes like `[CmdletBinding()]`, and statements that start with `$`, `(` or `@`.
- **History.** PSReadLine's file has no timestamps. The first time the plugin loads, it writes the history file's length to `$state/pwsh-history.mark`, and `import` reads only the bytes before that mark. Imported records get the source `history-pwsh`, and bash imports get `history-bash`, so importing one shell's history neither blocks nor replaces another's. zsh keeps plain `history` for existing logs.
- **Suggest.** It prints `function name { cmd @args }` instead of an alias, because PowerShell aliases can't take arguments.

## Storage

`$XDG_DATA_HOME/shtick` (default `~/.local/share/shtick`):

- `usage.tsv`: `unix_ts  kind  name  source`, kind is `alias`, `function`, or `missed`, source is `hook`, `history` (zsh), or `history-<shell>`. Imported entries without a timestamp have `unix_ts` 0. Appended with O_APPEND; lines stay well under PIPE_BUF.

`$XDG_STATE_HOME/shtick` (default `~/.local/state/shtick`):

- `defs/<pid>.defs`: per shell definition dumps. bash also keeps `<pid>.defs.tmp` and `<pid>.defs.line` scratch files; all three are pruned with the shell.

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

Parses zsh history (extended `: ts:dur;cmd` or plain, with backslash continuations and zsh metafied bytes) or bash history (with or without `#ts` lines). To avoid double counting, it only imports entries older than the first hook event, and only once (`--force` replaces an earlier import).

### Suggest

Scans history, counts the first two to three words of each simple command that no alias or function covers, and prints candidates above a threshold with a proposed name (initials, checked against existing aliases, functions, and `$PATH`).

## Why Go

Matches his other CLIs (Ferry, scute, mmtool). A static binary starts in about a millisecond, which matters for a hook that runs on every command, and a single binary is easy to ship through Homebrew and `go install`. Standard library only (no Cobra) to keep the hook path lean and the module dependency free.

## Later

- fish (`fish_preexec`, abbreviations)
- `--json` output, a Bubbletea view
- log compaction
- functions with positional parameters (`$1`) as templates
