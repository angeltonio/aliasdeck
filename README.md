# AliasDeck

> Your commands. Every machine.

AliasDeck is a cross-shell command compiler. You define an alias once, in a neutral format, and AliasDeck renders it into the correct syntax for each shell on each of your machines — macOS, Linux and Windows.

No more copying blocks between `.zshrc`, `.bashrc` and PowerShell profiles.

## Two ways to use it

**Standalone** — a single CLI reading an `aliases.yaml`. No server, no account, no database.

```bash
brew install aliasdeck
aliasdeck init
aliasdeck edit
aliasdeck sync
```

**Control plane** — a self-hosted server with a web UI and API, managing many devices and profiles centrally.

```bash
./aliasdeck serve
```

Same binary, same renderers, same validation. The server is an upgrade, never a prerequisite.

## Status

Early design. See [`docs/PROJECT.md`](docs/PROJECT.md) for the full product and architecture specification.

## How it works

```yaml
# aliases.yaml
aliases:
  - name: dps
    command: docker ps
    shells: [zsh, bash, powershell]
    profiles: [development]
```

Becomes `alias dps='docker ps'` on zsh and bash, and `function dps { docker ps @args }` on PowerShell — because `Set-Alias` cannot express it.

Output is written to a dedicated generated file, never into your shell config:

```text
~/.config/aliasdeck/aliases.zsh
```

Your `.zshrc` only needs one bootstrap line, so installing and uninstalling are both safe.

## How is this different from Chezmoi?

Chezmoi and similar tools are excellent, and AliasDeck does not replace them.

**They manage files. AliasDeck manages commands.**

A dotfile manager sees `~/.zshrc` as text to template and copy — it has no concept of an alias, so cross-shell support means hand-writing conditionals and owning the escaping yourself. AliasDeck treats the alias as a first-class entity and knows how each shell needs it expressed.

The other difference is targeting: dotfile managers target by machine via hostname conditionals, AliasDeck targets by **profile** — Development, Homelab, Work.

The two compose. Your `aliases.yaml` can live in your existing dotfiles repo, and AliasDeck can apply through Chezmoi.

## Design principle

The configuration source transmits **data**, never shell code. The CLI validates, escapes and renders it locally.

This keeps escaping logic next to the shell that needs it, lets an updated CLI support new shells against an old server, and is what makes standalone mode possible at almost no extra cost.

## Stack

- **Server and CLI**: Go — one language, one toolchain, a shared renderer package
- **Web**: Vite + React + Tailwind + shadcn/ui, embedded into the binary
- **Storage**: SQLite by default, PostgreSQL optional
- **Deployment**: a single static binary; Docker as a convenience, not a requirement

## Roadmap

| Version | Scope |
| --- | --- |
| v0.1 | Standalone CLI, zsh + bash, macOS + Linux |
| v0.2 | PowerShell + Windows, Git-hosted config |
| v0.3 | Self-hosted server, devices, profiles, API |
| v0.4 | Web UI with live rendered preview |
| later | Auto-sync, import/export, versioning, Chezmoi, MCP |

## CLI

```bash
aliasdeck init          # set up config and shell bootstrap
aliasdeck edit          # edit aliases.yaml
aliasdeck sync          # render and write
aliasdeck status        # active source, last sync, revision
aliasdeck list          # what is active on this machine
aliasdeck doctor        # diagnose config and validation problems
```

Control plane additions:

```bash
aliasdeck login https://aliases.example.com
aliasdeck register --name macbook
```

## License

TBD — AGPL-3.0 or MIT, decided before public launch.
