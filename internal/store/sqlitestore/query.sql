-- Queries for internal/store/sqlitestore, generated into this package by
-- sqlc (design decision 6). Every statement is parameterized; sqlc never
-- concatenates values into SQL text (threat matrix: SQL construction).

-- name: CreateAlias :one
INSERT INTO aliases (id, name, command, description, enabled, platforms, shells, tags, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id, name, command, description, enabled, platforms, shells, tags, created_at, updated_at;

-- name: GetAlias :one
SELECT id, name, command, description, enabled, platforms, shells, tags, created_at, updated_at
FROM aliases WHERE id = ?;

-- name: ListAliases :many
SELECT id, name, command, description, enabled, platforms, shells, tags, created_at, updated_at
FROM aliases ORDER BY name;

-- name: UpdateAlias :one
UPDATE aliases
SET name = ?, command = ?, description = ?, enabled = ?, platforms = ?, shells = ?, tags = ?, updated_at = ?
WHERE id = ?
RETURNING id, name, command, description, enabled, platforms, shells, tags, created_at, updated_at;

-- name: DeleteAlias :execrows
DELETE FROM aliases WHERE id = ?;

-- name: ClearAliasProfiles :exec
DELETE FROM alias_profiles WHERE alias_id = ?;

-- name: InsertAliasProfile :exec
INSERT INTO alias_profiles (alias_id, profile_id) VALUES (?, ?);

-- name: ListAliasProfileIDs :many
SELECT profile_id FROM alias_profiles WHERE alias_id = ? ORDER BY profile_id;

-- name: ClearAliasDevices :exec
DELETE FROM alias_devices WHERE alias_id = ?;

-- name: InsertAliasDevice :exec
INSERT INTO alias_devices (alias_id, device_id) VALUES (?, ?);

-- name: ListAliasDeviceIDs :many
SELECT device_id FROM alias_devices WHERE alias_id = ? ORDER BY device_id;

-- name: CreateProfile :one
INSERT INTO profiles (id, name, description, created_at, updated_at)
VALUES (?, ?, ?, ?, ?)
RETURNING id, name, description, created_at, updated_at;

-- name: GetProfile :one
SELECT id, name, description, created_at, updated_at FROM profiles WHERE id = ?;

-- name: ListProfiles :many
SELECT id, name, description, created_at, updated_at FROM profiles ORDER BY name;

-- name: UpdateProfile :one
UPDATE profiles SET name = ?, description = ?, updated_at = ?
WHERE id = ?
RETURNING id, name, description, created_at, updated_at;

-- name: DeleteProfile :execrows
DELETE FROM profiles WHERE id = ?;

-- name: InsertDevice :exec
INSERT INTO devices (id, name, platform, shell, client_version, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: GetDevice :one
SELECT id, name, platform, shell, client_version, created_at, updated_at, last_seen_at, last_sync_at, revoked_at
FROM devices WHERE id = ?;

-- name: ListDevices :many
SELECT id, name, platform, shell, client_version, created_at, updated_at, last_seen_at, last_sync_at, revoked_at
FROM devices ORDER BY name;

-- name: UpdateDevice :execrows
UPDATE devices SET name = ?, updated_at = ? WHERE id = ?;

-- name: TouchDevice :execrows
UPDATE devices SET platform = ?, shell = ?, last_seen_at = ?, last_sync_at = ?, updated_at = ? WHERE id = ?;

-- name: HeartbeatDevice :execrows
UPDATE devices SET last_seen_at = ?, updated_at = ? WHERE id = ?;

-- name: RevokeDevice :execrows
UPDATE devices SET revoked_at = ?, updated_at = ? WHERE id = ?;

-- name: DeleteDevice :execrows
DELETE FROM devices WHERE id = ?;

-- name: ClearDeviceProfiles :exec
DELETE FROM device_profiles WHERE device_id = ?;

-- name: InsertDeviceProfile :exec
INSERT INTO device_profiles (device_id, profile_id) VALUES (?, ?);

-- name: ListDeviceProfileIDs :many
SELECT profile_id FROM device_profiles WHERE device_id = ? ORDER BY profile_id;

-- name: CreateOperator :one
INSERT INTO operators (id, username, password_hash, created_at, updated_at)
VALUES (?, ?, ?, ?, ?)
RETURNING id, username, password_hash, created_at, updated_at;

-- name: GetOperator :one
SELECT id, username, password_hash, created_at, updated_at FROM operators WHERE id = ?;

-- name: GetOperatorByUsername :one
SELECT id, username, password_hash, created_at, updated_at FROM operators WHERE username = ?;

-- name: CountOperators :one
SELECT COUNT(*) FROM operators;

-- name: UpdateOperatorPassword :one
UPDATE operators SET password_hash = ?, updated_at = ? WHERE username = ?
RETURNING id, username, password_hash, created_at, updated_at;

-- name: CreateToken :exec
INSERT INTO tokens (id, kind, subject_id, lookup, secret_hash, profile_ids, created_at, expires_at, used_at, revoked_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetTokenByLookup :one
SELECT id, kind, subject_id, lookup, secret_hash, profile_ids, created_at, expires_at, used_at, revoked_at
FROM tokens WHERE lookup = ?;

-- name: ConsumeEnrollmentToken :execrows
UPDATE tokens
SET used_at = ?, subject_id = ?
WHERE lookup = ? AND kind = 'enrollment' AND used_at IS NULL AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > ?);

-- name: RevokeToken :execrows
UPDATE tokens SET revoked_at = ? WHERE id = ?;

-- name: RevokeTokensBySubject :exec
UPDATE tokens SET revoked_at = ? WHERE kind = ? AND subject_id = ? AND revoked_at IS NULL;
