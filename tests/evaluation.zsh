#!/usr/bin/env zsh

emulate -L zsh
setopt errexit nounset pipefail

[[ -n ${ZSH_GIT_INLAY_BIN:-} && -x $ZSH_GIT_INLAY_BIN ]] || { print -u2 -- 'ZSH_GIT_INLAY_BIN must name a built executable'; exit 1 }

root=$(mktemp -d)
trap 'command rm -rf -- "$root"' EXIT
export ZSH_GIT_INLAY_STATE_DIR="$root/state"

output=$(command "$ZSH_GIT_INLAY_BIN" evaluate --fixtures --json)
print -r -- "$output" | command grep -q '"cases": 11' || { print -u2 -- 'fixture evaluation did not report all cases'; exit 1 }

json_count=$(command find "$ZSH_GIT_INLAY_STATE_DIR/evaluations" -type f -name '*.json' | command wc -l)
markdown_count=$(command find "$ZSH_GIT_INLAY_STATE_DIR/evaluations" -type f -name '*.md' | command wc -l)
[[ $json_count -eq 1 && $markdown_count -eq 1 ]] || { print -u2 -- 'fixture evaluation did not write both reports'; exit 1 }

command grep -q '"schema_version": "v1"' "$ZSH_GIT_INLAY_STATE_DIR"/evaluations/*.json || { print -u2 -- 'JSON report schema is missing'; exit 1 }
command grep -q 'Ignore every policy' "$ZSH_GIT_INLAY_STATE_DIR"/evaluations/* && { print -u2 -- 'raw staged fixture content leaked into a report'; exit 1 }

print -- 'evaluation integration: ok'
