import hashlib
import io
import os
from pathlib import Path
import runpy
import subprocess
import tarfile
import tempfile
import unittest

PROJECT = Path(__file__).resolve().parent.parent
prepare = runpy.run_path(str(PROJECT / "scripts/prepare-package.py"))["prepare"]


class Packages(unittest.TestCase):
    def test_formula_requires_license_and_checks_all_archives(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            with self.assertRaises(ValueError):
                prepare(root, "v1.2.3", "MIT", root)
            (root / "LICENSE").write_text("fixture license for test only\n")
            records = []
            for target in ("darwin-amd64", "darwin-arm64", "linux-amd64", "linux-arm64"):
                name = f"zsh-git-inlay_v1.2.3_{target}.tar.gz"
                with tarfile.open(root / name, "w:gz") as archive:
                    archive.add(root / "LICENSE", arcname="LICENSE")
                digest = hashlib.sha256((root / name).read_bytes()).hexdigest()
                records.append(f"{digest}  ./{name}\n")
            (root / "checksums.txt").write_text("".join(records))
            formula = prepare(root, "v1.2.3", "MIT", root)
            self.assertEqual(formula.count('sha256 "'), 4)
            self.assertIn('"setup", "--check"', formula)
            (root / "zsh-git-inlay_v1.2.3_darwin-arm64.tar.gz").write_bytes(b"tampered")
            with self.assertRaises(ValueError):
                prepare(root, "v1.2.3", "MIT", root)
            with self.assertRaises(ValueError):
                prepare(root, "v1.2.3$(bad)", "MIT", root)

    def test_release_archives_ignore_source_time_path_and_ownership(self):
        archiver = runpy.run_path(str(PROJECT / "scripts/archive.py"))["archive"]
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            outputs = []
            for index in (1, 2):
                source = root / f"source-{index}"
                source.mkdir()
                for name in ("zsh-git-inlay", "zsh-git-inlay.plugin.zsh"):
                    path = source / name
                    path.write_bytes(b"fixture content")
                    os.utime(path, (index * 1000, index * 1000))
                output = root / f"archive-{index}.tar.gz"
                archiver(source, output, ["zsh-git-inlay", "zsh-git-inlay.plugin.zsh"])
                outputs.append(output)
            self.assertEqual(outputs[0].read_bytes(), outputs[1].read_bytes())
            with tarfile.open(outputs[0]) as bundle:
                for member in bundle.getmembers():
                    self.assertEqual((member.uid, member.gid, member.mtime), (0, 0, 946684800))
            self.assertEqual(outputs[0].read_bytes()[4:8], bytes(4))

    def test_binary_install_verifies_digest_before_writing(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            archive_path = root / "fixture.tar.gz"
            with tarfile.open(archive_path, "w:gz") as archive:
                for name in ("zsh-git-inlay", "zsh-git-inlay.plugin.zsh", "README.md", "CHANGELOG.md"):
                    content = b"fixture content\n"
                    member = tarfile.TarInfo(name)
                    member.size = len(content)
                    archive.addfile(member, io.BytesIO(content))
            fake_bin = root / "tools"
            fake_bin.mkdir()
            curl = fake_bin / "curl"
            curl.write_text('#!/bin/sh\nwhile [ "$1" != --output ]; do shift; done\ncp "$TEST_ARCHIVE" "$2"\n')
            curl.chmod(0o755)
            environment = dict(os.environ, PATH=f"{fake_bin}:{os.environ['PATH']}", TEST_ARCHIVE=str(archive_path))
            prefix = root / "prefix with spaces"
            args = ["sh", str(PROJECT / "scripts/install-binary.sh"), "--version", "v1.2.3", "--prefix", str(prefix), "--sha256"]
            failure = subprocess.run(args + ["0" * 64], env=environment, capture_output=True)
            self.assertNotEqual(failure.returncode, 0)
            self.assertFalse(prefix.exists())
            digest = hashlib.sha256(archive_path.read_bytes()).hexdigest()
            subprocess.run(args + [digest], env=environment, check=True, capture_output=True)
            self.assertEqual((prefix / "bin/zsh-git-inlay").read_bytes(), b"fixture content\n")


if __name__ == "__main__":
    unittest.main()
