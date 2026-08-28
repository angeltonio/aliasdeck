-- +goose Up
-- audit_events records what an operator did, so "who created this alias?" and
-- "when was that device revoked?" have an answer. AliasDeck distributes
-- definitions that get executed in people's shells; when something unexpected
-- shows up on a machine, this is the place to look.
--
-- Deliberately not a foreign key to operators. An audit record has to outlive
-- its subject and its actor — a row that cascades away when an operator or a
-- device is deleted is exactly the row an investigation needs, and the one
-- most likely to be gone. actor_id and subject_id are therefore plain text
-- identifiers, kept for correlation rather than for referential integrity.
--
-- actor_name and subject_label are denormalized on purpose, for the same
-- reason: they record what the thing was called at the time. Resolving the
-- name at read time would show today's name for yesterday's action, or
-- nothing at all once the record is gone.
--
-- No detail/payload column. The fields below are the ones that answer the
-- questions this table exists for, and an open-ended blob is where token
-- material eventually leaks in — see design decision 8 on what must never be
-- stored beside a hash.
CREATE TABLE audit_events (
    id            TEXT PRIMARY KEY,
    at            TEXT NOT NULL,
    actor_id      TEXT NOT NULL DEFAULT '',
    actor_name    TEXT NOT NULL DEFAULT '',
    action        TEXT NOT NULL,
    subject_kind  TEXT NOT NULL,
    subject_id    TEXT NOT NULL DEFAULT '',
    subject_label TEXT NOT NULL DEFAULT ''
);

-- Reads are "the most recent N", always. at is RFC 3339 text, which sorts
-- correctly as text, so this index serves the only query shape there is.
CREATE INDEX audit_events_at_idx ON audit_events (at DESC);

-- +goose Down
DROP TABLE audit_events;
