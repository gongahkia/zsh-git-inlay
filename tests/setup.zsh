#!/usr/bin/env zsh
emulate -L zsh
setopt errexit nounset pipefail
project=${0:A:h:h}
root=$(mktemp -d)
trap 'command rm -rf -- "$root"' EXIT
prefix="$root/prefix with spaces"
command sh "$project/scripts/install.sh" --source "$project" --prefix "$prefix" >/dev/null
before=$(command git -C "$project" status --porcelain)
result=$(command "$prefix/bin/zsh-git-inlay" setup --check --demo --json)
[[ $result == *'"check_passed": true'* && $result == *'git commit -m '* ]] || {print -u2 -- "$result";exit 1}
[[ $(command git -C "$project" status --porcelain) == "$before" ]] || {print -u2 -- 'demo changed the current repository';exit 1}
print 'installed setup/demo: ok'
