#!/usr/bin/env python3
"""Real-helper lifecycle rehearsals; all shell/config/data writes are disposable."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import time
import unittest

PROJECT = Path(__file__).resolve().parent.parent
BINARY = Path(os.environ.get("ZSH_GIT_INLAY_BIN", PROJECT / ".build/zsh-git-inlay")).resolve()
DEPENDENCY = next((path for path in [
    Path(os.environ.get("ZSH_AUTOSUGGESTIONS_SOURCE", "/unavailable")),
    Path("/opt/homebrew/share/zsh-autosuggestions/zsh-autosuggestions.zsh"),
    Path("/usr/local/share/zsh-autosuggestions/zsh-autosuggestions.zsh"),
    Path("/usr/share/zsh-autosuggestions/zsh-autosuggestions.zsh"),
] if path.is_file()), None)


def quote(value):
    return "'" + str(value).replace("'", "'\\''") + "'"


class Lifecycle(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if not BINARY.is_file() or DEPENDENCY is None or shutil.which("zsh") is None:
            raise RuntimeError("Build the helper and install zsh-autosuggestions (or set ZSH_AUTOSUGGESTIONS_SOURCE) before this required rehearsal")
        cls.old_build = tempfile.TemporaryDirectory(prefix="zgi-old-", dir="/tmp")
        cls.old_binary = Path(cls.old_build.name) / "old-helper"
        subprocess.run(["go", "build", "-trimpath", "-buildvcs=false", "-ldflags", "-s -w -buildid= -X main.version=old-lifecycle-build -X main.commit=older-build", "-o", str(cls.old_binary), "./cmd/zsh-git-inlay"], cwd=PROJECT, check=True, timeout=120)

    @classmethod
    def tearDownClass(cls):
        cls.old_build.cleanup()

    def setUp(self):
        self.directory = tempfile.TemporaryDirectory(prefix="zgi-life-", dir="/tmp")
        self.root = Path(self.directory.name)
        self.env = {key: value for key, value in os.environ.items() if not key.startswith(("ZSH_GIT_INLAY_", "GIT_")) and key not in ("ZDOTDIR", "OPENAI_API_KEY")}
        for key, name in {"HOME": "home", "XDG_CONFIG_HOME": "config", "XDG_DATA_HOME": "data", "XDG_CACHE_HOME": "cache", "XDG_STATE_HOME": "state", "XDG_RUNTIME_DIR": "run"}.items():
            path = self.root / name
            path.mkdir(mode=0o700)
            (path / "unrelated-sentinel").write_text("retain\n")
            self.env[key] = str(path)
        self.env.update(GIT_CONFIG_GLOBAL=os.devnull, GIT_CONFIG_NOSYSTEM="1", ZSH_AUTOSUGGESTIONS_SOURCE=str(DEPENDENCY))
        self.prefix = self.root / "prefix with ' quotes"
        self.installed = self.prefix / "bin/zsh-git-inlay"
        self.zdotdir = self.root / "zsh"
        self.zdotdir.mkdir(mode=0o700)
        self.rc_target = self.root / "dotfiles-zshrc"
        self.rc_target.write_text("# my surrounding configuration\nexport ZGI_LIFECYCLE_SENTINEL=kept\n")
        self.rc = self.zdotdir / ".zshrc"
        self.rc.symlink_to(self.rc_target)
        (Path(self.env["HOME"]) / ".zshenv").write_text("ZDOTDIR=" + quote(self.zdotdir) + "\n")
        self.config = Path(self.env["XDG_CONFIG_HOME"]) / "zsh-git-inlay/config.toml"
        self.data = Path(self.env["XDG_DATA_HOME"]) / "zsh-git-inlay"
        self.socket = Path(self.env["XDG_RUNTIME_DIR"]) / "zsh-git-inlay/daemon.sock"
        self.repo = self.root / "repo"
        self.git("init", "-q", "-b", "main", str(self.repo), cwd=self.root)
        self.git("config", "user.name", "Lifecycle Fixture")
        self.git("config", "user.email", "lifecycle@example.invalid")
        path = self.repo / "cmd/demo/main.go"
        path.parent.mkdir(parents=True)
        path.write_text("package demo\n")
        self.git("add", ".")
        self.git("commit", "-qm", "chore: initial fixture")
        path.write_text("package demo\nfunc Greeting() string { return \"hello\" }\n")
        self.git("add", ".")
        self.staged = self.git("diff", "--cached", "--binary").stdout
        self.old_process = None

    def tearDown(self):
        subprocess.run([str(BINARY), "daemon", "stop"], env=self.env, cwd=self.root, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=5)
        if self.old_process is not None:
            try:
                self.old_process.wait(timeout=3)
            except subprocess.TimeoutExpired:
                self.old_process.terminate()
                try:
                    self.old_process.wait(timeout=2)
                except subprocess.TimeoutExpired:
                    self.old_process.kill()
                    self.old_process.wait()
        deadline = time.monotonic() + 3
        while self.socket.exists() and time.monotonic() < deadline:
            time.sleep(0.02)
        self.directory.cleanup()

    def git(self, *args, cwd=None):
        return subprocess.run(["git", *args], cwd=cwd or self.repo, env=self.env, text=True, capture_output=True, check=True, timeout=10)

    def command(self, *args, binary=None, success=True):
        result = subprocess.run([str(binary or self.installed), *map(str, args)], cwd=self.repo, env=self.env, text=True, capture_output=True, timeout=20)
        if success:
            self.assertEqual(result.returncode, 0, f"{args}\n{result.stdout}\n{result.stderr}")
        return result

    def report(self, *args, **kwargs):
        result = self.command(*args, "--json", **kwargs)
        return json.loads(result.stdout)

    def install(self):
        result = self.report("setup", "install", "--source", PROJECT, "--prefix", self.prefix, "--autosuggestions", DEPENDENCY, binary=BINARY)
        self.assertTrue(result["changed"])
        self.assertEqual(Path(result["installation"]["shell_file"]), self.rc)
        self.assertTrue(self.rc.is_symlink())
        self.assertEqual(self.rc_target.read_text().count("# >>> zsh-git-inlay managed >>>"), 1)
        for probe in result["shell_checks"]:
            self.assertTrue(probe["loaded"] and probe["dependency_loaded"] and probe["async"])
        return result

    def wait_ready(self):
        self.command("observe", "--cwd", self.repo)
        deadline = time.monotonic() + 10
        last = None
        while time.monotonic() < deadline:
            last = self.report("suggest", "--no-start", "--cwd", self.repo, "--buffer", "git commit -m ")
            if last["status"] == "ready":
                return last
            time.sleep(0.05)
        self.fail(f"candidate did not become ready: {last}; {self.command('status', '--cwd', self.repo, '--json').stdout}")

    def test_install_first_suggestion_tune_repair_disable_remove_and_undo(self):
        installed = self.install()
        self.assertTrue(self.report("doctor", "--check")["ok"])
        self.assertTrue(self.wait_ready()["suggestion"].startswith("git commit -m "))
        actual_shell = subprocess.run(["zsh", "-ic", "suggestion=''; _zsh_autosuggest_strategy_git-inlay 'git commit -m '; print -r -- \"$suggestion\""], cwd=self.repo, env=self.env, text=True, capture_output=True, timeout=10)
        self.assertEqual(actual_shell.returncode, 0, actual_shell.stderr)
        self.assertTrue(actual_shell.stdout.strip().startswith("git commit -m "), actual_shell.stdout)
        repeated = self.report("setup", "install", "--source", PROJECT, "--prefix", self.prefix, "--autosuggestions", DEPENDENCY)
        self.assertFalse(repeated["changed"])
        self.assertEqual(self.rc_target.read_text().count("# >>> zsh-git-inlay managed >>>"), 1)
        tuned = self.report("setup", "customize", "--set", "commit.capitalization=sentence", "--set", "commit.line_length=72", "--set", "commit.scopes=[\"cli\"]", "--set", "commit.scope_paths=[\"cmd/demo=cli\"]", "--set", "suggestions.candidate_count=1", "--set", "learning.enabled=false")
        self.assertTrue(tuned["changed"])
        self.wait_ready()
        candidates = self.report("candidates", "--cwd", self.repo)
        self.assertEqual(len(candidates["candidates"]), 1)
        message = candidates["candidates"][0]["message"]
        self.assertRegex(message, r"^[a-z]+\(cli\): [A-Z]")
        self.assertLessEqual(len(message), 72)
        self.assertFalse(candidates["learning"]["enabled"])
        self.report("feedback", "too-generic", "--cwd", self.repo)
        self.report("feedback", "useful", "--cwd", self.repo)
        feedback = self.report("feedback", "show", "--cwd", self.repo)
        self.assertEqual(feedback["counts"], {"useful": 1, "too-generic": 0, "incorrect": 0})
        self.assertNotIn(message, (self.data / "feedback.json").read_text())
        self.command("disable", "--repo", "--cwd", self.repo)
        self.assertEqual(self.report("suggest", "--cwd", self.repo, "--buffer", "git commit -m ")["status"], "disabled")
        self.command("enable", "--repo", "--cwd", self.repo)
        self.command("snooze", "1h", "--repo", "--cwd", self.repo)
        self.assertEqual(self.report("suggest", "--cwd", self.repo, "--buffer", "git commit -m ")["status"], "snoozed")
        self.command("enable", "--repo", "--cwd", self.repo)
        self.assertEqual(self.report("suggest", "--cwd", self.repo, "--buffer", "git commit -m ")["status"], "ready")
        with self.config.open("a") as stream:
            stream.write("\n[provider]\nname = 'invalid-provider' # repair only this\n")
        broken = self.command("doctor", "--json", success=False)
        self.assertNotEqual(broken.returncode, 0)
        self.assertIn("provider.name", broken.stdout)
        repaired = self.report("doctor", "--fix")
        self.assertTrue(repaired["ok"])
        self.assertTrue(any(repair["kind"] == "configuration" for repair in repaired["repairs"]))
        self.assertIn("# name = 'invalid-provider' # repair only this", self.config.read_text())
        self.wait_ready()
        with self.rc_target.open("a") as stream:
            stream.write("# a later unrelated shell edit\n")
        self.command("disable")
        self.assertEqual(self.report("suggest", "--cwd", self.repo, "--buffer", "git commit -m ")["status"], "disabled")
        removed = self.report("uninstall")
        self.assertFalse(self.installed.exists())
        self.assertTrue(self.rc.is_symlink())
        self.assertIn("# a later unrelated shell edit", self.rc_target.read_text())
        self.assertNotIn("# >>> zsh-git-inlay managed >>>", self.rc_target.read_text())
        self.assertTrue(self.config.exists() and (self.data / "feedback.json").exists())
        recovery = Path(removed["recovery_helper"])
        self.assertEqual(recovery.stat().st_mode & 0o777, 0o700)
        self.command("undo", removed["transaction"], binary=recovery)
        self.assertTrue(self.installed.exists())
        self.assertIn("# a later unrelated shell edit", self.rc_target.read_text())
        self.command("enable")
        self.wait_ready()
        self.assertEqual(self.git("diff", "--cached", "--binary").stdout, self.staged)
        purged = self.report("uninstall", "--purge-local-data")
        self.assertTrue(purged["purged"])
        self.assertNotIn("transaction", purged)
        self.assertFalse(recovery.exists())
        for key in ("XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "XDG_RUNTIME_DIR"):
            self.assertFalse((Path(self.env[key]) / "zsh-git-inlay").exists())
            self.assertEqual((Path(self.env[key]) / "unrelated-sentinel").read_text(), "retain\n")
        self.assertTrue(Path(installed["installation"]["shell_file"]).is_symlink())

    def test_stale_running_build_upgrade_and_conservative_block_repair(self):
        self.install()
        self.command("daemon", "stop", success=False)
        deadline = time.monotonic() + 4
        while self.socket.exists() and time.monotonic() < deadline:
            time.sleep(0.02)
        self.assertFalse(self.socket.exists())
        old_path = self.root / "old-running-helper"
        shutil.copy2(self.old_binary, old_path)
        self.old_process = subprocess.Popen([str(old_path), "daemon", "serve", "--observe", str(self.repo)], cwd=self.repo, env=self.env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, start_new_session=True)
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            status = self.report("status", "--cwd", self.repo)
            if status.get("build", {}).get("version") == "old-lifecycle-build":
                break
            self.assertIsNone(self.old_process.poll(), "old build exited before serving")
            time.sleep(0.02)
        else:
            self.fail("old build did not become the active daemon")
        self.wait_ready()
        before = self.report("status", "--cwd", self.repo)
        self.assertEqual(before["build"]["version"], "old-lifecycle-build")
        replacement = self.root / "replacement-helper"
        shutil.copy2(BINARY, replacement)
        replacement.replace(old_path)
        after = self.report("status", "--cwd", self.repo)
        self.assertEqual(after["build"]["sha256"], before["build"]["sha256"])
        unhealthy = self.command("doctor", "--check", "--json", success=False)
        self.assertNotEqual(unhealthy.returncode, 0)
        self.assertTrue(any(check["name"] == "daemon_build" and check["status"] == "fail" for check in json.loads(unhealthy.stdout)["checks"]))
        self.report("setup", "upgrade", "--source", PROJECT, "--autosuggestions", DEPENDENCY)
        self.assertEqual(self.old_process.wait(timeout=5), 0)
        self.assertFalse(self.socket.exists())
        self.wait_ready()
        self.assertEqual(self.report("status", "--cwd", self.repo)["build"]["sha256"], self.report("version")["sha256"])
        plugin = self.prefix / "share/zsh-git-inlay/zsh-git-inlay.plugin.zsh"
        original_plugin = plugin.read_bytes()
        plugin.write_text("# damaged installed plugin\n")
        self.rc_target.write_text("# keep my replacement configuration\n")
        repaired = self.report("doctor", "--fix")
        self.assertTrue(repaired["ok"])
        self.assertEqual(plugin.read_bytes(), original_plugin)
        self.assertTrue(self.rc.is_symlink())
        self.assertTrue(self.rc_target.read_text().startswith("# keep my replacement configuration\n"))
        original_rc = self.rc_target.read_text()
        block = original_rc[original_rc.index("# >>> zsh-git-inlay managed >>>"):]
        duplicate = original_rc + block
        self.rc_target.write_text(duplicate)
        refused = self.command("doctor", "--fix", "--json", success=False)
        self.assertNotEqual(refused.returncode, 0)
        self.assertEqual(self.rc_target.read_text(), duplicate)
        self.rc_target.write_text(original_rc)
        self.assertTrue(self.report("doctor", "--check")["ok"])
        self.report("uninstall", "--purge-local-data")


if __name__ == "__main__":
    unittest.main(verbosity=2)
