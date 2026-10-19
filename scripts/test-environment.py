#!/usr/bin/env python3
"""Run the complete suite with disposable shell/config/data state."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import tempfile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    command = args.command
    if command and command[0] == "--":
        command = command[1:]
    if not command:
        parser.error("supply the test command after --")
    environment = dict(os.environ)
    # Keep compiler caches outside the disposable application HOME. This does
    # not change user Go settings or require downloading dependencies again.
    compiler = json.loads(subprocess.check_output(["go", "env", "-json", "GOCACHE", "GOMODCACHE"], text=True, timeout=10))
    environment.update(compiler)
    for key in list(environment):
        if key.startswith(("ZSH_GIT_INLAY_", "GIT_")) or key in ("ZDOTDIR", "OPENAI_API_KEY"):
            del environment[key]
    with tempfile.TemporaryDirectory(prefix="zgi-suite-", dir="/tmp") as directory:
        root = Path(directory)
        for key, name in {"HOME": "home", "XDG_CONFIG_HOME": "config", "XDG_DATA_HOME": "data", "XDG_CACHE_HOME": "cache", "XDG_STATE_HOME": "state", "XDG_RUNTIME_DIR": "runtime"}.items():
            path = root / name
            path.mkdir(mode=0o700)
            environment[key] = str(path)
        environment.update(GIT_CONFIG_GLOBAL=os.devnull, GIT_CONFIG_NOSYSTEM="1")
        try:
            return subprocess.run(command, env=environment).returncode
        finally:
            if args.binary.is_file():
                subprocess.run([str(args.binary), "daemon", "stop"], env=environment, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=5)


if __name__ == "__main__":
    raise SystemExit(main())
