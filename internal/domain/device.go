package domain

import "time"

// Device is a machine AliasDeck renders configuration for.
//
// In standalone mode the device is described entirely by the local
// config.yaml and is never registered anywhere. In control-plane mode the
// server owns the record and the CLI receives it back on sync. The type is the
// same either way so that resolution behaves identically in both modes.
type Device struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Hostname      string     `json:"hostname,omitempty"`
	Platform      Platform   `json:"platform"`
	Shell         Shell      `json:"shell"`
	Architecture  string     `json:"architecture,omitempty"`
	ProfileIDs    []string   `json:"profileIds,omitempty"`
	LastSeenAt    *time.Time `json:"lastSeenAt,omitempty"`
	LastSyncAt    *time.Time `json:"lastSyncAt,omitempty"`
	ClientVersion string     `json:"clientVersion,omitempty"`

	// RevokedAt is when an operator cut this device's access, or nil while it
	// is still trusted. It is carried here because revocation that cannot be
	// seen is revocation an operator cannot confirm: the row is otherwise
	// indistinguishable from a live device, so a second revoke looks like the
	// first one silently failed.
	//
	// It is a record, not the enforcement. What actually stops a revoked
	// device is that its device-kind tokens are revoked in the same
	// operation; this field is what lets a reader know that happened.
	RevokedAt *time.Time `json:"revokedAt,omitempty"`
}

// Profile groups aliases by purpose rather than by machine.
//
// Profiles are the targeting primitive: a device subscribes to "Development"
// and "Homelab", not to a list of hostnames.
type Profile struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"createdAt,omitzero"`
	UpdatedAt   time.Time `json:"updatedAt,omitzero"`
}
