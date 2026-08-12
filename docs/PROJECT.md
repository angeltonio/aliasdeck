# AliasDeck — Product & Architecture Specification

## 1. Vision

AliasDeck is a self-hosted control plane for managing shell aliases, reusable commands and related terminal configuration across multiple computers.

The user should be able to create or modify an alias once in a web interface and synchronize it to selected machines without manually editing `.zshrc`, `.bashrc`, PowerShell profiles or similar files.

The long-term vision is broader than a simple alias manager: AliasDeck should become a portable command layer for developers, homelab users and power users who work across several machines and operating systems.

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
5. Dotfile tools solve synchronization, but they do not provide a purpose-built graphical control plane for aliases and commands.
6. Managing aliases from an AI assistant or MCP client is difficult when the source of truth is an arbitrary local file.

AliasDeck solves this by introducing a centralized source of truth and a lightweight local agent.

---

## 3. Product principles

### 3.1 Self-hosted first

AliasDeck must run easily with Docker Compose and should not require a hosted SaaS service.

### 3.2 Native first, integrations second

The AliasDeck CLI must be capable of synchronizing aliases without Chezmoi or another dotfile manager.

Chezmoi should be supported as an optional backend/adapter for users who already use it.

### 3.3 Non-destructive

AliasDeck should avoid taking ownership of an entire `.zshrc` or `.bashrc` file.

Instead it should preferably manage a dedicated generated file, for example:

```text
~/.config/aliasdeck/aliases.zsh
~/.config/aliasdeck/aliases.bash
~/.config/aliasdeck/aliases.ps1
```

The user's shell config only needs a small bootstrap entry such as:

```bash
[ -f "$HOME/.config/aliasdeck/aliases.zsh" ] && source "$HOME/.config/aliasdeck/aliases.zsh"
```

This makes installation and removal safe.

### 3.4 Cross-platform model

Aliases should be stored in a neutral representation whenever possible and rendered into shell-specific syntax by adapters.

### 3.5 API-first

Everything available in the UI should eventually be available through the API and CLI.

This makes future MCP/AI integration straightforward.

---

## 4. MVP

The first usable version should include:

### Server

- Authentication
- CRUD for aliases
- CRUD for devices
- Profiles/groups
- Device targeting
- Platform targeting
- API tokens
- Sync endpoint
- Basic audit timestamps

### Web UI

- Login
- Dashboard
- Alias list
- Create/edit/delete alias
- Search and filtering
- Tags/groups
- Device list
- Device detail
- Profile management
- Sync status

### CLI

Commands:

```bash
aliasdeck login <server-url>
aliasdeck logout
aliasdeck register
aliasdeck sync
aliasdeck status
aliasdeck list
aliasdeck doctor
```

Initial supported environments:

- macOS + zsh
- Linux + bash
- Linux + zsh
- Windows + PowerShell

### Sync

MVP synchronization can be pull-based:

```text
AliasDeck Server
      │
      │ GET /api/v1/sync
      ▼
AliasDeck CLI
      │
      ├── detect platform
      ├── detect shell
      ├── render aliases
      └── write generated file
```

Automatic background sync can be added later.

---

## 5. Domain model

### Alias

Suggested fields:

```ts
interface Alias {
  id: string;
  name: string;
  command: string;
  description?: string;
  enabled: boolean;
  tags: string[];
  platforms: Platform[];
  shells: Shell[];
  profileIds: string[];
  deviceIds?: string[];
  createdAt: Date;
  updatedAt: Date;
}
```

Example:

```yaml
name: dcu
command: docker compose up -d
description: Start Docker Compose stack
platforms:
  - macos
  - linux
shells:
  - zsh
  - bash
tags:
  - docker
profiles:
  - development
```

### Device

```ts
interface Device {
  id: string;
  name: string;
  hostname: string;
  platform: 'macos' | 'linux' | 'windows';
  shell: 'zsh' | 'bash' | 'powershell';
  architecture?: string;
  profileIds: string[];
  lastSeenAt?: Date;
  lastSyncAt?: Date;
  clientVersion?: string;
}
```

