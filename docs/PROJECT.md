# AliasDeck — Product & Architecture Specification

## 1. Vision

AliasDeck is a portable command layer for people who work across several machines and operating systems.

It manages shell aliases, reusable commands and related terminal configuration from a single source of truth, and renders them into the correct syntax for each shell on each machine.

It works in two modes:

- **Standalone** — a single CLI reading a local or Git-hosted `aliases.yaml`. No server, no UI, no database.
- **Control plane** — a self-hosted server with a web UI and an API, managing many devices and profiles centrally.

Both modes use the same binary, the same renderers and the same validation. The server is an upgrade, never a prerequisite.

Tagline:

> **Your commands. Every machine.**

---

## 2. Problem

Aliases and shell helpers are normally stored locally in configuration files such as:

- `~/.zshrc`
- `~/.bashrc`
- `~/.config/fish/config.fish`
- PowerShell `$PROFILE`

This creates several problems:

1. Configuration becomes duplicated across machines.
2. Changes need to be copied manually.
3. Shell syntax differs between Bash/Zsh/Fish/PowerShell.
4. It is difficult to decide which aliases belong on which machines.
5. Dotfile tools solve file synchronization, but they do not understand what an alias *is*.
6. Managing aliases from an AI assistant or MCP client is difficult when the source of truth is an arbitrary local file.

### 2.1 Positioning — why not just use a dotfile manager?

Chezmoi, yadm and similar tools are excellent and AliasDeck does not try to replace them.

**They manage files. AliasDeck manages commands.**

A dotfile manager treats `~/.zshrc` as text to be templated and copied. It has no concept of an alias, so cross-shell support is the user's problem: to support an alias in both zsh and PowerShell you hand-write templates with conditionals, and you own the escaping.

AliasDeck treats the alias as a first-class entity:

```yaml
name: dps
command: docker ps
shells: [zsh, bash, powershell]
```

and knows that this renders as `alias dps='docker ps'` in zsh but must become a generated function in PowerShell, because `Set-Alias` cannot hold an arbitrary command string. See §6.3 for the exact form, which is less obvious than it looks.

The other structural difference is **targeting**. Dotfile managers target by machine, through hostname conditionals in templates. AliasDeck targets by **profile** — "Development", "Homelab", "Work" — which is how people actually think about their machines.

The two tools compose: AliasDeck can write into a Chezmoi-managed source directory, or its `aliases.yaml` can simply live in an existing dotfiles repository.

**AliasDeck is a cross-shell command compiler.** Synchronization is a feature, not the product.

---

## 3. Product principles

### 3.1 Local-first

The CLI must be fully useful with no server, no account and no network beyond fetching its own config. Installation is `brew install aliasdeck` followed by `aliasdeck init`.

This is not a degraded mode. It is the primary entry point, and it is what the project builds first.

### 3.2 Self-hosted second, and easy

When a user does want the control plane, the reference deployment is a **single static binary** serving the API and web UI, storing data in SQLite. Docker is a convenience, not a requirement.

Installation friction is the main adoption risk for self-hosted software. A three-service Compose stack is a worse first experience than `./aliasdeck serve`.

### 3.3 Native first, integrations second

The CLI must synchronize aliases without Chezmoi or any other dotfile manager. Chezmoi is an optional apply backend for users who already use it.

### 3.4 Non-destructive

AliasDeck never takes ownership of `.zshrc` or `.bashrc`. It manages a dedicated generated file:

```text
~/.config/aliasdeck/aliases.zsh
~/.config/aliasdeck/aliases.bash
~/.config/aliasdeck/aliases.ps1
```

The user's shell config only needs a small bootstrap entry:

```bash
[ -f "$HOME/.config/aliasdeck/aliases.zsh" ] && source "$HOME/.config/aliasdeck/aliases.zsh"
```

This makes installation and removal safe and reversible.

### 3.5 Cross-platform by rendering, not by branching

Aliases are stored in a neutral representation and rendered into shell-specific syntax by adapters. See section 6.

### 3.6 API-first

Everything available in the UI is available through the API and CLI. This makes MCP/AI integration a thin layer rather than a rewrite.

### 3.7 The server never ships shell code

The server transmits **data**. The client produces **shell syntax**. This is simultaneously a security boundary, a versioning boundary and the reason standalone mode is nearly free. See sections 6.1 and 7.

---

