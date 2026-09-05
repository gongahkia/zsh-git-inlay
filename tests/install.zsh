#!/usr/bin/env zsh

emulate -L zsh
setopt errexit nounset pipefail

[[ $# == 1 ]] || { print -u2 -- 'usage: tests/install.zsh <project-root>'; exit 2 }
project=${1:A}
[[ -x $project/.build/zsh-git-inlay ]] || { print -u2 -- 'build the project before running install tests'; exit 1 }

root=$(mktemp -d)
trap 'command rm -rf -- "$root"' EXIT
prefix="$root/prefix"

command sh "$project/scripts/install.sh" --source "$project" --prefix "$prefix"
[[ -x "$prefix/bin/zsh-git-inlay" && -r "$prefix/share/zsh-git-inlay/zsh-git-inlay.plugin.zsh" ]] || {
  print -u2 -- 'install did not create the expected files'
  exit 1
}
version=$(command "$prefix/bin/zsh-git-inlay" version --json)
[[ $version == *'"version"'* && $version == *'"commit"'* ]] || { print -u2 -- 'installed binary lacks build metadata'; exit 1 }
if command sh "$project/scripts/install.sh" --source "$project" --prefix / >/dev/null 2>&1; then
  print -u2 -- 'installer accepted the root prefix'
  exit 1
fi

command sh "$project/scripts/install.sh" --source "$project" --prefix "$prefix"
command cmp "$project/.build/zsh-git-inlay" "$prefix/bin/zsh-git-inlay"

first="$root/release-one"
second="$root/release-two"
for output in "$first" "$second"; do
  command sh "$project/scripts/release-snapshot.sh" --source "$project" --output "$output" --version test-0.1.0 --commit 0123456789abcdef
  [[ -r "$output/checksums.txt" && -r "$output/SBOM.spdx.json" ]] || { print -u2 -- 'release metadata is missing'; exit 1 }
  (cd "$output" && command shasum -a 256 -c checksums.txt >/dev/null)
  [[ "$(command grep -c 'SPDX-2.3' "$output/SBOM.spdx.json")" == 1 ]] || { print -u2 -- 'release SBOM is malformed'; exit 1 }
  for target in linux-amd64 linux-arm64 darwin-amd64 darwin-arm64; do
    archive="$output/zsh-git-inlay_test-0.1.0_$target.tar.gz"
    [[ -f $archive ]] || { print -u2 -- "missing $target release archive"; exit 1 }
    members=("${(@f)$(command tar -tzf "$archive")}")
    (( ${members[(Ie)zsh-git-inlay]} )) || { print -u2 -- "binary missing from $target archive"; exit 1 }
    (( ${members[(Ie)zsh-git-inlay.plugin.zsh]} )) || { print -u2 -- "plugin missing from $target archive"; exit 1 }
  done
done
for archive in "$first"/*.tar.gz; do
  command cmp "$archive" "$second/${archive:t}"
done
command cmp "$first/checksums.txt" "$second/checksums.txt"
command cmp "$first/SBOM.spdx.json" "$second/SBOM.spdx.json"
if command sh "$project/scripts/release-snapshot.sh" --source "$project" --output "$first" --version test-0.1.0 --commit 0123456789abcdef >/dev/null 2>&1; then
  print -u2 -- 'release snapshot overwrote an existing output'
  exit 1
fi
extract="$root/extract"
command mkdir "$extract"
command tar -xzf "$first/zsh-git-inlay_test-0.1.0_linux-amd64.tar.gz" -C "$extract"
packaged_version=$(command "$extract/zsh-git-inlay" version --json)
[[ $packaged_version == *'"version": "test-0.1.0"'* && $packaged_version == *'"commit": "0123456789abcdef"'* ]] || {
  print -u2 -- 'release binary lacks requested version metadata'
  exit 1
}

export XDG_CACHE_HOME="$root/cache"
export XDG_STATE_HOME="$root/state"
export XDG_DATA_HOME="$root/data"
export XDG_CONFIG_HOME="$root/config"
export XDG_RUNTIME_DIR="$root/runtime"
for base in "$XDG_CACHE_HOME" "$XDG_STATE_HOME" "$XDG_DATA_HOME" "$XDG_CONFIG_HOME" "$XDG_RUNTIME_DIR"; do
  command mkdir -p "$base/zsh-git-inlay"
  print -r -- keep > "$base/sentinel"
done
command sh "$project/scripts/uninstall.sh" --prefix "$prefix" --purge-local-data
[[ ! -e "$prefix/bin/zsh-git-inlay" && ! -e "$prefix/share/zsh-git-inlay/zsh-git-inlay.plugin.zsh" ]] || {
  print -u2 -- 'uninstall left installed files'
  exit 1
}
for base in "$XDG_CACHE_HOME" "$XDG_STATE_HOME" "$XDG_DATA_HOME" "$XDG_CONFIG_HOME" "$XDG_RUNTIME_DIR"; do
  [[ ! -e "$base/zsh-git-inlay" && -f "$base/sentinel" ]] || { print -u2 -- "purge escaped $base/zsh-git-inlay"; exit 1 }
done

print -- 'install and release snapshot: ok'
