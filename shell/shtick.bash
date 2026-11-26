# shtick bash plugin (bash 4.4 or newer)
# Load at the end of ~/.bashrc, after your aliases, functions, and prompt:
#   eval "$(shtick init bash)"
# Records which aliases and functions you use and nudges you when you type
# the long form of one. Set SHTICK_NUDGE=0 to record quietly, or
# SHTICK_DISABLE=1 to turn it off for a session.

[[ $- == *i* ]] || return 0
[[ -n ${_shtick_defs-} ]] && return 0 # already loaded

if (( BASH_VERSINFO[0] < 4 || (BASH_VERSINFO[0] == 4 && BASH_VERSINFO[1] < 4) )); then
  echo "shtick: bash 4.4 or newer is needed, this is $BASH_VERSION" >&2
  return 1
fi
if ! type -P shtick >/dev/null; then
  echo "shtick: binary not found in PATH" >&2
  return 1
fi

_shtick_defs="${SHTICK_STATE_DIR:-${XDG_STATE_HOME:-$HOME/.local/state}/shtick}/defs/$$.defs"
_shtick_sig=""
_shtick_histcmd=""

# Write this shell's aliases and functions for the binary to read. Runs on
# every prompt but only writes when the counts change. Completion functions
# (names starting with _) are left out here to keep the dump small. Builtins
# with redirections don't fork, so lists go through scratch files rather than
# $( ). extdebug is on only while dumping, so `declare -F` reports each
# function's source file.
_shtick_dump() {
  local f k ext=0 dir=${_shtick_defs%/*} tmp=$_shtick_defs.tmp
  local -a fns
  [[ -d $dir ]] || mkdir -p -m 700 -- "$dir" || return
  compgen -A function -X '_*' >| "$tmp" 2>/dev/null && mapfile -t fns < "$tmp" || return
  local sig="${#BASH_ALIASES[@]} ${#fns[@]}"
  [[ $sig == "$_shtick_sig" && -e $_shtick_defs ]] && return
  shopt -q extdebug && ext=1
  shopt -s extdebug
  {
    printf '%s\0' shell bash "" "" # names the shell for the binary
    for k in "${!BASH_ALIASES[@]}"; do printf '%s\0' alias "$k" "" "${BASH_ALIASES[$k]}"; done
    for f in "${fns[@]}"; do
      printf 'function\0%s\0' "$f"
      declare -F -- "$f"
      printf '\0'
      declare -f -- "$f"
      printf '\0'
    done
  } >| "$tmp"
  (( ext )) || shopt -u extdebug
  command mv -f -- "$tmp" "$_shtick_defs" && _shtick_sig=$sig
}

# Runs from PROMPT_COMMAND. Notes the next history number so the preexec can
# tell whether the line it is about to see was added to history. Keeps $? for
# a PS1 that shows it.
_shtick_precmd() {
  local status=$?
  _shtick_histcmd=$HISTCMD
  _shtick_dump
  return $status
}

# Runs inside PS0's command substitution, a subshell, after a line is read and
# before it runs. bash has no preexec hook that sees the typed line (by the
# time $BASH_COMMAND is set, aliases are already expanded), so the line comes
# from history. When HISTCMD has not moved, the line was not saved: it was a
# duplicate (ignoredups, erasedups), started with a space (ignorespace),
# matched HISTIGNORE, or history is off. A duplicate is counted again only
# when nothing else could have kept it out.
_shtick_preexec() {
  [[ -n ${SHTICK_DISABLE-} ]] && return
  if [[ $HISTCMD == "$_shtick_histcmd" ]]; then
    [[ -o history && -z ${HISTIGNORE-} && :${HISTCONTROL-}: != *:ignorespace:* && :${HISTCONTROL-}: != *:ignoreboth:* ]] || return
    [[ :${HISTCONTROL-}: == *:ignoredups:* || :${HISTCONTROL-}: == *:erasedups:* ]] || return
  fi
  local entry=$_shtick_defs.line
  HISTTIMEFORMAT= builtin history 1 >| "$entry" 2>/dev/null || return
  SHTICK_DEFS=$_shtick_defs exec shtick hook --history-entry "$entry" >/dev/null
}

# With bash-preexec loaded, its preexec passes the typed line directly.
_shtick_bp_preexec() {
  [[ -n ${SHTICK_DISABLE-} ]] && return
  SHTICK_DEFS=$_shtick_defs command shtick hook -- "$1"
}

_shtick_exit() {
  command rm -f -- "$_shtick_defs" "$_shtick_defs.tmp" "$_shtick_defs.line"
}

# Reports go through this wrapper so they see this shell's definitions and
# history file.
shtick() {
  _shtick_sig=""
  _shtick_dump
  SHTICK_DEFS=$_shtick_defs SHTICK_HISTFILE=${HISTFILE-} SHTICK_SHELL=bash command shtick "$@"
}

if [[ -n ${bash_preexec_imported-}${__bp_imported-} ]] && declare -p preexec_functions &>/dev/null; then
  precmd_functions+=(_shtick_precmd)
  preexec_functions+=(_shtick_bp_preexec)
else
  if [[ -v PROMPT_COMMAND && ${PROMPT_COMMAND@a} == *a* ]]; then # an array, bash 5.1+
    PROMPT_COMMAND+=(_shtick_precmd)
  else
    PROMPT_COMMAND="${PROMPT_COMMAND:+${PROMPT_COMMAND%;}; }_shtick_precmd"
  fi
  PS0="${PS0-}"'$(_shtick_preexec)'
  shopt -q promptvars || echo "shtick: promptvars is off, so commands are not recorded (shopt -s promptvars)" >&2
fi

# Run after any EXIT trap already set. One set later replaces this, which is
# fine: files from shells that are gone are pruned by PID.
_shtick_trap_exit() {
  local cur
  cur=$(trap -p EXIT)
  if [[ -n $cur ]]; then
    eval "set -- $cur" # trap -- 'command' EXIT
    trap -- "$3"$'\n_shtick_exit' EXIT
  else
    trap _shtick_exit EXIT
  fi
}
_shtick_trap_exit
unset -f _shtick_trap_exit
