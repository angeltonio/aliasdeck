# What AliasDeck Is

One sentence: **AliasDeck keeps your shell aliases the same on every machine you use.**

You write an alias once. Every machine you own gets it, in the syntax its own
shell speaks — `alias` on zsh and bash, a function on PowerShell — without you
maintaining three copies of the same idea.

## Two programs, two audiences

This is the decision that shapes everything else, and it reverses an earlier
one. AliasDeck ships **two** artifacts, not one.

### `aliasdeck` — the client

Installed on **every machine you use**. This is what almost everybody
downloads, and for many people it is the only part they ever run.

It reads aliases from a source, works out which ones apply to *this* machine,
renders them in *this* shell's syntax, and writes one file your shell reads at
startup.

```
brew install angeltonio/tap/aliasdeck
```

**It never serves a web page. It never listens on a port. It is a client.**

That is a hard rule, not a default. `aliasdeck` has no `serve` command,
because a program you install on your laptop to keep aliases in sync has no
business being a server, and offering the option only invites the question.

Its sources are:

| Source | For |
|---|---|
| A local `aliases.yaml` | One machine, or a file you sync yourself |
| A Git repository | Several machines, versioned in a repo you already have |
| An AliasDeck server | Several machines, managed from one place, with a UI |

The first two need no server at all. **Standalone is a first-class mode, not a
degraded one.**

### `aliasdeck-server` — the control plane

Deployed **once**, somewhere that stays on: a VPS, a homelab box, a Raspberry
Pi. Most people will never run it.

It holds the aliases, serves the web UI where you manage them, and answers the
clients that sync from it. It carries an embedded SQLite database, so there is
no separate database to run.

```
docker run -v aliasdeck:/data ghcr.io/angeltonio/aliasdeck-server
```

Docker is the primary way to deploy it, because deploying a service is what
Docker is for. A plain binary is published too, for people who prefer systemd.

## Why they are separate

They were one binary until this change, and the split is worth explaining
because the earlier reasoning was not wrong — it was answering a different
question.

**Because the mental model was not landing.** The single binary meant
`aliasdeck serve` existed on every laptop, and explaining which half was which
took repeated attempts even with the person who designed it. A model that
needs explaining to its own author will not survive contact with someone
arriving from a README.

**Because most users are carrying a server they will never run.** Measured on
darwin/arm64, stripped, the same flags the release uses:

| | |
|---|---|
| Combined binary, before the split | 11.7 MB |
| `aliasdeck` client, after | **6.6 MB** |
| `aliasdeck-server`, after | 10.6 MB |

A client user stops carrying **5.1 MB — 43%** of embedded SQLite, migration
runner and HTTP server they will never run.

An earlier draft of this section claimed 3.3 MB against 14.0 MB, a fourfold
difference. Both figures were wrong and the error is worth recording, because
it is the same shape this project keeps finding in its own code. The 3.3 MB
came from a probe that imported three packages and neither cobra nor a single
command — its incompleteness was noticed out loud and then the number was used
anyway. The 14.0 MB was the binary *with the web UI* from another branch,
compared against a client without it.

43% is a real saving and a weaker argument than fourfold. It is not the reason
for the split, and was never the main one — the reason is the paragraph above
this one.

**Because the two are deployed by different people, in different ways, on
different schedules.** You install a client. You deploy a server. Pretending
they are the same artifact does not make that true.

What the split costs: twelve release artifacts instead of six, and two things
to version. What it does **not** cost is the promise that actually mattered —
a single file with no runtime dependency. Neither program needs Node, Python,
or a database installed alongside it. That promise was never about the number
of binaries.

## The rule that does not move

**The server transmits data. The client produces shell syntax.**

The server never sends shell code to a machine. It sends aliases as data —
a name, a command, a description — and the client renders them. This is
enforced by a test that fails the build if any server package imports the
renderer, and proven by a test that the same aliases produce byte-identical
output whether they came from a local file or crossed a database, JSON, HTTP
and a second resolve.

It matters because a shell alias is a command your shell will execute. If a
server could send rendered shell syntax, then a compromised server — or a
server someone else runs — could send anything. Instead, every source is
treated as hostile input and passes the same validation, and the machine that
runs the code is the machine that wrote it.

## What each program will and will not do

| | `aliasdeck` | `aliasdeck-server` |
|---|---|---|
| Sync aliases to a machine | ✅ | — |
| Render shell syntax | ✅ | **never** |
| Work with no server at all | ✅ | — |
| Serve a web UI | **never** | ✅ |
| Listen on a port | **never** | ✅ |
| Store aliases for many machines | — | ✅ |
| Installed via brew / scoop | ✅ | also available |
| Deployed via Docker | — | ✅ primary |

## Where this is going

Shipping now: the client, and the server with its bilingual web UI and API.
Operator onboarding, device enrollment, and configurable automatic
synchronization on macOS are part of the current product.

Later, in rough order: teams, which is why alias ownership is being recorded
in the database before anyone can use it; and history, so a change can be seen
and undone.

Not planned: a hosted service. AliasDeck is something you run.
