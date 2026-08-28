# AliasDeck

> Your commands. Every machine.

You teach `gs` to mean `git status` on your laptop. Then you SSH into a
server and it isn't there. You copy your shell config across, and the machine
running bash gets zsh syntax. You put your dotfiles in Git, and every new
machine still needs the same manual repair — plus a `git pull` you will
forget on the machines you use least.

AliasDeck lets you define an alias once, manage it locally or from a
self-hosted control plane, and render it safely for zsh, bash, or PowerShell.

It understands commands rather than copying dotfiles, so every client writes
the syntax its own shell needs — and **the server never sends shell code**. It
transmits what an alias means; each machine decides how to spell it. That
boundary is enforced by tests, not by convention: the client binary cannot
import a single server package, and the same alias renders byte-for-byte
identically whether it came from a local file or from the server.

AliasDeck ships two separate programs:

| | `aliasdeck` | `aliasdeck-server` |
| --- | --- | --- |
| Purpose | Client installed on each machine | Self-hosted control plane, web UI, and REST API |
| Installation | Homebrew, Scoop, install script, or release archive | Docker Compose (recommended) or release archive |
| Network listener | None | Yes |
| Required for standalone use | Yes | No |

Full product reasoning lives in
[`docs/WHAT-WE-ARE-BUILDING.md`](docs/WHAT-WE-ARE-BUILDING.md).

## Quick start: server and macOS client

The server deployment and the client installation are independent steps. Run
the server once on a machine that stays on, then install and enroll the client
on every Mac you want to synchronize.

### 1. Deploy the server

Prerequisites: Git, Docker Engine, and Docker Compose v2.

The production Compose file lives in this repository, so run it from a clone:

```bash
git clone https://github.com/angeltonio/aliasdeck.git
cd aliasdeck
docker compose -f compose.prod.yaml up -d --pull always --wait
```

