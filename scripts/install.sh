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
source_dir=$(
	CDPATH=''
	export CDPATH
	cd -- "$source_dir" && pwd -P
)
binary="$source_dir/.build/zsh-git-inlay"
plugin="$source_dir/zsh-git-inlay.plugin.zsh"
[ -x "$binary" ] || { printf '%s\n' "build $binary first with make build" >&2; exit 1; }
[ -f "$plugin" ] || { printf '%s\n' "plugin source is missing from $source_dir" >&2; exit 1; }

bin_dir="$prefix/bin"
plugin_dir="$prefix/share/zsh-git-inlay"
mkdir -p "$bin_dir" "$plugin_dir"
tmp_binary=$(mktemp "$bin_dir/.zsh-git-inlay.XXXXXX")
tmp_plugin=$(mktemp "$plugin_dir/.zsh-git-inlay.plugin.zsh.XXXXXX")
tmp_license=
tmp_notices=
cleanup() {
	[ -z "$tmp_binary" ] || rm -f -- "$tmp_binary"
	[ -z "$tmp_plugin" ] || rm -f -- "$tmp_plugin"
	[ -z "$tmp_license" ] || rm -f -- "$tmp_license"
	[ -z "$tmp_notices" ] || rm -f -- "$tmp_notices"
}
trap cleanup EXIT HUP INT TERM
install -m 755 "$binary" "$tmp_binary"
install -m 644 "$plugin" "$tmp_plugin"
if [ -f "$source_dir/LICENSE" ]; then
    tmp_license=$(mktemp "$plugin_dir/.LICENSE.XXXXXX")
    install -m 644 "$source_dir/LICENSE" "$tmp_license"
fi
if [ -f "$source_dir/THIRD_PARTY_NOTICES" ]; then
    tmp_notices=$(mktemp "$plugin_dir/.THIRD_PARTY_NOTICES.XXXXXX")
    install -m 644 "$source_dir/THIRD_PARTY_NOTICES" "$tmp_notices"
fi
mv -f "$tmp_binary" "$bin_dir/zsh-git-inlay"
mv -f "$tmp_plugin" "$plugin_dir/zsh-git-inlay.plugin.zsh"
tmp_binary=
tmp_plugin=
if [ -n "$tmp_license" ]; then
    mv -f "$tmp_license" "$plugin_dir/LICENSE"
    tmp_license=
fi
if [ -n "$tmp_notices" ]; then
    mv -f "$tmp_notices" "$plugin_dir/THIRD_PARTY_NOTICES"
    tmp_notices=
fi

printf '%s\n' "installed $bin_dir/zsh-git-inlay"
printf '%s\n' 'add this after zsh-autosuggestions in .zshrc:'
printf '%s\n' "  path=(\"$prefix/bin\" \$path)"
printf '  source "%s/share/zsh-git-inlay/zsh-git-inlay.plugin.zsh"\n' "$prefix"
