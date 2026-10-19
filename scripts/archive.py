#!/usr/bin/env python3
"""Write release tar/gzip metadata independently of builder paths and ownership."""
import argparse
import gzip
from pathlib import Path
import tarfile


def archive(source, output, members):
    for name in members:
        path = source / name
        if Path(name).name != name or path.is_symlink() or not path.is_file():
            raise ValueError(f"invalid release member: {name}")
    with output.open("xb") as raw:
        with gzip.GzipFile(fileobj=raw, filename="", mode="wb", mtime=0, compresslevel=9) as compressed:
            with tarfile.open(fileobj=compressed, mode="w", format=tarfile.USTAR_FORMAT) as bundle:
                for name in members:
                    path = source / name
                    info = tarfile.TarInfo(name)
                    info.size = path.stat().st_size
                    info.mode = 0o755 if name == "zsh-git-inlay" else 0o644
                    info.uid = info.gid = 0
                    info.uname = info.gname = "root"
                    info.mtime = 946684800
                    with path.open("rb") as content:
                        bundle.addfile(info, content)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("members", nargs="+")
    args = parser.parse_args()
    try:
        archive(args.source, args.output, args.members)
    except (OSError, ValueError, tarfile.TarError) as error:
        parser.exit(1, f"release archive failed: {error}\n")


if __name__ == "__main__":
    main()