### Profile

Profiles group configuration by purpose rather than machine.

Examples:

- Development
- Homelab
- Work
- Docker
- Kubernetes

A device can subscribe to multiple profiles.

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

PowerShell output:

For simple aliases, PowerShell's native `Set-Alias` does not support arbitrary command strings well, so AliasDeck should generate functions where necessary:

```powershell
function dps { docker ps @args }
```

This renderer abstraction is important because not every shell feature maps one-to-one.

Proposed interface:

```ts
interface ShellRenderer {
  supports(shell: Shell): boolean;
  render(config: ResolvedConfig): string;
}
```

---

## 7. Architecture

Recommended high-level architecture:

```text
                  ┌───────────────────────┐
                  │      AliasDeck        │
                  │       Web UI          │
                  └───────────┬───────────┘
                              │
                              ▼
                  ┌───────────────────────┐
                  │      AliasDeck API    │
                  │                       │
                  │ aliases               │
                  │ profiles              │
                  │ devices               │
                  │ auth/tokens           │
                  └───────────┬───────────┘
                              │
                       PostgreSQL/SQLite
                              │
              HTTPS           │
        ┌─────────────────────┴─────────────────────┐
        │                     │                     │
        ▼                     ▼                     ▼
  macOS CLI               Linux CLI            Windows CLI
     zsh                  bash / zsh            PowerShell
        │                     │                     │
        ▼                     ▼                     ▼
 generated config        generated config       generated config
```

---

## 8. Suggested stack

The exact stack can evolve, but an initial implementation could use:

### Monorepo

- pnpm workspaces
- Turborepo optional

### API

- NestJS
- Prisma
- PostgreSQL for production
- SQLite allowed for very small/self-hosted deployments if abstraction remains clean
- REST API initially
- OpenAPI documentation

### Web

- Next.js
- React
- Tailwind CSS
- shadcn/ui

### CLI

Two reasonable options:

1. TypeScript/Node.js for maximum code sharing with the API.
2. Go for an extremely portable single binary.

Preferred long-term choice: **Go**, because the agent should be trivial to distribute to macOS/Linux/Windows and should not require a Node runtime.

For MVP, TypeScript is acceptable if development speed matters more than distribution.

### Deployment

Docker Compose:

```text
aliasdeck-web
aliasdeck-api
postgres
```

A single-image deployment can be considered later.

---

## 9. Proposed repository structure

```text
aliasdeck/
├── apps/
│   ├── api/
│   ├── web/
│   └── cli/
├── packages/
│   ├── shared/
│   ├── shell-renderers/
│   └── sdk/
├── docs/
│   ├── PROJECT.md
│   ├── API.md
│   └── ARCHITECTURE.md
├── docker-compose.yml
├── README.md
└── LICENSE
```

If the CLI is written in Go:

```text
aliasdeck/
├── apps/
│   ├── api/
│   └── web/
├── cli/
│   └── cmd/
├── packages/
│   ├── shared/
│   └── sdk/
└── docs/
```

---

## 10. Chezmoi integration

Chezmoi should NOT be required for AliasDeck to work.

Instead AliasDeck should expose a backend abstraction:

```ts
interface SyncBackend {
  apply(config: ResolvedConfig): Promise<void>;
}
```

Initial implementations:

```text
NativeBackend
ChezmoiBackend
```

### Native backend

AliasDeck writes its own generated files and inserts/validates the shell bootstrap.

### Chezmoi backend

AliasDeck generates or updates files inside the user's Chezmoi-managed source directory and then delegates application to Chezmoi.

Potential command:

```bash
aliasdeck config set sync.backend chezmoi
aliasdeck sync
```

This allows existing Chezmoi users to keep their dotfile workflows while using AliasDeck as the control plane.

---

## 11. Security

