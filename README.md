# AliasDeck

> Your commands. Every machine.

You write an alias once. AliasDeck compiles it into the right syntax for every shell you use — zsh, bash, PowerShell — and writes it to each machine without touching your `.zshrc`.

It is not a dotfile manager. Dotfile managers copy files; AliasDeck understands what an alias *is*, which is why it can turn one definition into three different shells.

---

## Install

**Homebrew** (macOS and Linux):

```bash
brew install angeltonio/tap/aliasdeck
```

**Or without Homebrew:**

```bash
curl -sSL https://raw.githubusercontent.com/angeltonio/aliasdeck/main/scripts/install.sh | sh
```

The script verifies the download against the release checksums before installing anything.

**Windows** (Scoop):

```powershell
scoop bucket add angeltonio https://github.com/angeltonio/scoop-bucket
scoop install aliasdeck
```

## Getting started

```bash
aliasdeck init      # creates ~/.config/aliasdeck/{config,aliases}.yaml, detects platform and shell,
                    # and asks before adding a one-line bootstrap to your shell rc file
aliasdeck edit      # opens aliases.yaml in $EDITOR — add your own aliases here
aliasdeck sync      # renders and writes the generated file for your shell
aliasdeck status    # active source, device identity, up-to-date check
aliasdeck list      # aliases that apply to this device, and why others are skipped
```

Reload your shell and your aliases are live. `aliasdeck uninstall` reverses all of it, leaving your rc file byte-identical to how it was found.

## Status

**v0.3** — adds a self-hosted server. `aliasdeck serve` runs a single static
binary with an embedded SQLite database, exposing a REST API under
`/api/v1` for aliases, profiles and devices, guarded by operator sessions
and per-device tokens (`login`, `register`, `logout` on the CLI side). It is
API-only: there is no web UI yet (that is v0.4). TLS is not built in — put a
reverse proxy in front for anything reachable beyond loopback. The default
bind (`aliasdeck serve --addr`) is `127.0.0.1:8080`, loopback only; widening
it to another interface is a deliberate, explicit operator choice.

| Component | State |
| --- | --- |
| Domain model and resolution | ✅ |
| Validation (name/command/description safety) | ✅ |
| zsh + bash renderers | ✅ |
| PowerShell renderer and Windows support | ✅ |
| Git-hosted configuration | ✅ |
| Standalone CLI (`init`, `sync`, `status`, `list`, `doctor`, `edit`, `uninstall`) | ✅ |
| Homebrew, Scoop and install script | ✅ |
| Self-hosted server (`serve`, REST API, SQLite, device enrollment) | ✅ |
| Web UI | ⬜ Later |

---

## The idea

You declare a command once, in neutral terms:

```yaml
# aliases.yaml
aliases:
  - name: dps
    command: docker ps
    shells: [zsh, bash, powershell]
    profiles: [development]
```

AliasDeck renders it per shell:

```bash
# zsh and bash
alias dps='docker ps'
```

```powershell
# PowerShell — Set-Alias cannot hold a command string, so a function is generated
function dps {
    $__aliasdeck_cmd = 'docker ps'
    & ([scriptblock]::Create($__aliasdeck_cmd + ' @args')) @args
}
```

That last line is the whole point. A dotfile manager cannot do this for you, because it does not know that `docker ps` is a command rather than a line of text.

### Where the output goes

Never into your shell config. AliasDeck owns one generated file:

```text
~/.config/aliasdeck/aliases.zsh
```

Your `.zshrc` needs exactly one line, added once:

```bash
[ -f "$HOME/.config/aliasdeck/aliases.zsh" ] && source "$HOME/.config/aliasdeck/aliases.zsh"
```

Installing is one line. Uninstalling is deleting it. AliasDeck never rewrites a file you own.

---

## How is this different from Chezmoi?

Chezmoi is excellent and AliasDeck does not replace it. They solve different problems.

