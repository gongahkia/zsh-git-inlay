#!/bin/sh

set -eu

usage() {
	printf '%s\n' 'usage: scripts/release-snapshot.sh --source <checkout> --output <new-directory> --version <version> --commit <commit>'
}

source_dir=
output=
version=
commit=
while [ "$#" -gt 0 ]; do
	case "$1" in
		--source|--output|--version|--commit)
			[ "$#" -ge 2 ] || { usage >&2; exit 2; }
			case "$1" in
				--source) source_dir=$2 ;;
				--output) output=$2 ;;
				--version) version=$2 ;;
				--commit) commit=$2 ;;
			esac
			shift 2
			;;
		*) usage >&2; exit 2 ;;
	esac
done

[ -n "$source_dir" ] && [ -n "$output" ] && [ -n "$version" ] && [ -n "$commit" ] || { usage >&2; exit 2; }
case "$version" in
	*[!A-Za-z0-9._-]*|"") printf '%s\n' 'invalid release version' >&2; exit 2 ;;
esac
case "$commit" in
	*[!A-Za-z0-9._-]*|"") printf '%s\n' 'invalid release commit' >&2; exit 2 ;;
esac
[ ! -e "$output" ] || { printf '%s\n' "release output already exists: $output" >&2; exit 1; }
source_dir=$(
	CDPATH=''
	export CDPATH
	cd -- "$source_dir" && pwd -P
)
[ -f "$source_dir/go.mod" ] && [ -f "$source_dir/zsh-git-inlay.plugin.zsh" ] && [ -f "$source_dir/CHANGELOG.md" ] || {
	printf '%s\n' 'release source is incomplete' >&2
	exit 1
}
mkdir -p "$(dirname "$output")"
mkdir "$output"
script_dir=$(
	CDPATH=''
	export CDPATH
	cd -- "$(dirname "$0")" && pwd -P
)
ldflags="-s -w -buildid= -X main.version=$version -X main.commit=$commit"
targets='linux-amd64 linux-arm64 darwin-amd64 darwin-arm64'
cleanup_stage=
cleanup() {
	[ -z "$cleanup_stage" ] || rm -rf -- "$cleanup_stage"
}
trap cleanup EXIT HUP INT TERM

for target in $targets; do
	os=${target%-*}
	arch=${target#*-}
	stage="$output/.staging-$target"
	cleanup_stage=$stage
	mkdir "$stage"
	GOOS=$os GOARCH=$arch CGO_ENABLED=0 go -C "$source_dir" build -trimpath -buildvcs=false -ldflags "$ldflags" -o "$stage/zsh-git-inlay" ./cmd/zsh-git-inlay
	cp "$source_dir/zsh-git-inlay.plugin.zsh" "$source_dir/README.md" "$source_dir/CHANGELOG.md" "$stage/"
	chmod 755 "$stage/zsh-git-inlay"
	chmod 644 "$stage/zsh-git-inlay.plugin.zsh" "$stage/README.md" "$stage/CHANGELOG.md"
	touch -t 200001010000 "$stage/zsh-git-inlay" "$stage/zsh-git-inlay.plugin.zsh" "$stage/README.md" "$stage/CHANGELOG.md"
	archive="$output/zsh-git-inlay_${version}_${target}.tar.gz"
	tar -C "$stage" -czf "$archive" zsh-git-inlay zsh-git-inlay.plugin.zsh README.md CHANGELOG.md
	chmod 644 "$archive"
	rm -rf -- "$stage"
	cleanup_stage=
done
sh "$script_dir/checksums.sh" "$output"
sh "$script_dir/sbom.sh" --source "$source_dir" --version "$version" --output "$output/SBOM.spdx.json"
printf '%s\n' "release snapshot created at $output"
