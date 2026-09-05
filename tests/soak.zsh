#!/usr/bin/env zsh

emulate -L zsh
setopt errexit nounset pipefail

[[ -n ${ZSH_GIT_INLAY_BIN:-} && -x $ZSH_GIT_INLAY_BIN ]] || { print -u2 -- 'ZSH_GIT_INLAY_BIN must name a built executable'; exit 1 }

root=$(mktemp -d)
trap 'command "$ZSH_GIT_INLAY_BIN" daemon stop >/dev/null 2>&1 || true; command rm -rf -- "$root"' EXIT
export ZSH_GIT_INLAY_RUNTIME_DIR="$root/runtime"
export ZSH_GIT_INLAY_CACHE_DIR="$root/cache"
export ZSH_GIT_INLAY_DATA_DIR="$root/data"

repo_a="$root/repository-a"
repo_b="$root/repository-b"
for repo in "$repo_a" "$repo_b"; do
  command git -C "$root" init -q -b main "${repo:t}"
  print -r -- initial > "$repo/file.txt"
  command git -C "$repo" add file.txt
done

wait_ready() {
  local repo=$1 attempts=0
  until command "$ZSH_GIT_INLAY_BIN" candidates --cwd "$repo" >/dev/null 2>&1; do
    (( attempts++ < 150 )) || { print -u2 -- "candidate preparation timed out for $repo"; return 1 }
    sleep 0.02
  done
}

command "$ZSH_GIT_INLAY_BIN" observe --cwd "$repo_a" >/dev/null
wait_ready "$repo_a"
command "$ZSH_GIT_INLAY_BIN" observe --cwd "$repo_b" >/dev/null
wait_ready "$repo_b"

typeset -a workers
for worker in {1..3}; do
  zsh -dfc '
    emulate -L zsh
    setopt errexit nounset pipefail
    binary=$1
    first=$2
    second=$3
    for iteration in {1..20}; do
      command "$binary" observe --cwd "$first" >/dev/null
      command "$binary" suggest --no-start --cwd "$first" --buffer "git commit -m " >/dev/null
      command "$binary" observe --cwd "$second" >/dev/null
      command "$binary" suggest --no-start --cwd "$second" --buffer "git commit -m " >/dev/null
    done
  ' zsh "$ZSH_GIT_INLAY_BIN" "$repo_a" "$repo_b" &
  workers+=($!)
done

for iteration in {1..30}; do
  print -r -- "rapid staged state $iteration" > "$repo_a/file.txt"
  command git -C "$repo_a" add file.txt
  command "$ZSH_GIT_INLAY_BIN" observe --cwd "$repo_a" >/dev/null
done
for worker in $workers; do
  wait "$worker"
done

wait_ready "$repo_a"
wait_ready "$repo_b"
suggestion_a=$(command "$ZSH_GIT_INLAY_BIN" suggest --no-start --cwd "$repo_a" --buffer 'git commit -m ')
suggestion_b=$(command "$ZSH_GIT_INLAY_BIN" suggest --no-start --cwd "$repo_b" --buffer 'git commit -m ')
[[ -n $suggestion_a && -n $suggestion_b ]] || { print -u2 -- 'final staged states did not receive candidates'; exit 1 }
command git -C "$repo_a" diff --cached --quiet && { print -u2 -- 'rapid staged state was lost'; exit 1 }
command git -C "$repo_b" diff --cached --quiet && { print -u2 -- 'second repository staged state was lost'; exit 1 }

print -- 'multi-shell multi-repository soak: ok'
