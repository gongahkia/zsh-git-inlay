# Installation and release preparation

## Supported evidence

Fedora Linux 43 is the only environment exercised locally for the full runtime,
Zsh integration, install/upgrade/uninstall, and release-snapshot tests. The
snapshot builder cross-compiles `linux/{amd64,arm64}` and `darwin/{amd64,arm64}`
archives, but macOS runtime execution has not been performed on this host.
The GitHub Actions workflow is prepared for Linux and macOS but has not run
from this local checkout. These are not package publications or platform-wide
compatibility claims.

RC1 validated the source and Linux-archive paths in temporary XDG directories,
including spaces in the install prefix, reinstall, default local-data
preservation, explicit purge, and a permission-denied prefix when the host user
is non-root. It did not execute a macOS binary or a remote CI job.

Requirements are Git, Zsh 5.0.8+, Go 1.26.7 to reproduce the current build,
and `zsh-autosuggestions` loaded before the plugin. On Fedora, install the
runtime prerequisites with DNF:

```zsh
sudo dnf install git zsh zsh-autosuggestions golang
```

Ollama is optional and is not an installation requirement or implicit model
download. The controlled live-model evaluation has a separate explicit approval
boundary, reviewed Fedora package method, disk footprint, and cleanup choices
in [EXECPLAN-LIVE-MODEL.md](EXECPLAN-LIVE-MODEL.md).

Build from a checked-out source tree, then use the local installer. It neither
downloads a model nor edits `.zshrc`:

```zsh
make build
sh scripts/install.sh --source "$PWD"
```

The default prefix is `~/.local`. To choose another prefix, provide
`--prefix /chosen/prefix`. The installer atomically replaces only its binary
and plugin files after both temporary copies have been prepared. It prints the
two lines to add after loading `zsh-autosuggestions`:

```zsh
path=("$HOME/.local/bin" $path)
source "$HOME/.local/share/zsh-git-inlay/zsh-git-inlay.plugin.zsh"
```

Start a fresh Zsh session, then verify dependency/load order and runtime paths:

```zsh
zsh-git-inlay version --json
zsh-git-inlay doctor --json
```

## Upgrade and uninstall

To upgrade, check out the desired reviewed source revision, run `make build`,
and rerun the same install command. The installer replaces only
`<prefix>/bin/zsh-git-inlay` and
`<prefix>/share/zsh-git-inlay/zsh-git-inlay.plugin.zsh`; it does not touch
Ollama models, repository data, or other prefix contents.

Before uninstalling, remove the source line from `.zshrc`. The uninstaller
stops a daemon started by the installed binary when possible and removes only
those two installed files:

```zsh
sh scripts/uninstall.sh --prefix "$HOME/.local"
```

Local cache, state, data, configuration, and runtime directories are retained
by default. To delete only the product-named directories under the standard XDG
roots, make that destructive choice explicit:

```zsh
sh scripts/uninstall.sh --prefix "$HOME/.local" --purge-local-data
```

Custom `ZSH_GIT_INLAY_*` directory overrides are never guessed or removed by
the script.

## Reproducible snapshots

`make release-snapshot` creates a new output directory; it refuses to overwrite
an existing one. Supply a reviewed version identifier for a candidate artifact:

```zsh
make release-snapshot VERSION=0.1.0-rc.1 DIST=dist/0.1.0-rc.1
```

The command cross-compiles four archives with `-trimpath`, disabled VCS
embedding, a fixed linker build ID, and embedded version/commit values. It
normalizes archive input timestamps, emits `checksums.txt`, and writes a
deterministic SPDX 2.3 SBOM. The current Go module has no third-party modules;
the SBOM therefore describes only this application and does not assert a
license. The checked-in lifecycle test builds two snapshots from the same
inputs and compares every archive and metadata file byte-for-byte on Fedora.

Run the local release checks with:

```zsh
make test-install
make release-snapshot VERSION=0.1.0-rc.1 DIST=dist/0.1.0-rc.1
(cd dist/0.1.0-rc.1 && sha256sum -c checksums.txt)
```

After independently verifying the published checksum, a Linux archive can be
installed without a source checkout. The installer accepts only the four
expected member names, streams each into a private staging directory without
archive extraction, and atomically replaces the same two product files as the
source installer; it does not modify configuration, cache, state, or
managed-model data.

```zsh
sha256sum -c checksums.txt
sh scripts/install-release.sh \
  --archive zsh-git-inlay_0.1.0-rc.1_linux-amd64.tar.gz \
  --prefix "$HOME/.local"
```

This archive flow was exercised locally with a Linux cross-compiled artifact.
It is not evidence that a macOS binary has run on macOS.

The repository has no `LICENSE` file. Snapshot generation is useful for
internal validation, but publishing or redistributing artifacts remains blocked
until the maintainer selects and adds a license. `CHANGELOG.md` defines the
Semantic Versioning and approval policy; no tag, package, or GitHub release is
created by these commands. See [LICENSE-REVIEW.md](LICENSE-REVIEW.md) before
redistribution. Archives include neither Ollama, `zsh-autosuggestions`, model
weights, cached activity, configuration, nor local evaluation reports.
