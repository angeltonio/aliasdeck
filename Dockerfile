# AliasDeck server — a container around the aliasdeck-server binary the
# release ships (docs/WHAT-WE-ARE-BUILDING.md: two binaries, two audiences —
# this image runs the control plane, never the client). Nothing here changes
# how the program works; it only chooses the two settings a container makes
# different.
#
# Two of this project's deliberate decisions need a container-specific answer,
# and both are chosen below rather than left to surprise an operator.

FROM golang:1.25-alpine AS build
WORKDIR /src

# Dependencies first, so a source-only change does not re-download them.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 is what makes the result a static binary with no libc to link
# against, which is the whole reason the runtime stage below can be distroless.
# It is also what modernc.org/sqlite buys us: a pure-Go SQLite with no C
# toolchain in the image.
#
# cmd/aliasdeck-server, not cmd/aliasdeck: the client never serves anything
# (docs/WHAT-WE-ARE-BUILDING.md) and has no business in a container image at
# all — this is the control-plane binary, the one artifact this image exists
# to run.
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/aliasdeck-server ./cmd/aliasdeck-server

# An empty /data owned by distroless's nonroot uid, carried into the runtime
# stage below. Docker seeds a fresh named volume from whatever the image has
# at that path, ownership included — so creating it here is what makes the
# volume writable by a non-root process.
#
# Measured, by not doing it first: the container restart-looped on
# "opening database file /data/aliasdeck.db: permission denied", because a
# volume mounted over a path the image never created lands owned by root.
RUN mkdir -p /data && chown -R 65532:65532 /data

# distroless rather than scratch: it carries a nonroot user and CA
# certificates. The server makes no outbound TLS calls today, but running as
# root in a container for no reason is a habit worth not forming.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/aliasdeck-server /usr/local/bin/aliasdeck-server
COPY --from=build --chown=65532:65532 /data /data

# The database lives on a volume. Without one, every container restart starts
# from an empty schema and mints a new operator — which looks like data loss
# and is really just an ephemeral filesystem.
VOLUME ["/data"]
EXPOSE 8080

USER nonroot

# --addr 0.0.0.0:8080 overrides the binary's own default of 127.0.0.1:8080.
#
# That default exists because binding every interface on a host puts a control
# plane on the LAN behind one password (design decision 21). A container is the
# case where that reasoning inverts: 0.0.0.0 here means every interface *of the
# container's network namespace*, which nothing outside reaches until you
# publish a port. Leaving the loopback default would make the container
# unreachable through -p and look broken.
#
# What that does NOT do is make exposure safe. `-p 0.0.0.0:8080:8080` still
# puts it on your LAN over plaintext HTTP. Publish to 127.0.0.1, or put TLS in
# front.
#
# aliasdeck-server has no `serve` subcommand — the binary itself is the
# command (docs/WHAT-WE-ARE-BUILDING.md), so the flags below go straight on
# the entrypoint instead of behind a verb.
ENTRYPOINT ["/usr/local/bin/aliasdeck-server"]
CMD ["--addr", "0.0.0.0:8080", "--db", "/data/server.db"]
