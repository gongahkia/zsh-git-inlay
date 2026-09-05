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
module=$(
	CDPATH=''
	export CDPATH
	cd -- "$source_dir" && go list -m -f '{{.Path}}'
)
case "$module" in
	*[!A-Za-z0-9./_-]*|"") printf '%s\n' 'unexpected module identifier' >&2; exit 1 ;;
esac
temporary=$(mktemp "$(dirname "$output")/.sbom.XXXXXX")
cleanup() {
	[ -z "$temporary" ] || rm -f -- "$temporary"
}
trap cleanup EXIT HUP INT TERM
cat > "$temporary" <<EOF
{
  "spdxVersion": "SPDX-2.3",
  "dataLicense": "CC0-1.0",
  "SPDXID": "SPDXRef-DOCUMENT",
  "name": "zsh-git-inlay-${version}",
  "documentNamespace": "https://github.com/gongahkia/zsh-git-inlay/spdx/${version}",
  "creationInfo": {
    "creators": ["Tool: zsh-git-inlay release snapshot"],
    "created": "1970-01-01T00:00:00Z"
  },
  "packages": [
    {
      "SPDXID": "SPDXRef-Package-zsh-git-inlay",
      "name": "${module}",
      "versionInfo": "${version}",
      "downloadLocation": "NOASSERTION",
      "filesAnalyzed": false,
      "licenseConcluded": "NOASSERTION",
      "licenseDeclared": "NOASSERTION",
      "copyrightText": "NOASSERTION",
      "primaryPackagePurpose": "APPLICATION"
    }
  ],
  "relationships": [
    {
      "spdxElementId": "SPDXRef-DOCUMENT",
      "relationshipType": "DESCRIBES",
      "relatedSpdxElement": "SPDXRef-Package-zsh-git-inlay"
    }
  ]
}
EOF
chmod 644 "$temporary"
mv -f "$temporary" "$output"
temporary=