Open <http://127.0.0.1:8088/setup> and create the first operator. The
production file is deliberately pinned to the published
[`v0.6.1`](https://github.com/angeltonio/aliasdeck/releases/tag/v0.6.1)
server image and digest.

### 2. Install or update the client on macOS

Install the published Homebrew cask:

```bash
brew install --cask angeltonio/tap/aliasdeck
```

Update an existing installation:

```bash
brew update
brew upgrade --cask aliasdeck
```

Homebrew reports that the latest version is already installed when there is
nothing to upgrade.

### 3. Enroll the Mac

1. Sign in to the web UI.
2. Open **Devices → Add device**.
3. Choose whether automatic alias synchronization should be enabled and select
   its interval.
4. Mint the single-use enrollment token and run the generated command on the
   new Mac.

The generated command initializes the client, exchanges the short-lived token
for a device credential, performs the first sync, optionally installs the
macOS background agent, and loads the generated aliases into the current
shell. Do not copy enrollment tokens into documentation or logs.

`aliasdeck init` adds one reversible shell bootstrap after confirmation. A zsh
session also notices watcher-written updates at its next prompt. Other shells
load an updated generated file in a new session or when it is sourced again.

> Automatic background agent installation is currently supported only on
> macOS. Linux and Windows clients can synchronize manually with
> `aliasdeck sync`; do not treat the macOS LaunchAgent as cross-platform.

## Production operations

Run these commands from the repository directory that contains
`compose.prod.yaml`. The fixed Compose project name is `aliasdeck-prod`.

### Status and health

```bash
docker compose -f compose.prod.yaml ps
curl -fsS http://127.0.0.1:8088/api/v1/health
```

The separate health service exists because the minimal server image contains
no shell or HTTP client.

### Logs

```bash
docker compose -f compose.prod.yaml logs --follow aliasdeck-server
```

### Update and recreate

Back up the persistent data before upgrading. Database migrations are
forward-only; restoring a pre-upgrade backup is the rollback path.

```bash
git pull --ff-only
docker compose -f compose.prod.yaml up -d --pull always --wait
```

The image is pinned in `compose.prod.yaml`, so updating the checkout is what
moves the deployment to a newer reviewed pin when the repository publishes
one.

### Stop while preserving data

```bash
docker compose -f compose.prod.yaml down
```

Operator accounts, aliases, devices, and tokens remain in the named Docker
volume `aliasdeck-prod_aliasdeck-data`. The database is stored at
`/data/server.db` inside that volume.

### Recover a lost operator password

If you can no longer log in, reset the password instead of deleting the
volume. This keeps every alias, device, and token:

```bash
docker compose -f compose.prod.yaml exec aliasdeck-server \
  aliasdeck-server reset-password --db /data/server.db
```

The new password is generated and written to `/data/reset-password.txt` at
mode `0600` — read it, then remove the file. To choose the password yourself,
supply it in the environment instead, in which case nothing is printed or
written:

```bash
docker compose -f compose.prod.yaml exec \
  -e ALIASDECK_ADMIN_PASSWORD='your-new-password' aliasdeck-server \
  aliasdeck-server reset-password --db /data/server.db
```

The password must be at least 12 characters — the same floor first-run setup
applies. It is never accepted as a command-line flag, because arguments are
visible to other processes on the host and are recorded in shell history.

The reset also **revokes every session that operator holds**, so anyone
already logged in as them is signed out. That is deliberate: if you are
resetting because the password may be known to someone else, leaving their
session open would make the reset cosmetic. The server does not need to be
stopped, and `--username` targets a different account if you have one.

### Replace a device's credential without re-enrolling it

If a device token may have leaked but the machine is still yours, rotate the
credential instead of revoking the device. Rotating keeps the machine's
server-side identity; enrolling it again would create a second device, so any
alias aimed at the first one would stop reaching it and its history would
restart.

Rotate through the REST API to obtain the replacement, then adopt it on the
machine:

```bash
aliasdeck register --url https://aliases.example.com \
  --device-token '<the rotated token>' --force
```

`--force` is required because this replaces a credential that already works.
The token is verified against the server before anything local is written, so
a token that does not authenticate cannot take the machine offline.

### Reset everything

> **DANGER — permanent data loss:** this deletes the production volume and
> every operator, alias, device, and token stored in it. The next start returns
> to `/setup`. Do not run it as a normal restart or update command. To recover
> a forgotten password, use `reset-password` above instead — it keeps your data.

```bash
docker compose -f compose.prod.yaml down --volumes
```

Start again with the quick-start command after the destructive reset.

## Security boundary

The production Compose file publishes the server only on
`127.0.0.1:8088`. Its `ALIASDECK_LOCAL_SETUP=true` setting is safe only while
that host-port boundary remains loopback-only: it permits the direct local
`/setup` flow even though Docker bridge traffic is not loopback inside the
container.

Complete first-run setup through the local URL before exposing the service.
The credentialless local setup path rejects requests carrying proxy metadata;
remote or proxied setup does not inherit local trust.

For remote access:

1. Keep the Compose port bound to loopback.
2. Put a TLS-terminating reverse proxy in front of
   `http://127.0.0.1:8088`.
3. Set `ALIASDECK_PUBLIC_URL` to the exact browser-facing HTTPS origin and
   recreate the service:

```bash
ALIASDECK_PUBLIC_URL=https://aliases.example.com \
  docker compose -f compose.prod.yaml up -d --wait
```

The value must be an origin only: no credentials, path, query, or fragment.
AliasDeck deliberately does not trust `Forwarded`, `X-Forwarded-*`, or similar
headers for locality, setup authorization, cookie security, or enrollment
URLs. `ALIASDECK_PUBLIC_URL` is the explicit source for secure cookies and the
URL embedded in enrollment commands. Never publish the Compose port on
`0.0.0.0` as a shortcut for remote access.

## Other client installation paths

The install script supports macOS and Linux on amd64 and arm64, resolves the
latest published release, and verifies its checksum before installation:

```bash
curl -sSL https://raw.githubusercontent.com/angeltonio/aliasdeck/main/scripts/install.sh | sh
```

Windows packages are available through Scoop:

```powershell
scoop bucket add angeltonio https://github.com/angeltonio/scoop-bucket
scoop install aliasdeck
```

Published release archives contain client and server binaries for macOS,
Linux, and Windows on amd64 and arm64. The client renders zsh, bash, and
PowerShell aliases. See the
[`v0.6.1` release](https://github.com/angeltonio/aliasdeck/releases/tag/v0.6.1)
for the currently pinned deployment artifacts and checksums.

## Use the client without a server

Standalone mode needs no account, database, or network service:

```bash
aliasdeck init      # create local configuration and configure shell integration
aliasdeck edit      # edit aliases.yaml in $EDITOR
aliasdeck sync      # render the aliases file for the active shell
aliasdeck status    # show source, device identity, and sync status
aliasdeck list      # show which aliases apply to this device
```

`aliasdeck uninstall` removes the managed integration and restores the shell
configuration it changed.

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

That is the core difference from a dotfile manager: AliasDeck knows that
`docker ps` is a command rather than an arbitrary line of text.

### Where the output goes

AliasDeck owns one generated file instead of rewriting your shell config:

```text
~/.config/aliasdeck/aliases.zsh
```

The shell configuration contains one bootstrap line, added once by
`aliasdeck init` and removed by `aliasdeck uninstall`.

## How is this different from Chezmoi?

Chezmoi is excellent and AliasDeck does not replace it. They solve different
problems.

| | Chezmoi and similar tools | AliasDeck |
| --- | --- | --- |
| Unit of work | Files | Commands |
| Cross-shell support | Template conditionals | Built-in shell renderers |
| Escaping | Maintained by the user | Handled and tested by AliasDeck |
| Targeting | Hostname or template conditions | Profiles such as Development, Homelab, and Work |
| Scope | All dotfiles | Aliases and reusable commands |

They can be used together: an `aliases.yaml` file can live in an existing
dotfiles repository while AliasDeck owns rendering and synchronization.

## Design decisions worth knowing

The detailed reasoning lives in [`docs/PROJECT.md`](docs/PROJECT.md).

| Decision | Why |
| --- | --- |
| **The client renders, never the server** | Escaping stays next to the shell that needs it, and clients can add shell support independently. |
| **The server transmits data, never shell code** | The control plane cannot directly write pre-rendered commands into clients. |
| **Strict alias names, not escaped ones** | Names outside quoted command regions are rejected when unsafe. |
| **Output has no timestamp** | Generated files stay byte-deterministic, so sync can compare hashes. |
| **One config source per device** | Local and remote sources never merge silently. |
| **Go everywhere** | The same domain and validation rules support every client platform. |

## Current capabilities

| Component | State |
| --- | --- |
| Domain model, targeting, and validation | ✅ |
| zsh, bash, and PowerShell renderers | ✅ |
| Standalone and Git-hosted client configuration | ✅ |
| Self-hosted server, REST API, SQLite, and device enrollment | ✅ |
| English and Spanish web UI | ✅ |
| Alias, group, and device management from the browser, including targeting | ✅ |
| Per-device preview of which aliases arrive, and why the others do not | ✅ |
| Device access revocation from the browser | ✅ |
| Credential rotation without re-enrolling a device | ✅ |
| Every operator action available in the browser, not only through the REST API | ✅ |
| Audit record of every operator action, from either surface | ✅ |
| Operator password recovery without data loss | ✅ |
| Configurable automatic alias synchronization on macOS | ✅ |
| Homebrew cask, Scoop manifest, release archives, and checksum-verifying install script | ✅ |

## Development

For the isolated hot-reload server, checkout-built client, test workflow, and
safe development reset, read [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md).

## Contributing

Contributions are welcome, and [`CONTRIBUTING.md`](CONTRIBUTING.md) is worth
reading first: AliasDeck enforces several rules through tests rather than
review — the client binary cannot import server packages, the server cannot
emit shell syntax, both translation catalogues must stay in step — and a pull
request bounced for a rule you had no way of knowing is nobody's idea of a
good time.

Issues labelled `good first issue` are scoped and have context. The
architecture is still evolving, so disagreement is useful too: open an issue
if you think something in [`docs/PROJECT.md`](docs/PROJECT.md) is wrong. A
wrong decision is cheapest to fix before users depend on it.

## License

[MIT](LICENSE).
