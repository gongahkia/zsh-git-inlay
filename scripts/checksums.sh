#!/bin/sh

set -eu

[ "$#" -eq 1 ] && [ -d "$1" ] || {
	printf '%s\n' 'usage: scripts/checksums.sh <release-directory>' >&2
	exit 2
}

directory=$1
temporary=$(mktemp "$directory/.checksums.XXXXXX")
cleanup() {
	[ -z "$temporary" ] || rm -f -- "$temporary"
}
trap cleanup EXIT HUP INT TERM
(
	cd "$directory"
	set -- ./*.tar.gz
	[ -f "$1" ] || exit 1
	for archive in "$@"; do
		if command -v shasum >/dev/null 2>&1; then
			shasum -a 256 "$archive"
		else
			sha256sum "$archive"
		fi
	done
) > "$temporary"
chmod 644 "$temporary"
mv -f "$temporary" "$directory/checksums.txt"
temporary=
