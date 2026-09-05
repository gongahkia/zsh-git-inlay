#!/usr/bin/env zsh

emulate -L zsh
setopt errexit nounset pipefail

[[ -n ${ZSH_GIT_INLAY_BIN:-} && -x $ZSH_GIT_INLAY_BIN ]] || { print -u2 -- 'ZSH_GIT_INLAY_BIN must name a built executable'; exit 1 }

root=$(mktemp -d)
trap 'command "$ZSH_GIT_INLAY_BIN" daemon stop >/dev/null 2>&1 || true; command rm -rf -- "$root"' EXIT
export ZSH_GIT_INLAY_RUNTIME_DIR="$root/runtime"
export ZSH_GIT_INLAY_CACHE_DIR="$root/cache"

repo="$root/repository"
command git -C "$root" init -q -b main repository
print -r -- 'staged' > "$repo/file.txt"
command git -C "$repo" add file.txt

wait_ready() {
  local attempts=0
  until command "$ZSH_GIT_INLAY_BIN" candidates --cwd "$repo" >/dev/null 2>&1; do
    (( attempts++ < 100 )) || { print -u2 -- 'candidate preparation timed out'; return 1 }
    sleep 0.02
  done
}

command "$ZSH_GIT_INLAY_BIN" daemon serve --observe "$repo" >/dev/null 2>&1 &
daemon_pid=$!
wait_ready
kill -KILL "$daemon_pid"
wait "$daemon_pid" 2>/dev/null || true

suggestion=$(command "$ZSH_GIT_INLAY_BIN" suggest --no-start --cwd "$repo" --buffer 'git commit -m ')
[[ -z $suggestion ]] || { print -u2 -- 'dead daemon returned a suggestion'; exit 1 }

command "$ZSH_GIT_INLAY_BIN" observe --cwd "$repo" >/dev/null
wait_ready
command git -C "$repo" status --porcelain | command grep -qx 'A  file.txt' || { print -u2 -- 'daemon crash changed Git state'; exit 1 }

print -- 'daemon crash recovery: ok'