## 4. Scope

### 4.1 Standalone CLI — the first shippable product

- `aliases.yaml` as source of truth (local file or Git repository)
- Platform, shell and profile targeting
- Rendering for zsh, bash and PowerShell
- Safe generated file plus shell bootstrap management
- `init`, `sync`, `status`, `list`, `doctor`, `edit`, `uninstall`

No server. No database. No account.

### 4.2 Control plane — the upgrade

**Server**

- Authentication
- CRUD for aliases, devices and profiles
- Device and platform targeting
- Device API tokens
- Sync endpoint
- Audit timestamps

**Web UI**

- Login, dashboard
- Alias list, create/edit/delete
- Search, filtering, tags and groups
- Device list and detail
- Profile management
- Sync status
- Live preview of rendered output, per shell

### 4.3 Sync model

Pull-based. No daemon, no background service, no inbound ports on the user's machine.

```text
ConfigSource  (file | git | server)
      │
      │  neutral configuration + revision
      ▼
AliasDeck CLI
      │
      ├── detect platform
      ├── detect shell
      ├── resolve against device profiles
      ├── validate alias names and payload
      ├── render (local renderer package)
      ├── write generated file atomically (tmp + rename)
      └── record local state (revision, hash, timestamp)
```

Automatic background sync is deliberately deferred. A daemon means three service mechanisms (launchd, systemd, Task Scheduler), three failure modes and three support burdens. See section 13.

---

## 5. Domain model

### Alias

```go
type Alias struct {
    ID          string     `json:"id"`
    Name        string     `json:"name"`
    Command     string     `json:"command"`
    Description string     `json:"description,omitempty"`
    Enabled     bool       `json:"enabled"`
    Tags        []string   `json:"tags"`
    Platforms   []Platform `json:"platforms"`
    Shells      []Shell    `json:"shells"`
    ProfileIDs  []string   `json:"profileIds"`
    DeviceIDs   []string   `json:"deviceIds,omitempty"`
    CreatedAt   time.Time  `json:"createdAt"`
    UpdatedAt   time.Time  `json:"updatedAt"`
}
```

In standalone mode `ID` is derived from `Name`, and `DeviceIDs` is unused — the file *is* the device's scope.

### Device

```go
type Device struct {
    ID            string     `json:"id"`
    Name          string     `json:"name"`
    Hostname      string     `json:"hostname"`
    Platform      Platform   `json:"platform"`      // macos | linux | windows
    Shell         Shell      `json:"shell"`         // zsh | bash | powershell
    Architecture  string     `json:"architecture,omitempty"`
    ProfileIDs    []string   `json:"profileIds"`
    LastSeenAt    *time.Time `json:"lastSeenAt,omitempty"`
    LastSyncAt    *time.Time `json:"lastSyncAt,omitempty"`
    ClientVersion string     `json:"clientVersion,omitempty"`
}
```

In standalone mode the device is local and self-described by `config.yaml`; it is never registered anywhere.

### Profile

Profiles group configuration by purpose rather than by machine: Development, Homelab, Work, Docker, Kubernetes. A device subscribes to multiple profiles.

Profiles exist in both modes. They are the targeting primitive.

### ResolvedConfig

The output of resolution and the input to rendering. This is the contract every `ConfigSource` must satisfy.

```go
type ResolvedConfig struct {
    Revision  string    // content hash or server revision
    Device    Device
    Aliases   []Alias   // already filtered by platform, shell and profile
    GeneratedAt time.Time
}
```

---

## 6. Rendering model

Neutral alias:

```yaml
name: dps
command: docker ps
```

Bash/Zsh output:

```bash
alias dps='docker ps'
```

PowerShell output — `Set-Alias` cannot hold an arbitrary command string, so a function is generated:

```powershell
function dps {
    $__aliasdeck_cmd = 'docker ps'
    & ([scriptblock]::Create($__aliasdeck_cmd + ' @args')) @args
}
```

Renderer interface:

```go
type ShellRenderer interface {
    Supports(shell Shell) bool
    Render(cfg ResolvedConfig) (string, error)
}
```

### 6.1 Where rendering happens — DECIDED: in the client

**The source returns a neutral resolved configuration. The CLI renders it into shell syntax.**

Three reasons, in order of weight:

**1. Version skew is guaranteed, not hypothetical.**