Important design requirements:

- Device-specific API tokens
- Tokens stored securely by CLI
- TLS expected in production
- Secrets must not be embedded in aliases by default
- Sensitive environment variables should eventually use a separate secret mechanism
- Audit trail for configuration changes
- Device revocation
- Token rotation

Do not blindly execute arbitrary server-side commands during sync. The sync process should render configuration files; command execution should only happen through explicitly designed features.

---

## 12. Future features

After the MVP:

### Shell features

- Fish support
- Shell functions
- Parameterized commands
- Environment variables
- PATH entries
- Snippets
- SSH shortcuts

### Synchronization

- Automatic periodic synchronization
- Push notifications / websocket invalidation
- Config versioning
- Rollback
- Diff before apply
- Conflict detection

### Organization

- Shared/team aliases
- Role-based access control
- Multiple users
- Shared profiles

### Developer experience

- Import aliases from existing `.zshrc`, `.bashrc` or PowerShell profile
- Export configuration
- CLI autocomplete
- Homebrew package
- Scoop/WinGet package
- Debian/RPM packages
- Docker image

### Integrations

- Chezmoi
- Git repository export
- 1Password / Bitwarden / Vault for secret references
- MCP server

---

## 13. MCP / AI integration

AliasDeck should eventually expose MCP tools such as:

```text
list_aliases
get_alias
create_alias
update_alias
delete_alias
list_devices
list_profiles
assign_alias_to_profile
sync_device
```

Example interaction:

> Create an alias called `pve` that SSHs into my Proxmox host and enable it only for my Homelab profile on macOS and Linux.

The MCP layer should call the same application service/API used by the Web UI.

No MCP-only business logic.

---

## 14. MVP user flow

### Server installation

```bash
git clone <repo>
cd aliasdeck
docker compose up -d
```

### Device setup

```bash
aliasdeck login https://aliases.example.com
aliasdeck register --name macbook
aliasdeck sync
```

### UI

Create alias:

```text
Name: dcu
Command: docker compose up -d
Description: Start current Compose stack
Profiles: Development, Homelab
Platforms: macOS, Linux
Shells: zsh, bash
```

### Result

AliasDeck CLI receives the resolved config and writes:

```bash
alias dcu='docker compose up -d'
```

The alias becomes available after reload/source or through shell bootstrap behavior.

---

## 15. Milestones

### Milestone 1 — Foundation

- Monorepo
- API skeleton
- Web skeleton
- Database schema
- Docker Compose
- Authentication

### Milestone 2 — Alias management

- Alias CRUD
- Tags
- Profiles
- Platform/shell filters
- Web UI

### Milestone 3 — Device + sync

- Device registration
- Device tokens
- Sync endpoint
- CLI
- Bash/Zsh renderer
- Safe generated-file bootstrap

### Milestone 4 — Windows

- PowerShell renderer
- Windows device support
- CLI packaging

### Milestone 5 — Advanced

- Chezmoi adapter
- Import/export
- Version history
- Diff/rollback

### Milestone 6 — AI

- MCP server
- MCP authorization
- Agent-safe mutation APIs

---

## 16. Decisions for the first implementation

Recommended defaults:

- Project name: **AliasDeck**
- License: AGPL-3.0 or MIT — decide before public launch
- API: NestJS
- Web: Next.js
- Database: PostgreSQL
- ORM: Prisma
- CLI: Go preferred
- Deployment: Docker Compose
- First shells: zsh + bash
- Windows/PowerShell: immediately after first MVP sync works
- Native sync backend: required
- Chezmoi: optional adapter
- MCP: post-MVP

---

## 17. Non-goals for MVP

Do not initially build:

- Full dotfile management
- Remote command execution
- Terminal emulator
- SSH key management
- Secret manager
- Cloud-hosted SaaS
- Complex team RBAC
- Real-time collaboration

The first release should solve one problem extremely well:

> Create a command once and safely make it available on the machines where you want it.
