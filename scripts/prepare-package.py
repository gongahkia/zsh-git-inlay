#!/usr/bin/env python3
"""Generate a reviewable Homebrew formula from licensed, verified snapshots."""
import argparse
import hashlib
from pathlib import Path
import re
import tarfile


def prepare(dist, version, license_id, source):
    if not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?", version):
        raise ValueError("version must be explicit vMAJOR.MINOR.PATCH")
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9.+-]{0,79}", license_id):
        raise ValueError("supply the maintainer-selected SPDX license identifier")
    license_path = source / "LICENSE"
    if not license_path.is_file() or not license_path.read_bytes().strip():
        raise ValueError("select and commit LICENSE before preparing distributable packages")
    checksums = {}
    for line in (dist / "checksums.txt").read_text().splitlines():
        digest, name = line.split(maxsplit=1)
        name = name.removeprefix("./")
        if not re.fullmatch(r"[a-f0-9]{64}", digest) or name in checksums or "/" in name:
            raise ValueError("invalid or duplicate checksum record")
        checksums[name] = digest
    blocks = []
    for platform, os_name in (("on_macos", "darwin"), ("on_linux", "linux")):
        blocks.append(f"  {platform} do")
        for cpu, arch in (("on_intel", "amd64"), ("on_arm", "arm64")):
            name = f"zsh-git-inlay_{version}_{os_name}-{arch}.tar.gz"
            archive = dist / name
            digest = hashlib.sha256(archive.read_bytes()).hexdigest()
            if checksums.get(name) != digest:
                raise ValueError(f"checksum mismatch: {name}")
            with tarfile.open(archive, "r:gz") as bundle:
                member = bundle.getmember("LICENSE")
                if not member.isfile() or bundle.extractfile(member).read() != license_path.read_bytes():
                    raise ValueError(f"missing or mismatched selected license in {name}")
            blocks += [f"    {cpu} do", f'      url "https://github.com/gongahkia/zsh-git-inlay/releases/download/{version}/{name}"', f'      sha256 "{digest}"', "    end"]
        blocks.append("  end")
    return '\n'.join([
        "class ZshGitInlay < Formula",
        '  desc "Prepared commit-message autosuggestions for Zsh"',
        '  homepage "https://github.com/gongahkia/zsh-git-inlay"',
        f'  version "{version[1:]}"', f'  license "{license_id}"',
        *blocks, '  depends_on "git"', '  depends_on "zsh"', '  depends_on "zsh-autosuggestions"',
        "  def install", '    bin.install "zsh-git-inlay"',
        '    (share/"zsh-git-inlay").install "zsh-git-inlay.plugin.zsh"',
        '    pkgshare.install "LICENSE"', "  end", "  test do",
        '    system bin/"zsh-git-inlay", "setup", "--check"', "  end", "end", "",
    ])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dist", type=Path, required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--license-id", required=True)
    parser.add_argument("--source", type=Path, default=Path(__file__).resolve().parent.parent)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    try:
        formula = prepare(args.dist, args.version, args.license_id, args.source)
        with args.output.open("x") as output:
            output.write(formula)
    except (OSError, ValueError, KeyError, tarfile.TarError) as error:
        parser.exit(1, f"package preparation failed: {error}\n")


if __name__ == "__main__":
    main()
