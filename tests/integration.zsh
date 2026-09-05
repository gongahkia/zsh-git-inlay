#!/usr/bin/env zsh

emulate -L zsh
setopt errexit nounset pipefail

[[ -n ${ZSH_GIT_INLAY_BIN:-} && -x $ZSH_GIT_INLAY_BIN ]] || { print -u2 -- 'ZSH_GIT_INLAY_BIN must name a built executable'; exit 1 }
export PATH="${ZSH_GIT_INLAY_BIN:h}:$PATH"
autosuggestions=${ZSH_AUTOSUGGESTIONS_SOURCE:-/usr/share/zsh-autosuggestions/zsh-autosuggestions.zsh}
[[ -r $autosuggestions ]] || { print -u2 -- "zsh-autosuggestions source unavailable: $autosuggestions"; exit 1 }

root=$(mktemp -d)
project_dir="${0:A:h:h}"
trap 'command "$ZSH_GIT_INLAY_BIN" daemon stop >/dev/null 2>&1 || true; command rm -rf -- "$root"' EXIT
export ZSH_GIT_INLAY_RUNTIME_DIR="$root/runtime"
export ZSH_GIT_INLAY_CACHE_DIR="$root/cache"

repo="$root/repository"
command git -C "$root" init -q -b main repository
print -r -- 'prototype' > "$repo/cache.txt"
command git -C "$repo" add cache.txt
[[ ! -e "$ZSH_GIT_INLAY_RUNTIME_DIR/daemon.sock" ]] || { print -u2 -- 'daemon was not lazy'; exit 1 }

wait_ready() {
  local attempts=0
  until command "$ZSH_GIT_INLAY_BIN" candidates --cwd "$repo" >/dev/null 2>&1; do
    (( attempts++ < 100 )) || {
      print -u2 -- 'candidate preparation timed out'
      command "$ZSH_GIT_INLAY_BIN" status --json >&2 || true
      command "$ZSH_GIT_INLAY_BIN" candidates --cwd "$repo" --json >&2 || true
      return 1
    }
    sleep 0.02
  done
}

command "$ZSH_GIT_INLAY_BIN" observe --cwd "$repo" >/dev/null
wait_ready
[[ -S "$ZSH_GIT_INLAY_RUNTIME_DIR/daemon.sock" && "$(command stat -c '%a' "$ZSH_GIT_INLAY_RUNTIME_DIR/daemon.sock")" == 600 ]] || { print -u2 -- 'daemon socket was not private'; exit 1 }

source "$autosuggestions"
typeset -ga ZSH_AUTOSUGGEST_STRATEGY=(history)
source "$project_dir/zsh-git-inlay.plugin.zsh"

[[ ${ZSH_AUTOSUGGEST_STRATEGY[1]} == git-inlay && ${ZSH_AUTOSUGGEST_STRATEGY[2]} == history ]] || { print -u2 -- 'strategy registration replaced existing strategy'; exit 1 }
(( ! ${+functions[git]} )) || { print -u2 -- 'plugin defined a git function'; exit 1 }

cd "$repo"
base='git commit -m '
suggestion=''
_zsh_autosuggest_strategy_git-inlay "$base"
primary=$suggestion
[[ $primary == "$base"* && -n $primary ]] || { print -u2 -- 'fresh candidate did not appear through strategy contract'; exit 1 }

BUFFER=$base
CURSOR=${#BUFFER}
POSTDISPLAY="${primary[${#BUFFER}+1,-1]}"
KEYMAP=main
_zsh_autosuggest_accept
[[ $BUFFER == "$primary" && $CURSOR == ${#BUFFER} ]] || { print -u2 -- 'standard autosuggestion acceptance did not transfer candidate'; exit 1 }

ZSH_GIT_INLAY_CYCLE=1
suggestion=''
_zsh_autosuggest_strategy_git-inlay "$base"
[[ -n $suggestion && $suggestion != "$primary" ]] || { print -u2 -- 'cycle selection did not change candidate'; exit 1 }

suggestion=''
_zsh_autosuggest_strategy_git-inlay 'echo unaffected'
[[ -z $suggestion ]] || { print -u2 -- 'unrelated command received Git Inlay suggestion'; exit 1 }
diagnostic=$(command "$ZSH_GIT_INLAY_BIN" suggest --cwd "$repo" --buffer 'git merge' --json)
[[ $diagnostic == *'unsupported_command'* ]] || { print -u2 -- 'unsupported syntax diagnostic was unclear'; exit 1 }

print -r -- 'new staged state' > "$repo/extra.txt"
command git -C "$repo" add extra.txt
suggestion=''
_zsh_autosuggest_strategy_git-inlay "$base"
[[ -z $suggestion ]] || { print -u2 -- 'stale candidate appeared after index changed'; exit 1 }

command "$ZSH_GIT_INLAY_BIN" observe --cwd "$repo" >/dev/null
wait_ready
ZSH_GIT_INLAY_CYCLE=0
suggestion=''
_zsh_autosuggest_strategy_git-inlay "$base"
[[ -n $suggestion && $suggestion != "$primary" ]] || { print -u2 -- 'new staged state did not receive distinct candidate'; exit 1 }

command "$ZSH_GIT_INLAY_BIN" daemon stop
attempts=0
while [[ -e "$ZSH_GIT_INLAY_RUNTIME_DIR/daemon.sock" ]]; do
  (( attempts++ < 100 )) || { print -u2 -- 'daemon did not stop'; exit 1 }
  sleep 0.02
done
command "$ZSH_GIT_INLAY_BIN" observe --cwd "$repo" >/dev/null
wait_ready

command git -C "$repo" reset --mixed -q
suggestion=''
_zsh_autosuggest_strategy_git-inlay "$base"
[[ -z $suggestion ]] || { print -u2 -- 'no-staged state received a candidate'; exit 1 }

zsh_git_inlay_unload
[[ ${ZSH_AUTOSUGGEST_STRATEGY[(Ie)git-inlay]} == 0 ]] || { print -u2 -- 'unload left strategy registered'; exit 1 }

missing_result=$(zsh -dfc 'source "'$project_dir'/zsh-git-inlay.plugin.zsh"; print -r -- "$ZSH_GIT_INLAY_DEPENDENCY_ERROR"' 2>&1)
[[ $missing_result == *'requires zsh-autosuggestions'* ]] || { print -u2 -- 'missing dependency diagnostic was not actionable'; exit 1 }

print -- 'zsh integration: ok'