| | Chezmoi & friends | AliasDeck |
| --- | --- | --- |
| Unit of work | Files | Commands |
| Cross-shell support | You write template conditionals | Built in, per-shell renderers |
| Escaping | Yours to get right | Handled and tested against real shells |
| Targeting | Hostname conditionals | Profiles: Development, Homelab, Work |
| Scope | All your dotfiles | Aliases and reusable commands |

**They compose.** Your `aliases.yaml` can live in your existing dotfiles repo, and AliasDeck will be able to apply through Chezmoi instead of writing files itself.

---

## Two ways to use it

**Standalone** — one CLI, one `aliases.yaml`. No server, no account, no database. This is the primary way to use AliasDeck, and it is what v0.1 ships.

```bash
aliasdeck init
aliasdeck edit
aliasdeck sync
```

**Control plane** — a self-hosted server for managing many machines centrally: a REST API under `/api/v1`, guarded by operator sessions and per-device tokens. A single static binary with an embedded SQLite database — no separate database server to run. There is no web UI yet (v0.4) and no official Docker image yet; TLS is not built in, so put a reverse proxy in front for anything reachable beyond loopback.

```bash
aliasdeck serve
```

Same binary, same renderers, same validation. The server is an upgrade, never a prerequisite.

---

## Design decisions worth knowing

These are the choices that shape everything else. The reasoning lives in [`docs/PROJECT.md`](docs/PROJECT.md).

| Decision | Why |
| --- | --- |
| **The client renders, never the server** | Escaping belongs next to the shell that needs it. It also means an updated CLI can support new shells against a server nobody has touched in a year. |
| **The server transmits data, never shell code** | A compromised server should not be able to write arbitrary commands into every connected machine. |
| **Strict alias names, not escaped ones** | A name sits outside the quoted region of the generated line. Rather than escaping names, AliasDeck refuses the ones that would need it. |
| **Output has no timestamp** | Rendered files are byte-deterministic, so sync compares hashes instead of diffing. A generation time would make every sync look like a change. |
| **One config source per device** | No merging local and remote. Two sources of truth that silently reconcile is a bottomless pit of conflict bugs. |
| **Go everywhere** | One renderer package shared by the CLI and the server's UI preview. Two implementations of escaping logic would mean two sets of escaping bugs. |

---

## Roadmap

| Version | Scope |
| --- | --- |
| — | Renderer core, validation, golden tests ✅ |
| **v0.1** | Standalone CLI · zsh + bash · macOS + Linux ✅ |
| **v0.2** | PowerShell + Windows · Git-hosted config ✅ |
| **v0.3** | Self-hosted server · devices, profiles, REST API ✅ |
| v0.4 | Web UI with live rendered preview |
| later | Auto-sync, import/export, version history, Chezmoi backend, MCP server |

---

## Building from source

Requires Go 1.25 or newer.

```bash
git clone https://github.com/angeltonio/aliasdeck
cd aliasdeck
make check              # format, vet, test
go build -o aliasdeck ./cmd/aliasdeck
```

Useful targets:

```bash
make test      # tests only
make cover     # per-package coverage
make golden    # regenerate renderer golden files, then read the diff
```

### A note on the tests

The renderers are covered by golden files and by an integration test that sources AliasDeck's output in **real bash and zsh binaries**, feeding it four shell-injection payloads and proving the output stays inert.

That test exists because unit tests only confirm that the escaping logic does what its author believed. This project writes executable code into other people's shells, so the assumption itself has to be verified against the shells rather than against our reading of them.

If you contribute to the renderers or to validation, that is the test to keep green.

---

## Contributing

Early days — the architecture is still settling, so the most useful contribution right now is discussion. Open an issue if you disagree with something in [`docs/PROJECT.md`](docs/PROJECT.md); a wrong decision is cheapest to fix before there are users.

## License

[MIT](LICENSE).

Permissive on purpose. AliasDeck's main artifact is a CLI that people install on
their work machines, and a copyleft licence would put it behind a legal review
at exactly the companies where a shared alias set is most useful.