AliasDeck is a public, self-hosted project. Users install a server once and update it rarely, while updating the CLI through Homebrew or Scoop regularly. The versions *will* diverge.

If the server rendered, adding a new shell (fish, nushell) would require every user to upgrade their server first. The roadmap becomes hostage to the oldest deployed server. With client-side rendering, an updated CLI gains fish support against a year-old server, because that server never needed to know what fish is.

**2. Quoting and escaping is the attack surface.**

Escaping rules are shell-specific. The component that knows the target shell must be the component that escapes. If the server sent pre-rendered shell text, a compromised or buggy server would write arbitrary shell code into every connected machine. Sending structured data means the client validates and escapes before touching disk.

**3. It makes standalone mode nearly free.**

If rendering lives in the client, the server is just one possible supplier of `ResolvedConfig`. A local file is another. This single decision is what allows sections 3.1 and 7 to exist at almost no additional cost.

### 6.2 The shared renderer package

Renderers live in a package imported by **both** the server and the CLI:

```text
internal/renderers/    →  CLI (authoritative write) + API (UI preview)
```

The web UI's preview calls the same code that writes the file. No duplication, no drift between what the UI shows and what lands on disk.

This is only possible because server and CLI share a language, and it is the primary reason the backend is Go rather than TypeScript (section 9.1).

**The CLI's render is authoritative.** Server-side preview is a UI convenience and is never written to a device.


### 6.3 Why PowerShell does not inline the command

The obvious form, `function dps { docker ps @args }`, is unsafe and was the
documented design until it was tested against a real PowerShell.

In POSIX the command body sits inside a quoted string, so escaping one
character makes it inert. In that inlined PowerShell form nothing is quoted at
all: the command is raw code inside a function block. A command containing `}`
closes the block early, and whatever follows executes **when the file is
sourced** — before the user has typed the alias.

Verified against PowerShell 7.6.4: an alias whose command contained `}` ran an
injected `New-Item` at source time.

Wrapping the command in a single-quoted string restores the property POSIX
already had — **the command is data at definition time and code at call time**,
exactly like `alias x='...'`, which runs nothing when sourced. `scriptblock::Create`
compiles it only when the function is invoked, which is what the user asked for.

The escaping rule follows from the quoting: single-quote the command and double
every embedded `'`. Same shape as the POSIX renderer's `'` → `'\''`, different
mechanism.

**`@args` appears twice, and both are necessary.** The second one passes the
caller's arguments to the script block; the first, inside the compiled string,
is what makes the command actually receive them. Compiling the command alone
and splatting at the invocation looks equivalent and silently drops every
argument — an alias for `git checkout` would ignore the branch name. Verified
on Windows PowerShell 5.1 and PowerShell 7.

### 6.4 PowerShell editions are not interchangeable

Windows ships **PowerShell 5.1 (Desktop)** by default. **PowerShell 7 (Core)**
is a separate installation, and the two resolve `$PROFILE` to different paths:

```text
5.1   ~\Documents\WindowsPowerShell\Microsoft.PowerShell_profile.ps1
7     ~\Documents\PowerShell\Microsoft.PowerShell_profile.ps1
```

A user can have both installed. Bootstrapping the wrong profile leaves aliases
that never load, with nothing obviously wrong to look at.

Both editions were verified to inject on the inlined form and to contain on the
corrected one, so the renderer does not need to branch on edition — but rc-file
detection does.

---

## 7. Configuration sources

The CLI does not know or care where configuration comes from. It depends on one interface:

```go
type ConfigSource interface {
    Resolve(ctx context.Context, dev Device) (ResolvedConfig, error)
}
```

Implementations:

| Source | Origin | Mode |
| --- | --- | --- |
| `FileSource` | local `aliases.yaml` | standalone |
| `GitSource` | `aliases.yaml` in a Git repository | standalone |
| `ServerSource` | `GET /api/v1/sync` | control plane |

`FileSource` and `GitSource` resolve locally: read, filter by the device's platform, shell and profiles, hash the result. `ServerSource` delegates that same resolution to the server and receives the result.

Everything downstream — validation, rendering, atomic write, state recording — is identical.

### 7.1 One source per device — hard rule

A device is bound to exactly one source, declared explicitly in its config. There is no merging, no fallback chain and no automatic reconciliation between local and remote configuration.

Two sources of truth that silently merge is a bottomless pit of conflict-resolution bugs, and it makes `aliasdeck doctor` unable to answer the only question that matters: *where did this alias come from?*

`aliasdeck status` always reports the active source.

Switching sources is an explicit, single command:

```bash
aliasdeck config set source.type server
```

### 7.2 `aliases.yaml`

The standalone source of truth. Designed to be readable, diffable and comfortable inside an existing dotfiles repository.

```yaml
version: 1

