#!/bin/sh
set -eu
umask 077
version=
expected=
prefix="${HOME}/.local"
while [ "$#" -gt 0 ]; do
 case "$1" in
  --version|--sha256|--prefix)
   [ "$#" -ge 2 ] || exit 2
   case "$1" in --version) version=$2;; --sha256) expected=$2;; --prefix) prefix=$2;; esac
   shift 2;;
  *) printf '%s\n' 'usage: install-binary.sh --version vX.Y.Z --sha256 <reviewed archive checksum> [--prefix directory]' >&2; exit 2;;
 esac
done
# Pin an explicit version and an independently reviewed digest before download.
case "$version" in v[0-9]*.[0-9]*.[0-9]*) ;; *) printf '%s\n' 'an explicit vX.Y.Z version is required' >&2; exit 2;; esac
case "$version" in *[!A-Za-z0-9._-]*) exit 2;; esac
[ "${#expected}" -eq 64 ] || { printf '%s\n' 'a reviewed SHA-256 checksum is required' >&2; exit 2; }
case "$expected" in *[!a-f0-9]*) exit 2;; esac
case "$(uname -s)" in Darwin) target_os=darwin;; Linux) target_os=linux;; *) printf '%s\n' 'unsupported operating system' >&2; exit 2;; esac
case "$(uname -m)" in x86_64|amd64) target_arch=amd64;; arm64|aarch64) target_arch=arm64;; *) printf '%s\n' 'unsupported architecture' >&2; exit 2;; esac
stage=$(mktemp -d)
trap 'rm -rf -- "$stage"' EXIT HUP INT TERM
archive="zsh-git-inlay_${version}_${target_os}-${target_arch}.tar.gz"
curl --fail --location --proto '=https' --tlsv1.2 --output "$stage/$archive" "https://github.com/gongahkia/zsh-git-inlay/releases/download/$version/$archive"
if command -v shasum >/dev/null 2>&1; then
 actual=$(shasum -a 256 "$stage/$archive"); actual=${actual%% *}
else
 actual=$(sha256sum "$stage/$archive"); actual=${actual%% *}
fi
[ "$actual" = "$expected" ] || { printf '%s\n' 'archive checksum mismatch; nothing installed' >&2; exit 1; }
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd -P)
sh "$script_dir/install-release.sh" --archive "$stage/$archive" --prefix "$prefix"
