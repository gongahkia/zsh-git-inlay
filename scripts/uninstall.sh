#!/bin/sh

set -eu

usage() {
	printf '%s\n' 'usage: scripts/uninstall.sh [--prefix <directory>] [--purge-local-data]'
}

prefix="${HOME}/.local"
purge=false
while [ "$#" -gt 0 ]; do
	case "$1" in
		--prefix)
			[ "$#" -ge 2 ] || { usage >&2; exit 2; }
			prefix=$2
			shift 2
			;;
		--purge-local-data)
			purge=true
			shift
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

case "$prefix" in
	""|/)
		printf '%s\n' 'refusing an empty or root install prefix' >&2
		exit 2
		;;
esac
binary="$prefix/bin/zsh-git-inlay"
plugin="$prefix/share/zsh-git-inlay/zsh-git-inlay.plugin.zsh"
if [ -x "$binary" ]; then
	"$binary" daemon stop >/dev/null 2>&1 || true
fi
rm -f -- "$binary" "$plugin"
rmdir "$prefix/share/zsh-git-inlay" 2>/dev/null || true
rmdir "$prefix/share" 2>/dev/null || true
rmdir "$prefix/bin" 2>/dev/null || true

remove_product_directory() {
	directory=$1
	case "$directory" in
		*/zsh-git-inlay) ;;
		*)
			printf '%s\n' "refusing unexpected data path: $directory" >&2
			return 1
			;;
	esac
	[ -e "$directory" ] && rm -rf -- "$directory"
}

if [ "$purge" = true ]; then
	home=${HOME:?HOME is required to purge local data}
	remove_product_directory "${XDG_CACHE_HOME:-$home/.cache}/zsh-git-inlay"
	remove_product_directory "${XDG_STATE_HOME:-$home/.local/state}/zsh-git-inlay"
	remove_product_directory "${XDG_DATA_HOME:-$home/.local/share}/zsh-git-inlay"
	remove_product_directory "${XDG_CONFIG_HOME:-$home/.config}/zsh-git-inlay"
	if [ -n "${XDG_RUNTIME_DIR:-}" ]; then
		remove_product_directory "$XDG_RUNTIME_DIR/zsh-git-inlay"
	fi
	printf '%s\n' 'removed zsh-git-inlay local data after explicit purge request'
fi
printf '%s\n' 'removed installed zsh-git-inlay files; remove its source line from .zshrc'
