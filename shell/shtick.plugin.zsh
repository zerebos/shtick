# shtick zsh plugin
# Load with: eval "$(shtick init zsh)"   or source this file from .zshrc.
# Records which aliases and functions you use and nudges you when you type
# the long form of one. Set SHTICK_NUDGE=0 to record quietly, or
# SHTICK_DISABLE=1 to turn it off for a session.

if (( ! $+commands[shtick] )); then
  print -u2 "shtick: binary not found in PATH"
  return 1
fi

zmodload zsh/parameter 2>/dev/null
autoload -Uz add-zsh-hook

typeset -g _shtick_defs="${SHTICK_STATE_DIR:-${XDG_STATE_HOME:-$HOME/.local/state}/shtick}/defs/$$.defs"
typeset -g _shtick_sig=""

# Write this shell's aliases and functions for the binary to read. Runs on
# every prompt but only writes when the counts change. Completion functions
# (names starting with _) are left out here to keep the dump small.
_shtick_dump() {
  emulate -L zsh
  local -a fns=(${(k)functions:#_*})
  local sig="${#aliases} ${#galiases} ${#fns}"
  [[ $sig == $_shtick_sig && -e $_shtick_defs ]] && return
  local dir=${_shtick_defs:h} tmp=$_shtick_defs.tmp k
  [[ -d $dir ]] || mkdir -p -m 700 -- $dir || return
  {
    for k in ${(k)aliases}; do print -rN -- alias "$k" "" "${aliases[$k]}"; done
    for k in ${(k)galiases}; do print -rN -- global "$k" "" "${galiases[$k]}"; done
    for k in $fns; do print -rN -- function "$k" "${functions_source[$k]-}" "${functions[$k]}"; done
  } >| $tmp && command mv -f -- $tmp $_shtick_defs && _shtick_sig=$sig
}

_shtick_preexec() {
  [[ -n $SHTICK_DISABLE ]] && return
  SHTICK_DEFS=$_shtick_defs command shtick hook -- "$1"
}

_shtick_exit() {
  command rm -f -- $_shtick_defs
}

# Reports go through this wrapper so they see this shell's definitions and
# history file.
shtick() {
  _shtick_sig=""
  _shtick_dump
  SHTICK_DEFS=$_shtick_defs SHTICK_HISTFILE=$HISTFILE command shtick "$@"
}

add-zsh-hook precmd _shtick_dump
add-zsh-hook preexec _shtick_preexec
add-zsh-hook zshexit _shtick_exit
