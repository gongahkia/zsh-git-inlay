#!/bin/sh

set -eu

usage() {
	printf '%s\n' 'usage: scripts/sbom.sh --source <checkout> --version <version> --output <path>'
}

source_dir=
version=
output=
while [ "$#" -gt 0 ]; do
	case "$1" in
		--source|--version|--output)
			[ "$#" -ge 2 ] || { usage >&2; exit 2; }
			case "$1" in
				--source) source_dir=$2 ;;
				--version) version=$2 ;;
				--output) output=$2 ;;
			esac
			shift 2
			;;
		*) usage >&2; exit 2 ;;
	esac
done

[ -n "$source_dir" ] && [ -n "$version" ] && [ -n "$output" ] || { usage >&2; exit 2; }
case "$version" in
	*[!A-Za-z0-9._-]*|"") printf '%s\n' 'invalid release version' >&2; exit 2 ;;
esac
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd -P)
python3 "$script_dir/sbom.py" --source "$source_dir" --version "$version" --output "$output"
