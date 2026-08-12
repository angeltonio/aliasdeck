# AliasDeck

> Your commands. Every machine.

You write an alias once. AliasDeck compiles it into the right syntax for every shell you use — zsh, bash, PowerShell — and writes it to each machine without touching your `.zshrc`.

It is not a dotfile manager. Dotfile managers copy files; AliasDeck understands what an alias *is*, which is why it can turn one definition into three different shells.

---

## ⚠️ Status: early development — not usable yet

**There is no installable binary.** Do not expect `brew install` to work; the Homebrew tap does not exist.

What is built today is the core that everything else depends on:

| Component | State |
| --- | --- |
| Domain model and resolution | ✅ Done |
| Validation (name/command/description safety) | ✅ Done |
| zsh + bash renderers | ✅ Done |
| PowerShell renderer | ⬜ Planned |
| CLI (`init`, `sync`, `status`, …) | ⬜ Next |
| Server + web UI | ⬜ Later |

You can read the code, run the tests and follow along. You cannot manage your aliases with it. Watch the repo if you want to know when v0.1 lands.

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
function dps { docker ps @args }
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

## Two ways it will work

**Standalone** — one CLI, one `aliases.yaml`. No server, no account, no database. This is the primary way to use AliasDeck and the first thing being built.

```bash
aliasdeck init
aliasdeck edit
aliasdeck sync
```

**Control plane** — a self-hosted server with a web UI and API, for managing many machines centrally. A single static binary with SQLite; Docker optional.

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
| **v0.1** | Standalone CLI · zsh + bash · macOS + Linux |
| v0.2 | PowerShell + Windows · Git-hosted config |
| v0.3 | Self-hosted server · devices, profiles, REST API |
| v0.4 | Web UI with live rendered preview |
| later | Auto-sync, import/export, version history, Chezmoi backend, MCP server |

---

## Building from source

Requires Go 1.25 or newer. There is no binary to build yet — this runs the library and its tests.

```bash
git clone https://github.com/angeltonio/aliasdeck
cd aliasdeck
make check     # format, vet, test
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

**Not yet licensed.** Until a LICENSE file lands, default copyright applies and this code carries no usage grant. The choice is between AGPL-3.0 and MIT and will be made before v0.1.
