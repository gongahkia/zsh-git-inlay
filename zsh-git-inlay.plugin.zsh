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

_zsh_git_inlay_observe() {
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
  ZSH_AUTOSUGGEST_STRATEGY=(${ZSH_AUTOSUGGEST_STRATEGY:#git-inlay})
  if [[ -n ${_ZSH_GIT_INLAY_CYCLE_KEY:-} && -n ${_ZSH_GIT_INLAY_CYCLE_ORIGINAL_WIDGET:-} ]]; then
    bindkey -M emacs "$_ZSH_GIT_INLAY_CYCLE_KEY" "$_ZSH_GIT_INLAY_CYCLE_ORIGINAL_WIDGET"
  fi
  zle -D zsh-git-inlay-cycle 2>/dev/null
  unfunction _zsh_autosuggest_strategy_git-inlay _zsh_git_inlay_observe _zsh_git_inlay_commit_context _zsh_git_inlay_cycle_widget _zsh_git_inlay_bind_cycle
}

ZSH_AUTOSUGGEST_STRATEGY=(git-inlay "${ZSH_AUTOSUGGEST_STRATEGY[@]}")
autoload -Uz add-zsh-hook
add-zsh-hook precmd _zsh_git_inlay_observe
_zsh_git_inlay_bind_cycle
