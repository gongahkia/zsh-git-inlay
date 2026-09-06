#!/usr/bin/env zsh

emulate -L zsh
setopt errexit nounset pipefail extendedglob

[[ -n ${ZSH_GIT_INLAY_BIN:-} && -x $ZSH_GIT_INLAY_BIN ]] || { print -u2 -- 'ZSH_GIT_INLAY_BIN must name a built executable'; exit 1 }
model=${ZSH_GIT_INLAY_LIVE_OLLAMA_MODEL:-}
[[ -n $model && $model == [[:alnum:]_.:/-]## ]] || { print -u2 -- 'set ZSH_GIT_INLAY_LIVE_OLLAMA_MODEL to an installed Ollama model tag'; exit 1 }
(( $+commands[ollama] )) || { print -u2 -- 'Ollama CLI is unavailable; this opt-in test never installs it'; exit 1 }
command ollama list | command awk -v model="$model" 'NR > 1 && $1 == model { found = 1 } END { exit !found }' || { print -u2 -- "Ollama model $model is not installed; this opt-in test never pulls it"; exit 1 }

root=$(command mktemp -d)
daemon_pid=''
cleanup() {
  if [[ -n $daemon_pid ]]; then
    command "$ZSH_GIT_INLAY_BIN" daemon stop >/dev/null 2>&1 || true
    wait "$daemon_pid" 2>/dev/null || true
  fi
  command rm -rf -- "$root"
}
trap cleanup EXIT HUP INT TERM

export ZSH_GIT_INLAY_RUNTIME_DIR="$root/runtime"
export ZSH_GIT_INLAY_CACHE_DIR="$root/cache"
export ZSH_GIT_INLAY_DATA_DIR="$root/data"
export ZSH_GIT_INLAY_CONFIG="$root/config.toml"

print -r -- "[provider]
name = \"ollama\"
model = \"$model\"
timeout = \"10s\"
fallback = \"none\"" > "$ZSH_GIT_INLAY_CONFIG"

repo="$root/repository"
command git -C "$root" init -q -b main repository
print -r -- 'live local-model fixture' > "$repo/message.txt"
command git -C "$repo" add message.txt

start_daemon() {
  command "$ZSH_GIT_INLAY_BIN" daemon serve --observe "$repo" >"$root/daemon.log" 2>&1 &
  daemon_pid=$!
}

wait_ready() {
  local expected_fingerprint=$1 attempt=0 record=''
  until record=$(command "$ZSH_GIT_INLAY_BIN" candidates --cwd "$repo" --json 2>/dev/null); do
    (( attempt++ < 120 )) || { print -u2 -- "model preparation did not finish within 12 seconds; daemon log: $(<"$root/daemon.log")"; return 1 }
    sleep 0.1
  done
  print -r -- "$record" | command grep -Fq -- "\"fingerprint\": \"$expected_fingerprint\"" || { print -u2 -- 'candidate lookup did not return the current exact fingerprint'; return 1 }
  print -r -- "$record" | command grep -Fq -- '"name": "ollama"' || { print -u2 -- 'candidate record did not come from Ollama'; return 1 }
  print -r -- "$record" | command grep -Fq -- "\"model\": \"$model\"" || { print -u2 -- 'candidate record did not use the requested model'; return 1 }
}

start_daemon
first_fingerprint=$(command "$ZSH_GIT_INLAY_BIN" fingerprint --cwd "$repo")
wait_ready "$first_fingerprint"

suggestion=$(command "$ZSH_GIT_INLAY_BIN" suggest --no-start --cwd "$repo" --buffer 'git commit -m ' --json)
print -r -- "$suggestion" | command grep -Fq -- '"status": "ready"' || { print -u2 -- 'prepared local-model candidate was not available to the normal lookup command'; exit 1 }

explanation=$(command "$ZSH_GIT_INLAY_BIN" explain --cwd "$repo" --json)
[[ $explanation == *'"state": "GROUNDED"'* || $explanation == *'"state": "PARTIALLY_GROUNDED"'* ]] || { print -u2 -- 'prepared local-model candidate has no grounded explanation'; exit 1 }
[[ $explanation != *'"state": "UNGROUNDED"'* ]] || { print -u2 -- 'ungrounded local-model candidate reached the prepared record'; exit 1 }

print -r -- 'superseding staged fixture' >> "$repo/message.txt"
command git -C "$repo" add message.txt
second_fingerprint=$(command "$ZSH_GIT_INLAY_BIN" fingerprint --cwd "$repo")
[[ $second_fingerprint != "$first_fingerprint" ]] || { print -u2 -- 'staged-state change did not alter the exact fingerprint'; exit 1 }
command "$ZSH_GIT_INLAY_BIN" observe --cwd "$repo" >/dev/null
wait_ready "$second_fingerprint"

kill -KILL "$daemon_pid"
wait "$daemon_pid" 2>/dev/null || true
daemon_pid=''
after_crash=$(command "$ZSH_GIT_INLAY_BIN" suggest --no-start --cwd "$repo" --buffer 'git commit -m ' --json)
print -r -- "$after_crash" | command grep -Fq -- '"status": "daemon_unavailable"' || { print -u2 -- 'killed daemon still returned a lookup result'; exit 1 }

start_daemon
command "$ZSH_GIT_INLAY_BIN" observe --cwd "$repo" >/dev/null
wait_ready "$second_fingerprint"

print -- 'live Ollama daemon/socket rehearsal: ok'
