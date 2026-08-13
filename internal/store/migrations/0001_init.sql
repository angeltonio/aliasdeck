-- +goose Up
-- Storage Schema (design.md): operators, profiles, aliases and devices are
-- the control plane's subjects; alias_profiles/alias_devices/device_profiles
-- are join tables because targeting membership is the actual thing being
-- modeled (design decision 4 keeps platforms/shells/tags as JSON text
-- instead, since domain.Resolve is the only reader and normalizing them
-- into tables would buy query shapes that decision forbids).
--
-- Every timestamp is stored as RFC 3339 text; sqlite has no native
-- timestamp type, and text sorts and compares correctly for that format.

CREATE TABLE operators (
    id            TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE TABLE profiles (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE TABLE aliases (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    command     TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    enabled     INTEGER NOT NULL DEFAULT 1,
    platforms   TEXT NOT NULL DEFAULT '[]',
    shells      TEXT NOT NULL DEFAULT '[]',
    tags        TEXT NOT NULL DEFAULT '[]',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE TABLE devices (
    id             TEXT PRIMARY KEY,
    name           TEXT NOT NULL,
    platform       TEXT NOT NULL DEFAULT '',
    shell          TEXT NOT NULL DEFAULT '',
    client_version TEXT NOT NULL DEFAULT '',
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    last_seen_at   TEXT,
    last_sync_at   TEXT,
    revoked_at     TEXT
);

-- Join tables: ON DELETE CASCADE relies on the sqlitestore connection
-- running with `foreign_keys=on` (design decision 7); without that pragma
-- sqlite accepts but ignores the clause.
CREATE TABLE alias_profiles (
    alias_id   TEXT NOT NULL REFERENCES aliases(id) ON DELETE CASCADE,
    profile_id TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    PRIMARY KEY (alias_id, profile_id)
);

CREATE TABLE alias_devices (
    alias_id  TEXT NOT NULL REFERENCES aliases(id) ON DELETE CASCADE,
    device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    PRIMARY KEY (alias_id, device_id)
);

CREATE TABLE device_profiles (
    device_id  TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    profile_id TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    PRIMARY KEY (device_id, profile_id)
);

-- kind is one of "session", "enrollment", "device" (store.TokenKind).
-- lookup is indexed uniquely so authentication is always one row lookup,
-- never a scan (design decision 8).
CREATE TABLE tokens (
    id          TEXT PRIMARY KEY,
    kind        TEXT NOT NULL,
    subject_id  TEXT NOT NULL DEFAULT '',
    lookup      TEXT NOT NULL,
    secret_hash BLOB NOT NULL,
    profile_ids TEXT NOT NULL DEFAULT '[]',
    created_at  TEXT NOT NULL,
    expires_at  TEXT,
    used_at     TEXT,
    revoked_at  TEXT
);

CREATE UNIQUE INDEX tokens_lookup_idx ON tokens (lookup);

-- +goose Down
DROP TABLE tokens;
DROP TABLE device_profiles;
DROP TABLE alias_devices;
DROP TABLE alias_profiles;
DROP TABLE devices;
DROP TABLE aliases;
DROP TABLE profiles;
DROP TABLE operators;
