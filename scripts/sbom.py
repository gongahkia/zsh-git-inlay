#!/usr/bin/env python3
"""Emit a reproducible SPDX inventory from the complete Go module graph."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import tempfile


def inventory(source, version):
    output = subprocess.check_output(["go", "-C", str(source), "list", "-m", "-json", "all"], text=True)
    modules = []
    decoder = json.JSONDecoder()
    while output.strip():
        module, end = decoder.raw_decode(output.lstrip())
        modules.append(module)
        output = output.lstrip()[end:]
    packages, relationships = [], []
    main_id = "SPDXRef-Package-zsh-git-inlay"
    for index, module in enumerate(sorted(modules, key=lambda item: (not item.get("Main", False), item["Path"]))):
        main = module.get("Main", False)
        identifier = main_id if main else f"SPDXRef-Dependency-{index}"
        package_version = version if main else module["Version"]
        package = {
            "SPDXID": identifier, "name": module["Path"], "versionInfo": package_version,
            "downloadLocation": "NOASSERTION", "filesAnalyzed": False,
            "licenseConcluded": "NOASSERTION",
            "licenseDeclared": {"github.com/pelletier/go-toml/v2": "MIT"}.get(module["Path"], "NOASSERTION"),
            "copyrightText": "NOASSERTION", "primaryPackagePurpose": "APPLICATION" if main else "LIBRARY",
        }
        if not main:
            package["externalRefs"] = [{"referenceCategory": "PACKAGE-MANAGER", "referenceType": "purl", "referenceLocator": f"pkg:golang/{module['Path']}@{package_version}"}]
        packages.append(package)
        relationships.append({"spdxElementId": "SPDXRef-DOCUMENT" if main else main_id, "relationshipType": "DESCRIBES" if main else "DEPENDS_ON", "relatedSpdxElement": identifier})
    return {
        "spdxVersion": "SPDX-2.3", "dataLicense": "CC0-1.0", "SPDXID": "SPDXRef-DOCUMENT",
        "name": f"zsh-git-inlay-{version}",
        "documentNamespace": f"https://github.com/gongahkia/zsh-git-inlay/spdx/{version}",
        "creationInfo": {"creators": ["Tool: zsh-git-inlay release snapshot"], "created": "1970-01-01T00:00:00Z"},
        "packages": packages, "relationships": relationships,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    data = inventory(args.source, args.version)
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(mode="w", dir=args.output.parent, prefix=".sbom.", delete=False) as stream:
            temporary = stream.name
            json.dump(data, stream, indent=2)
            stream.write("\n")
        os.chmod(temporary, 0o644)
        os.replace(temporary, args.output)
        temporary = None
    finally:
        if temporary:
            os.unlink(temporary)


if __name__ == "__main__":
    main()
