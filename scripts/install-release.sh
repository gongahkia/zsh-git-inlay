#!/bin/sh

set -eu
umask 077

usage() {
	printf '%s\n' 'usage: scripts/install-release.sh --archive <release-archive> [--prefix <directory>]'
}

archive=
prefix="${HOME}/.local"
while [ "$#" -gt 0 ]; do
	case "$1" in
		--archive)
			[ "$#" -ge 2 ] || { usage >&2; exit 2; }
			archive=$2
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

[ -f "$archive" ] || { printf '%s\n' "release archive is unavailable: $archive" >&2; exit 1; }
case "$prefix" in
	""|/)
		printf '%s\n' 'refusing an empty or root install prefix' >&2
		exit 2
		;;
esac

expected_members='zsh-git-inlay
zsh-git-inlay.plugin.zsh
README.md
CHANGELOG.md'
members=$(tar -tzf "$archive")
[ "$members" = "$expected_members" ] || {
	printf '%s\n' 'release archive has unexpected members' >&2
	exit 1
}

stage=$(mktemp -d)
tmp_binary=
tmp_plugin=
cleanup() {
	rm -rf -- "$stage"
	[ -z "$tmp_binary" ] || rm -f -- "$tmp_binary"
	[ -z "$tmp_plugin" ] || rm -f -- "$tmp_plugin"
}
trap cleanup EXIT HUP INT TERM
for file in zsh-git-inlay zsh-git-inlay.plugin.zsh README.md CHANGELOG.md; do
	if ! tar -xOf "$archive" "$file" > "$stage/$file"; then
		printf '%s\n' "release archive cannot stream $file" >&2
		exit 1
	fi
done
[ -s "$stage/zsh-git-inlay" ] || { printf '%s\n' 'release binary is empty' >&2; exit 1; }

bin_dir="$prefix/bin"
plugin_dir="$prefix/share/zsh-git-inlay"
mkdir -p "$bin_dir" "$plugin_dir"
tmp_binary=$(mktemp "$bin_dir/.zsh-git-inlay.XXXXXX")
tmp_plugin=$(mktemp "$plugin_dir/.zsh-git-inlay.plugin.zsh.XXXXXX")
install -m 755 "$stage/zsh-git-inlay" "$tmp_binary"
install -m 644 "$stage/zsh-git-inlay.plugin.zsh" "$tmp_plugin"
mv -f "$tmp_binary" "$bin_dir/zsh-git-inlay"
mv -f "$tmp_plugin" "$plugin_dir/zsh-git-inlay.plugin.zsh"
tmp_binary=
tmp_plugin=

printf '%s\n' "installed release archive at $prefix"
printf '%s\n' 'add this after zsh-autosuggestions in .zshrc:'
printf '%s\n' "  path=(\"$prefix/bin\" \$path)"
printf '  source "%s/share/zsh-git-inlay/zsh-git-inlay.plugin.zsh"\n' "$prefix"
