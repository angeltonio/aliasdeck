# Local development

Start the hot-reloading server:

```sh
make dev-up
```

It listens only on `http://127.0.0.1:8088`, stores SQLite data in the
dedicated `aliasdeck-dev_server-data` volume, and rebuilds after Go or embedded
web asset changes. Open `/setup` after the first start.

In a second zsh, activate the checkout-built client:

```zsh
source scripts/dev.zsh
aliasdeck init --shell zsh --no-bootstrap --skip-initial-sync
```

The `aliasdeck` function rebuilds the client from the current checkout before
every command and forces all client state into ignored
`build/aliasdeck-dev/client`; it never uses `~/.config/aliasdeck`. Create a
device token under **Devices → Add device**, then register it:

```zsh
aliasdeck register --url http://127.0.0.1:8088 --token '<one-time-token>'
```

## Exercising the command the web UI hands out

`scripts/dev.zsh` above keeps client state out of `~/.config/aliasdeck`, and
`init --no-bootstrap` keeps the shell block out of `~/.zshrc`. Neither applies
to the command **Devices → Add device** actually produces, which is what a
real user pastes:

```
aliasdeck init --yes --skip-initial-sync && aliasdeck register --url ... && aliasdeck sync
```

That has no `--no-bootstrap`, by design — a real machine wants the bootstrap
line. Pasting it on your own machine rewrites your real `~/.zshrc` to point at
whatever `ALIASDECK_HOME` was set to, and `init` does **not** rewrite an
existing block back, so the damage outlives the session. It also leaves a
credential for one server in place, which is enough to make a registration
against another one refuse.

For that, use a throwaway machine:

```sh
make dev-client
```

An Alpine container with zsh, the client built from the mounted checkout, and
its own `/root` — so `init`, `register`, `agent install` and the shell block
all land inside it. Reach the dev server at `http://host.docker.internal:8088`
(`127.0.0.1` inside a container is the container), which needs
`--allow-insecure` since it is neither https nor loopback from in there:

```sh
aliasdeck register --url http://host.docker.internal:8088 --token '<token>' --allow-insecure
```

Its home persists between sessions so a revoke-then-re-enrol test can span
two of them. `make dev-client-reset` forgets it.

The development server explicitly generates automatic-enrollment commands
with a `5s` watcher interval for a fast feedback loop. Normal server and CLI
defaults remain `30s`; `aliasdeck agent install --interval <duration>` records
an explicit validated override in the platform background agent.

The fast loop is: create or delete an alias in `/aliases`, then run:

```zsh
aliasdeck sync --shell zsh
```

After a successful sync the activation function sources `aliases.zsh` into
the current shell. Generated POSIX files remove only aliases managed by their
previous revision before defining the new set, so delete-and-sync preserves
unrelated user aliases. Generated zsh files also reload at prompt boundaries,
which makes changes written by the background watcher visible without opening
a new terminal.

State is persistent by default:

```sh
make dev-down  # stop containers; keep server and client state
make dev-up    # resume the same state
```

Reset only the dedicated development environment:

```zsh
aliasdeck_dev_deactivate  # remove aliases loaded into this zsh
make dev-reset            # remove dev containers, volume, client state and dev binary
```

`dev-reset` first removes a macOS watcher only when its plist exactly matches
the dev executable and dev `ALIASDECK_HOME`. When the isolated client has
recorded an exact shell bootstrap, reset then runs that dev client's scoped
uninstall before deleting its state, restoring the affected shell rc file.
Finally it removes Compose project `aliasdeck-dev` and
`build/aliasdeck-dev`. It does not touch another AliasDeck installation,
container, volume, global binary, or unrelated shell content.