profiles:
  - development
  - homelab

aliases:
  - name: dcu
    command: docker compose up -d
    description: Start Docker Compose stack
    platforms: [macos, linux]
    shells: [zsh, bash]
    tags: [docker]
    profiles: [development]

  - name: dps
    command: docker ps
    shells: [zsh, bash, powershell]
    profiles: [development, homelab]

  - name: pve
    command: ssh root@proxmox.local
    platforms: [macos, linux]
    shells: [zsh]
    profiles: [homelab]
```

Omitted `platforms` or `shells` means "all supported". Omitted `profiles` means "always active".

### 7.3 `config.yaml`

Per-device configuration at `~/.config/aliasdeck/config.yaml`. This file is local, never synchronized, and defines the device's identity and its source.

```yaml
version: 1

device:
  name: macbook
  profiles: [development, homelab]

source:
  type: file            # file | git | server
  path: ~/dotfiles/aliases.yaml

backend: native         # native | chezmoi
```

Server mode replaces the `source` block:

```yaml
source:
  type: server
  url: https://aliases.example.com
  # token stored separately at 0600, never in this file
```

---

## 8. Architecture

### 8.1 Standalone

```text
  aliases.yaml  ──►  FileSource / GitSource
                            │
                            ▼
                     ┌──────────────┐
                     │ aliasdeck    │
                     │   resolve    │
                     │   validate   │
                     │   render     │
                     │   write      │
                     └──────┬───────┘
                            ▼
                  ~/.config/aliasdeck/aliases.zsh
```

### 8.2 Control plane

```text
                  ┌──────────────────────────────────────┐
                  │      aliasdeck serve                 │
                  │      (single static binary)          │
                  │                                      │
                  │  ┌────────────────────────────────┐  │
                  │  │  embedded web UI (embed.FS)    │  │
                  │  ├────────────────────────────────┤  │
                  │  │  HTTP API                      │  │
                  │  │  aliases / profiles / devices  │  │
                  │  │  auth / tokens / sync          │  │
                  │  ├────────────────────────────────┤  │
                  │  │  internal/renderers (preview)  │  │
                  │  └────────────────────────────────┘  │
                  └──────────────────┬───────────────────┘
                                     │
                            SQLite (default)
                            PostgreSQL (optional)
                                     │
                     HTTPS · neutral JSON · no shell code
        ┌────────────────────────────┼────────────────────────────┐
        ▼                            ▼                            ▼
   macOS CLI                    Linux CLI                   Windows CLI
      zsh                       bash / zsh                  PowerShell
        │                            │                            │
        └──── internal/renderers ────┴──── internal/renderers ─────┘
                          (authoritative render)
        │                            │                            │
        ▼                            ▼                            ▼
 generated config             generated config            generated config
