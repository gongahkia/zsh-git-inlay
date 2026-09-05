# zsh-git-inlay is intentionally a strategy provider, not a git wrapper.

if (( ! ${+functions[_zsh_autosuggest_fetch_suggestion]} )); then
  typeset -g ZSH_GIT_INLAY_DEPENDENCY_ERROR='zsh-git-inlay requires zsh-autosuggestions; source it before zsh-git-inlay.plugin.zsh'
  print -u2 -- "$ZSH_GIT_INLAY_DEPENDENCY_ERROR"
  return 0
fi

typeset -g ZSH_GIT_INLAY_CYCLE=${ZSH_GIT_INLAY_CYCLE:-0}
if (( ! ${+ZSH_GIT_INLAY_CYCLE_KEYBINDING} )); then
  ZSH_GIT_INLAY_CYCLE_KEYBINDING="$(command zsh-git-inlay config cycle-keybinding 2>/dev/null || true)"
  [[ -n $ZSH_GIT_INLAY_CYCLE_KEYBINDING ]] || ZSH_GIT_INLAY_CYCLE_KEYBINDING='^Xg'
fi

_zsh_autosuggest_strategy_git-inlay() {
  emulate -L zsh
  local -a arguments
  arguments=(suggest --cwd "$PWD" --buffer "$1" --cycle "${ZSH_GIT_INLAY_CYCLE:-0}")
  [[ ${ZSH_GIT_INLAY_CYCLING:-0} == 1 ]] && arguments+=(--no-start)
  local result
  result="$(command zsh-git-inlay "${arguments[@]}" 2>/dev/null)" || return 0
  [[ $result == "$1"* ]] || return 0
  typeset -g suggestion="$result"
}

_zsh_git_inlay_activity_classify() {
  emulate -L zsh
  case "$1" in
    (git\ commit*|command\ git\ commit*) print -r -- git-commit ;;
    (git\ add*|git\ rm*|git\ restore\ --staged*|git\ reset*|command\ git\ add*|command\ git\ rm*|command\ git\ restore\ --staged*|command\ git\ reset*) print -r -- git-index ;;
    (go\ test*|go\ vet*|cargo\ test*|pytest*|python\ -m\ pytest*|make\ test*|npm\ test*|pnpm\ test*|yarn\ test*) print -r -- test ;;
    (go\ build*|cargo\ build*|make|make\ all*|npm\ run\ build*|pnpm\ run\ build*|yarn\ build*) print -r -- build ;;
  esac
}

_zsh_git_inlay_activity_emit() {
  emulate -L zsh
  local kind=$1 class=$2
  shift 2
  local -a arguments
  arguments=(activity emit --cwd "$PWD" --source shell --kind "$kind" --sensitivity private --data "class=$class")
  local value
  for value in "$@"; do
    arguments+=(--data "$value")
  done
  { command zsh-git-inlay "${arguments[@]}" >/dev/null 2>&1 &! }
}

_zsh_git_inlay_activity_preexec() {
  emulate -L zsh
  local class
  class="$(_zsh_git_inlay_activity_classify "$1")"
  [[ -n $class ]] || return 0
  typeset -g ZSH_GIT_INLAY_ACTIVITY_CLASS="$class"
  typeset -g ZSH_GIT_INLAY_ACTIVITY_STARTED="$SECONDS"
  _zsh_git_inlay_activity_emit shell.command_started "$class"
  if [[ $class == git-commit ]]; then
    _zsh_git_inlay_learning_prepare
  fi
  return 0
}

_zsh_git_inlay_learning_prepare() {
  { command zsh-git-inlay learning prepare --cwd "$PWD" >/dev/null 2>&1 &! }
}

_zsh_git_inlay_learning_commit() {
  [[ ${ZSH_GIT_INLAY_LEARNING_COMMIT:-0} == 1 ]] || return 0
  unset ZSH_GIT_INLAY_LEARNING_COMMIT
  { command zsh-git-inlay learning commit --cwd "$PWD" >/dev/null 2>&1 &! }
}

_zsh_git_inlay_activity_finish() {
  emulate -L zsh
  local exit_status=$1 class=${ZSH_GIT_INLAY_ACTIVITY_CLASS:-}
  [[ -n $class && -n ${ZSH_GIT_INLAY_ACTIVITY_STARTED:-} ]] || return 0
  integer duration_ms
  (( duration_ms = (SECONDS - ZSH_GIT_INLAY_ACTIVITY_STARTED) * 1000 ))
  (( duration_ms < 0 )) && duration_ms=0
  _zsh_git_inlay_activity_emit shell.command_finished "$class" "status=$exit_status" "duration_ms=$duration_ms"
  if (( exit_status == 0 )) && [[ $class == git-commit ]]; then
    typeset -g ZSH_GIT_INLAY_ACTIVITY_GIT_COMMIT=1
    typeset -g ZSH_GIT_INLAY_LEARNING_COMMIT=1
  fi
  case "$class" in
    (test) _zsh_git_inlay_activity_emit test.completed "$class" "status=$exit_status" "duration_ms=$duration_ms" ;;
    (build) _zsh_git_inlay_activity_emit build.completed "$class" "status=$exit_status" "duration_ms=$duration_ms" ;;
  esac
  unset ZSH_GIT_INLAY_ACTIVITY_CLASS ZSH_GIT_INLAY_ACTIVITY_STARTED
}

