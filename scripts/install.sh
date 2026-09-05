#!/bin/sh

set -eu

usage() {
	printf '%s\n' 'usage: scripts/install.sh --source <checkout> [--prefix <directory>]'
}

source_dir=
prefix="${HOME}/.local"
while [ "$#" -gt 0 ]; do
	case "$1" in
		--source)
			[ "$#" -ge 2 ] || { usage >&2; exit 2; }
			source_dir=$2
			shift 2
			;;
		--prefix)
			[ "$#" -ge 2 ] || { usage >&2; exit 2; }
			prefix=$2
			shift 2
			;;
		--help)
			usage
			exit 0
			;;
		*)
			usage >&2
			exit 2
			;;
	esac
done

[ -n "$source_dir" ] || { usage >&2; exit 2; }
case "$prefix" in
	""|/)
		printf '%s\n' 'refusing an empty or root install prefix' >&2
		exit 2
		;;
esac
source_dir=$(CDPATH= cd -- "$source_dir" && pwd -P)
binary="$source_dir/.build/zsh-git-inlay"
plugin="$source_dir/zsh-git-inlay.plugin.zsh"
[ -x "$binary" ] || { printf '%s\n' "build $binary first with make build" >&2; exit 1; }
[ -f "$plugin" ] || { printf '%s\n' "plugin source is missing from $source_dir" >&2; exit 1; }

bin_dir="$prefix/bin"
plugin_dir="$prefix/share/zsh-git-inlay"
mkdir -p "$bin_dir" "$plugin_dir"
tmp_binary=$(mktemp "$bin_dir/.zsh-git-inlay.XXXXXX")
tmp_plugin=$(mktemp "$plugin_dir/.zsh-git-inlay.plugin.zsh.XXXXXX")
cleanup() {
	[ -z "$tmp_binary" ] || rm -f -- "$tmp_binary"
	[ -z "$tmp_plugin" ] || rm -f -- "$tmp_plugin"
}
trap cleanup EXIT HUP INT TERM
install -m 755 "$binary" "$tmp_binary"
install -m 644 "$plugin" "$tmp_plugin"
mv -f "$tmp_binary" "$bin_dir/zsh-git-inlay"
mv -f "$tmp_plugin" "$plugin_dir/zsh-git-inlay.plugin.zsh"
tmp_binary=
tmp_plugin=

printf '%s\n' "installed $bin_dir/zsh-git-inlay"
printf '%s\n' 'add this after zsh-autosuggestions in .zshrc:'
printf '  path=("%s/bin" $path)\n' "$prefix"
printf '  source "%s/share/zsh-git-inlay/zsh-git-inlay.plugin.zsh"\n' "$prefix"