```

Server and CLI compile from the same module and share renderers, domain types and validation.

---

## 9. Stack

### 9.1 Language: Go, everywhere

The original draft proposed NestJS for the API and Go for the CLI. That split is rejected:

1. **Renderer duplication.** With a TypeScript API, renderers must exist twice — TS for UI preview, Go for the CLI — or preview is dropped. Two implementations of escaping logic is two sets of escaping bugs.
2. **Distribution.** Local-first and self-hosted-first both want a single binary with no runtime dependency. Go delivers that; Node does not.
3. **Contributor surface.** One language, one toolchain, one test command for a public project.

**What this costs:** slower CRUD development than Prisma + NestJS, and the loss of shared types between API and web. The latter is recovered by generating a TypeScript client from the OpenAPI spec, which was needed anyway.

### 9.2 CLI

- Go, `cobra` for command structure
- `go.yaml.in/yaml/v3` for YAML parsing (`aliases.yaml`, `config.yaml`) — not `gopkg.in/yaml.v3`, which go-yaml archived in April 2025 and froze at v3.0.1
- Owns `internal/renderers`, `internal/validate` and the `ConfigSource` implementations
- Static binary per platform/architecture
- Packaging: Homebrew, Scoop, `.deb`/`.rpm`, tarball

Milestone 1 (the renderer core: `internal/domain`, `internal/renderers`, `internal/validate`) is stdlib-only by design — it is imported by both the CLI and a future server, so it stays free of choices either side would have to inherit. `cobra` and `go.yaml.in/yaml/v3` above are AliasDeck's first two runtime dependencies, both introduced in Milestone 2 once an actual CLI needed flag parsing and config parsing.

### 9.3 Server

- Go, current stable release
- Routing: stdlib `net/http` with method-aware patterns (Go 1.22+); `chi` acceptable if middleware ergonomics justify the dependency
- Persistence: SQLite by default, PostgreSQL optional behind a repository interface
- SQLite driver: **`modernc.org/sqlite` (pure Go, no cgo)** — required so `CGO_ENABLED=0` cross-compilation to darwin/linux/windows on amd64/arm64 stays trivial. A cgo-dependent driver such as `mattn/go-sqlite3` would compromise the release pipeline
- Queries: `sqlc` for type-safe generated code; no ORM
- Migrations: `goose` or `golang-migrate`, embedded and applied on startup
- API: REST under `/api/v1`, documented with OpenAPI

### 9.4 Web

- **Vite + React** (not Next.js), TypeScript, Tailwind CSS, shadcn/ui
- Built to static assets, embedded into the Go binary via `embed.FS`

**Why not Next.js:** Next earns its complexity through SSR, routing and server components, all of which need a Node runtime at serve time. Embedding it in a Go binary means static export, which discards most of that value while keeping the framework weight. An authenticated control panel is honestly an SPA. Node stays a build-time dependency only.

### 9.5 Build and release

- `goreleaser` for cross-compiled artifacts
- Web assets built before the Go build and embedded
- Docker image as a thin wrapper around the same binary

---

## 10. Repository structure

```text
aliasdeck/
├── cmd/
│   └── aliasdeck/          # the single binary: CLI and `aliasdeck serve`
├── internal/
│   ├── domain/             # entities, shared by server and CLI
│   ├── renderers/          # shell renderers (bash, zsh, powershell)
│   ├── validate/           # alias name and payload validation
│   ├── source/             # ConfigSource: file, git, server
│   ├── apply/              # atomic write, bootstrap, native + chezmoi backends
│   ├── config/             # config.yaml and aliases.yaml parsing
│   ├── api/                # HTTP handlers, middleware, routing
│   ├── store/              # repository interfaces + sqlite/postgres impls
│   ├── auth/               # sessions, device tokens
│   └── sync/               # server-side resolution
├── web/                    # Vite + React app, built into internal/api/static
├── migrations/
├── docs/
│   ├── PROJECT.md
│   ├── API.md
│   └── ARCHITECTURE.md
├── .goreleaser.yaml
├── Dockerfile
├── README.md
└── LICENSE
```

---

## 11. Chezmoi integration

Chezmoi is never required. It sits behind an apply-time interface in the CLI:

```go
type SyncBackend interface {
    Name() string
    OutputPath(dev Device) (string, error)
    Apply(ctx context.Context, cfg ResolvedConfig, rendered string) error
}
```

Implementations: `NativeBackend`, `ChezmoiBackend`.

- **Native** — writes the generated file and manages the shell bootstrap.
- **Chezmoi** — writes into the user's Chezmoi source directory and delegates application to Chezmoi.

```bash
aliasdeck config set backend chezmoi
aliasdeck sync
```

The interface ships in the MVP. `ChezmoiBackend` is implemented on demand, not preemptively.

Note that `SyncBackend` (how output is applied) is orthogonal to `ConfigSource` (where input comes from). A user can read from a server and apply through Chezmoi, or read from a Git repo and apply natively.

---

## 12. Security

- Device-specific API tokens, hashed at rest
- Tokens stored by the CLI with `0600` permissions, OS keychain later
- TLS expected in production
- Secrets must not be embedded in aliases by default
- Sensitive environment variables use a separate secret mechanism (post-MVP)
- Audit trail for configuration changes
- Device revocation and token rotation

### 12.1 Client-side validation is mandatory

Because the client renders, the client is the last line of defense. This applies to *every* source — a malicious `aliases.yaml` pulled from a Git repo deserves the same scrutiny as a compromised server. Before writing anything:

- **Alias names** must match a strict identifier pattern for the target shell. A name containing quotes, newlines, `;`, `$` or control characters can break out of the generated construct and corrupt the user's shell configuration
- **Commands** must be escaped by the renderer for the target shell, never concatenated
- **Payload size** must be bounded
- Anything failing validation is skipped and reported by `aliasdeck doctor`, never silently written

### 12.2 Sync never executes

Sync renders and writes configuration files. It does not execute source-supplied content. Execution happens only when the user sources their shell, which is their explicit act.

The generated file is written atomically (temp file + rename) so an interrupted sync can never leave a truncated file that a shell will try to source.

---

## 13. Future features

### Shell features

- Fish and Nushell support
- Shell functions, parameterized commands
- Environment variables, PATH entries
- Snippets, SSH shortcuts

### Synchronization

- **Opportunistic auto-sync** — the generated file carries a non-blocking TTL check that triggers a background sync when stale. Automatic synchronization without a daemon. It must never add measurable latency to shell startup; a slow shell is an uninstall
- Conditional requests (`ETag` / `If-None-Match`) so a no-op sync is cheap
- Push invalidation via websocket
- Config versioning, rollback, diff before apply, conflict detection
- A real background daemon, only if opportunistic sync proves insufficient

### Organization

- Shared/team aliases, RBAC, multiple users, shared profiles

### Developer experience

- Import from existing `.zshrc`, `.bashrc` or PowerShell profile
- Export configuration (including server → `aliases.yaml`, so users can always leave)
- CLI autocomplete
- Homebrew, Scoop/WinGet, Debian/RPM packages, Docker image

### Integrations

- Chezmoi, Git repository export
- 1Password / Bitwarden / Vault for secret references
- MCP server

---

## 14. MCP / AI integration

Eventually exposed MCP tools:

```text
list_aliases      get_alias         create_alias
update_alias      delete_alias      list_devices
list_profiles     assign_alias_to_profile
sync_device
```

Example interaction:

> Create an alias called `pve` that SSHs into my Proxmox host and enable it only for my Homelab profile on macOS and Linux.

The MCP layer calls the same application services used by the Web UI and the API. No MCP-only business logic.

---

## 15. User flows

### 15.1 Standalone — the default path

```bash
brew install aliasdeck
aliasdeck init                    # creates config.yaml + aliases.yaml, adds shell bootstrap
aliasdeck edit                    # opens aliases.yaml in $EDITOR
aliasdeck sync
```

Pointing at an existing dotfiles repository instead:

```bash
aliasdeck init --source ~/dotfiles/aliases.yaml
aliasdeck sync
```

### 15.2 Control plane

Server:

```bash
curl -sSL https://.../install.sh | sh
./aliasdeck serve
```

Device:

```bash
aliasdeck login https://aliases.example.com
aliasdeck register --name macbook
aliasdeck sync
```

Creating an alias in the UI:

```text
Name: dcu
Command: docker compose up -d
Description: Start current Compose stack
Profiles: Development, Homelab
Platforms: macOS, Linux
Shells: zsh, bash
```

### 15.3 Result, in both cases

```bash
alias dcu='docker compose up -d'
```

Available after reload/source, or through shell bootstrap behavior.

---

## 16. Milestones

Ordered so that the hardest and most dangerous code is written and tested first, and so that a genuinely useful product ships before any server exists.

### Milestone 1 — Renderer core

- Go module and repository layout
- `internal/domain` types
- `internal/renderers` for zsh and bash
- `internal/validate` — alias name rules, escaping, size bounds
- Golden-file tests for every renderer

Nothing user-facing. All of the project's delicate logic, tested in isolation.

### Milestone 2 — Standalone CLI · **v0.1, first release**

- `aliases.yaml` and `config.yaml` schemas
- `FileSource` and local resolution
- `internal/apply`: atomic write, shell bootstrap management
- CLI: `init`, `sync`, `status`, `list`, `doctor`, `edit`
- `goreleaser`, Homebrew tap, install script

A complete, useful tool on macOS and Linux. Zero server, zero UI.

### Milestone 3 — Windows and Git · **v0.2, released**

- PowerShell renderer and Windows support
- Scoop packaging, published to `angeltonio/scoop-bucket`
- `GitSource`

The standalone product now covers all three operating systems and composes with existing dotfiles repositories, and installs from Homebrew, Scoop or the install script. A known limitation surfaced during this milestone: on Windows, `config.yaml` and `state.json` (which can hold a credential-bearing Git URL) are written with the same `0600`-mode call as on POSIX, but Windows reports every writable file as `0666` regardless of the requested mode — there is no Unix-style owner-only protection on that platform via this mechanism. Real protection would require Windows ACL manipulation, which this milestone does not add.

### Milestone 4 — Server · **v0.3**

- `aliasdeck serve`, SQLite schema and embedded migrations
- Authentication, device registration, device tokens
- Alias/profile/device CRUD over REST, OpenAPI spec
- Server-side resolution and sync endpoint
- `ServerSource` in the CLI

### Milestone 5 — Web UI · **v0.4**

- Vite + React app embedded via `embed.FS`
- Alias, profile and device management
- Search, filtering, tags
- Sync status
- Live rendered preview using the shared renderer package

### Milestone 6 — Advanced

- Opportunistic auto-sync
- Import from existing shell config, export to `aliases.yaml`
- Version history, diff, rollback
- Chezmoi backend

### Milestone 7 — AI

- MCP server, MCP authorization, agent-safe mutation APIs

---

## 17. Decisions for the first implementation

Settled as of 2026-08-12:

| Area | Decision |
| --- | --- |
| Project name | AliasDeck |
| Positioning | Cross-shell command compiler, not a dotfile manager |
| Build order | **Local-first** — standalone CLI ships before the server exists |
| Server language | Go |
| CLI language | Go |
| Rendering location | **Client (CLI)** — no source ever emits shell code |
| Renderer sharing | Shared Go package, used by CLI and by server-side preview |
| Config input | `ConfigSource` interface: file, git, server |
| Source binding | Exactly one source per device, explicit, no merging |
| Standalone format | `aliases.yaml` (source) + `config.yaml` (device-local) |
| Web | Vite + React + Tailwind + shadcn/ui, embedded via `embed.FS` |
| Database | SQLite by default (pure-Go driver), PostgreSQL optional |
| Query layer | `sqlc`, no ORM |
| Deployment | Single static binary; Docker as convenience |
| First shells | zsh + bash, PowerShell in v0.2 |
| Sync model | Pull-based, manual, no daemon |
| Native apply backend | Required |
| Chezmoi | Optional apply backend, interface only in MVP |
| MCP | Post-MVP |
| License | **MIT** — the CLI is installed on work machines, and copyleft would put it behind corporate legal review where the tool is most useful |
| Windows path shape (v0.2) | Generated `.ps1` files use LF unconditionally, same as zsh/bash. Bootstrap lines emitted into `$PROFILE` always use forward slashes (`filepath.ToSlash`), which PowerShell accepts natively; the rc file's own pre-existing line ending (LF or CRLF) is detected and preserved rather than forced. `config.ExpandPath` additionally recognizes a literal `~\` prefix alongside `~/`, independent of the host OS |
| PowerShell edition handling (v0.2) | Never write to both `$PROFILE` locations. Precedence: `--rc-file` → `$ALIASDECK_PWSH_PROFILE` → `LookPath("pwsh")` ⇒ Core → `LookPath("powershell")` ⇒ Desktop → Core default. `doctor` warns, without writing, when the *other* edition's profile also exists |
| Git source read-only in v0.2 (`GitSource`) | Clone or fetch only — AliasDeck never commits, pushes, or otherwise mutates a user's Git repository. Cached at a hashed, AliasDeck-owned path so the URL itself never becomes a directory name. Offline with an existing cache resolves the last-known content and reports staleness; offline with no cache is a hard error naming the source |
| Windows file-mode security limitation (v0.2, known gap) | `config.yaml`/`state.json` are still written with the same `0600` call as on POSIX, but Windows reports every writable file as `0666` regardless of the requested mode — there is no owner-only protection for a credential-bearing Git URL on that platform via this mechanism. ACL-based protection is out of scope for v0.2 |

---

## 18. Non-goals for MVP

Not built initially:

- Full dotfile management
- Remote command execution
- Terminal emulator
- SSH key management
- Secret manager
- Cloud-hosted SaaS
- Complex team RBAC
- Real-time collaboration
- A background sync daemon
- Merging or reconciling multiple configuration sources
- Chezmoi backend implementation

The first release solves one problem extremely well:

> Define a command once and safely make it available, in the right syntax, on the machines where you want it.
