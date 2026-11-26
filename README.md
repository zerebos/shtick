# shtick

Know your shell shortcuts. shtick tracks which zsh and PowerShell aliases **and functions** you actually use, nudges you when you type the long form of one, and tells you which ones you never touch.

```
$ git checkout -b feature
shtick: gco = git checkout
```

```
$ shtick report
Most used
NAME  KIND      USES  LAST USED
gst   alias     412   2m ago
mkcd  function  37    3d ago

Never used
NAME   KIND      DEFINITION
gsta   alias     git stash apply
dps    function  { docker ps --format 'table {{.Names}}' }

Typed the long way
NAME  KIND      MISSED  USES  YOU TYPED
gco   function  58      12    { git checkout "$@" }
```

## Why

Tools like zsh-you-should-use and alias-tips remind you about aliases. None of them know about the functions in your `.zshrc`, and none tell you which shortcuts are dead weight. shtick does both.

## Install

```sh
go install github.com/zerebos/shtick@latest
```

Then add to `~/.zshrc`, after your aliases and functions are defined:

```zsh
eval "$(shtick init zsh)"
```

Or, with a plugin manager, point it at `shell/shtick.plugin.zsh` in this repo.

### PowerShell (experimental)

Add to your `$PROFILE`, after your aliases and functions are defined:

```powershell
Invoke-Expression (& shtick init pwsh | Out-String)
```

Works in pwsh 7 and Windows PowerShell 5.1, and needs PSReadLine (on by default in both). PowerShell aliases can't take arguments, so the shortcuts worth nudging are functions like `function gst { git status @args }`. Built-in aliases such as `gci` and functions from modules are skipped.

Open a new shell. To count what you did before installing:

```sh
shtick import
```

## Commands

| command | does |
| --- | --- |
| `shtick report` | most used, never used, and typed the long way |
| `shtick top [-n N]` | most used aliases and functions |
| `shtick unused` | defined but never used |
| `shtick missed [-n N]` | times you typed the long form |
| `shtick suggest [--min N]` | commands you type often that have no shortcut, with a proposed name |
| `shtick import [--force]` | one-time backfill from `$HISTFILE` |
| `shtick defs [--skipped]` | what shtick tracks, whether it can nudge, and what it skips |
| `shtick init zsh\|pwsh` | print the zsh or PowerShell plugin |

Run these from a shell with the plugin loaded so they see the current shell's aliases and functions.

## What gets nudged

- **Aliases:** typing an alias's value, with or without extra arguments. The most specific alias wins, so `git status -s` suggests `gss` over `gst`.
- **Functions:** when the body is a plain command or sequence of commands, optionally ending in `"$@"`. `gco() { git checkout "$@" }` matches `git checkout -b x`. Functions with `$1`, local variables, or control flow are tracked for usage but not nudged; `shtick defs` shows which.

In PowerShell, a function body counts as a shortcut when it's a plain command or sequence of commands, optionally ending in `@args` or `$args`. Bodies with `param()`, `$args[0]`, `$_`, or control flow are tracked but not nudged. Command names match regardless of case.

## Which functions count

zsh carries hundreds of framework and completion functions, so shtick skips names starting with `_`, hook functions, prompt themes, unloaded autoloads, and anything defined under oh-my-zsh, zinit, antidote, zim, zplug, prezto, or system `site-functions`. `shtick defs --skipped` lists them with the reason.

## Configuration

| variable | default | effect |
| --- | --- | --- |
| `SHTICK_NUDGE` | on | `0` records without printing nudges |
| `SHTICK_DISABLE` | unset | any value turns the hook off for that shell |
| `SHTICK_IGNORE` | none | space-separated name globs to skip, e.g. `'tmp* x'` |
| `SHTICK_IGNORE_SOURCES` | framework paths | space-separated source path globs to skip; set empty to track everything |
| `SHTICK_DATA_DIR` | `~/.local/share/shtick` (Windows: `%LOCALAPPDATA%\shtick`) | usage log location |
| `SHTICK_STATE_DIR` | `~/.local/state/shtick` (Windows: `%LOCALAPPDATA%\shtick\state`) | per shell definition dumps |
| `SHTICK_DEBUG` | unset | print hook errors |

## Privacy

Everything stays on your machine. The usage log records a timestamp and the name of the alias or function you used or missed, never the command line or its arguments. `shtick suggest` and `shtick import` read your history file when you run them and store nothing extra.

## How it works

The plugin writes this shell's aliases and functions to a small file whenever they change, then calls `shtick hook` from `preexec`. The hook matches the typed line, prints a nudge, and appends to the log. See [DESIGN.md](DESIGN.md).

## Status

zsh is supported. PowerShell is experimental. bash and fish are planned.

## License

Apache 2.0
