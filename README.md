# AliasDeck

> Your commands. Every machine.

AliasDeck is a self-hosted alias and command manager designed to centrally manage, organize and synchronize shell aliases, functions and reusable commands across macOS, Linux and Windows.

The goal is to avoid manually editing `.zshrc`, `.bashrc`, PowerShell profiles or other shell configuration files on every machine.

AliasDeck provides a web control plane, API and CLI agent. A native backend should work without external dependencies, while integrations such as Chezmoi can be added as optional adapters.

## Status

Early design / MVP planning.

See [`docs/PROJECT.md`](docs/PROJECT.md) for the full product and architecture specification.

## Initial scope

- Self-hosted Web UI
- API
- CLI agent
- macOS, Linux and Windows
- zsh, bash and PowerShell initially
- Aliases and commands
- Device registration
- Profiles / groups
- Per-device and per-platform targeting
- Manual sync first, automatic sync later
- Optional Chezmoi backend
- Future MCP integration

## Example

```bash
aliasdeck login https://aliases.example.com
aliasdeck sync
aliasdeck status
aliasdeck list
```

## License

TBD.
