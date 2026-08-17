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

The development server explicitly generates automatic-enrollment commands
with a `5s` watcher interval for a fast feedback loop. Normal server and CLI
defaults remain `30s`; `aliasdeck agent install --interval <duration>` records
an explicit validated override in the macOS LaunchAgent.

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