_zsh_git_inlay_activity_git_state() {
  local -a arguments
  arguments=(activity git-state --cwd "$PWD")
  if [[ ${ZSH_GIT_INLAY_ACTIVITY_GIT_COMMIT:-0} == 1 ]]; then
    arguments+=(--commit)
  fi
  unset ZSH_GIT_INLAY_ACTIVITY_GIT_COMMIT
  { command zsh-git-inlay "${arguments[@]}" >/dev/null 2>&1 &! }
}

_zsh_git_inlay_observe() {
  local exit_status=$?
  _zsh_git_inlay_activity_finish "$exit_status"
  _zsh_git_inlay_activity_git_state
  _zsh_git_inlay_learning_commit
  typeset -g ZSH_GIT_INLAY_CYCLE=0
  { command zsh-git-inlay observe --cwd "$PWD" >/dev/null 2>&1 &! }
}

_zsh_git_inlay_commit_context() {
  [[ $BUFFER == git\ commit\ * || $BUFFER == command\ git\ commit\ * ]] || return 1
  [[ $BUFFER != *'--amend'* && $BUFFER != *'--fixup'* && $BUFFER != *'--squash'* ]]
}

_zsh_git_inlay_cycle_widget() {
  emulate -L zsh
  _zsh_git_inlay_commit_context || return 0
  typeset -g ZSH_GIT_INLAY_CYCLE=$(( ${ZSH_GIT_INLAY_CYCLE:-0} + 1 ))
  typeset -g ZSH_GIT_INLAY_CYCLING=1
  _zsh_autosuggest_fetch
  unset ZSH_GIT_INLAY_CYCLING
}

_zsh_git_inlay_bind_cycle() {
  [[ ${ZSH_GIT_INLAY_CYCLE_KEYBINDING:-} == 'none' ]] && return 0
  [[ -n ${ZSH_GIT_INLAY_CYCLE_KEYBINDING:-} ]] || return 0
  local binding
  binding="$(bindkey -M emacs "$ZSH_GIT_INLAY_CYCLE_KEYBINDING" 2>/dev/null)"
  typeset -g _ZSH_GIT_INLAY_CYCLE_KEY="$ZSH_GIT_INLAY_CYCLE_KEYBINDING"
  typeset -g _ZSH_GIT_INLAY_CYCLE_ORIGINAL_WIDGET="${binding##* }"
  zle -N zsh-git-inlay-cycle _zsh_git_inlay_cycle_widget
  bindkey -M emacs "$ZSH_GIT_INLAY_CYCLE_KEYBINDING" zsh-git-inlay-cycle
}

zsh_git_inlay_unload() {
  emulate -L zsh
  autoload -Uz add-zsh-hook
  add-zsh-hook -d precmd _zsh_git_inlay_observe
  add-zsh-hook -d preexec _zsh_git_inlay_activity_preexec
  ZSH_AUTOSUGGEST_STRATEGY=(${ZSH_AUTOSUGGEST_STRATEGY:#git-inlay})
  if [[ -n ${_ZSH_GIT_INLAY_CYCLE_KEY:-} && -n ${_ZSH_GIT_INLAY_CYCLE_ORIGINAL_WIDGET:-} ]]; then
    bindkey -M emacs "$_ZSH_GIT_INLAY_CYCLE_KEY" "$_ZSH_GIT_INLAY_CYCLE_ORIGINAL_WIDGET"
  fi
  zle -D zsh-git-inlay-cycle 2>/dev/null
  unset ZSH_GIT_INLAY_ACTIVITY_CLASS ZSH_GIT_INLAY_ACTIVITY_STARTED ZSH_GIT_INLAY_ACTIVITY_GIT_COMMIT ZSH_GIT_INLAY_LEARNING_COMMIT
  unfunction _zsh_autosuggest_strategy_git-inlay _zsh_git_inlay_activity_classify _zsh_git_inlay_activity_emit _zsh_git_inlay_activity_preexec _zsh_git_inlay_learning_prepare _zsh_git_inlay_learning_commit _zsh_git_inlay_activity_finish _zsh_git_inlay_activity_git_state _zsh_git_inlay_observe _zsh_git_inlay_commit_context _zsh_git_inlay_cycle_widget _zsh_git_inlay_bind_cycle
}

ZSH_AUTOSUGGEST_STRATEGY=(git-inlay "${ZSH_AUTOSUGGEST_STRATEGY[@]}")
autoload -Uz add-zsh-hook
add-zsh-hook precmd _zsh_git_inlay_observe
add-zsh-hook preexec _zsh_git_inlay_activity_preexec
_zsh_git_inlay_bind_cycle
