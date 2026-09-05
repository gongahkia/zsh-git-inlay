#!/usr/bin/env zsh

emulate -L zsh
setopt errexit nounset pipefail

[[ -n ${ZSH_GIT_INLAY_BIN:-} && -x $ZSH_GIT_INLAY_BIN ]] || { print -u2 -- 'ZSH_GIT_INLAY_BIN must name a built executable'; exit 1 }

project=${0:A:h:h}
autosuggestions=${ZSH_AUTOSUGGESTIONS_SOURCE:-/usr/share/zsh-autosuggestions/zsh-autosuggestions.zsh}
[[ -r $autosuggestions ]] || { print -u2 -- "zsh-autosuggestions source unavailable: $autosuggestions"; exit 1 }

root=$(mktemp -d)
trap 'command "$ZSH_GIT_INLAY_BIN" daemon stop >/dev/null 2>&1 || true; command rm -rf -- "$root"' EXIT
export ZSH_GIT_INLAY_RUNTIME_DIR="$root/runtime"
export ZSH_GIT_INLAY_CACHE_DIR="$root/cache"
export ZSH_GIT_INLAY_DATA_DIR="$root/data"
prefix="$root/installed plugin"
repo="$root/repository"

command git -C "$root" init -q -b main repository
print -r -- staged > "$repo/file.txt"
command git -C "$repo" add file.txt
command sh "$project/scripts/install.sh" --source "$project" --prefix "$prefix" >/dev/null

wait_ready() {
  local attempts=0
  until command "$prefix/bin/zsh-git-inlay" candidates --cwd "$repo" >/dev/null 2>&1; do
    (( attempts++ < 150 )) || { print -u2 -- 'candidate preparation timed out'; return 1 }
    sleep 0.02
  done
}

command "$prefix/bin/zsh-git-inlay" observe --cwd "$repo" >/dev/null
wait_ready

run_session() {
  zsh -dfc '
    emulate -L zsh
    setopt errexit nounset pipefail
    autosuggestions=$1
    prefix=$2
    repo=$3
    path=("$prefix/bin" $path)
    source "$autosuggestions"
    typeset -ga ZSH_AUTOSUGGEST_STRATEGY=(history)
    source "$prefix/share/zsh-git-inlay/zsh-git-inlay.plugin.zsh"
    [[ ${ZSH_AUTOSUGGEST_STRATEGY[1]} == git-inlay && ${ZSH_AUTOSUGGEST_STRATEGY[2]} == history ]] || exit 1
    cd "$repo"
    base="git commit -m "
    suggestion=""
    _zsh_autosuggest_strategy_git-inlay "$base"
    primary=$suggestion
    [[ -n $primary && $primary == "$base"* ]] || exit 1
    partial=${primary[1,$(( ${#base} + 7 ))]}
    suggestion=""
    _zsh_autosuggest_strategy_git-inlay "$partial"
    [[ $suggestion == "$primary" ]] || exit 1
    BUFFER=$base
    CURSOR=${#BUFFER}
    POSTDISPLAY="${primary[${#BUFFER}+1,-1]}"
    KEYMAP=main
    _zsh_autosuggest_accept
    [[ $BUFFER == "$primary" ]] || exit 1
    ZSH_GIT_INLAY_CYCLE=1
    suggestion=""
    _zsh_autosuggest_strategy_git-inlay "$base"
    [[ -n $suggestion && $suggestion != "$primary" ]] || exit 1
    zsh_git_inlay_unload
    [[ ${ZSH_AUTOSUGGEST_STRATEGY[(Ie)git-inlay]} == 0 ]] || exit 1
  ' zsh "$autosuggestions" "$prefix" "$repo"
}

run_session
run_session

zmodload zsh/zpty || { print -u2 -- 'zsh/zpty is unavailable'; exit 1 }
zpty -be dogfood-pty zsh -dfi

pty_read_until() {
  local marker=$1 line attempts=0
  REPLY=''
  while (( attempts++ < 100 )); do
    if zpty -r -t dogfood-pty line; then
      REPLY+=$line
      [[ $REPLY == *"$marker"* ]] && return 0
    else
      sleep 0.02
    fi
  done
  print -u2 -- "PTY output did not contain $marker: $REPLY"
  return 1
}

zpty -w dogfood-pty 'trap - EXIT'
zpty -w dogfood-pty 'PS1="zsh-git-inlay-pty> "'
zpty -w dogfood-pty "path=(\"$prefix/bin\" \$path)"
zpty -w dogfood-pty "source \"$autosuggestions\""
zpty -w dogfood-pty 'typeset -ga ZSH_AUTOSUGGEST_STRATEGY=(history)'
zpty -w dogfood-pty "source \"$prefix/share/zsh-git-inlay/zsh-git-inlay.plugin.zsh\""
zpty -w dogfood-pty "cd \"$repo\""
zpty -w dogfood-pty 'marker=__PTY_; marker+=READY__; print -r -- "$marker"'
pty_read_until '__PTY_READY__'
while zpty -r -t dogfood-pty _; do :; done
zpty -w -n dogfood-pty 'git commit -m '
sleep 0.2
pty_read_until $'\e[90m'
[[ $REPLY == *'git commit -m '* ]] || { print -u2 -- 'PTY session did not receive the commit buffer'; exit 1 }

print -r -- updated > "$repo/second.txt"
command git -C "$repo" add second.txt
stale=$(command "$prefix/bin/zsh-git-inlay" suggest --no-start --cwd "$repo" --buffer 'git commit -m ')
[[ -z $stale ]] || { print -u2 -- 'stale candidate appeared after staging change'; exit 1 }
command "$prefix/bin/zsh-git-inlay" observe --cwd "$repo" >/dev/null
wait_ready
run_session

command sh "$project/scripts/uninstall.sh" --prefix "$prefix" >/dev/null
[[ ! -e "$prefix/bin/zsh-git-inlay" && ! -e "$prefix/share/zsh-git-inlay/zsh-git-inlay.plugin.zsh" ]] || {
  print -u2 -- 'clean uninstall left installed dogfood files'
  exit 1
}

print -- 'installed Zsh dogfood: ok'
