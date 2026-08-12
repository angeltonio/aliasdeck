package domain

import "time"

// Alias is a command definition in AliasDeck's neutral representation.
//
// It is deliberately shell-agnostic: it says *what* the command is and *where*
// it applies, never how it should be written. Turning it into shell syntax is
// the renderers' job.
//
// Empty targeting slices mean "everywhere". This matters: it is the difference
// between an alias nobody gets and an alias everybody gets, and the zero value
// must be the useful one so that a minimal aliases.yaml entry just works.
type Alias struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Command     string     `json:"command"`
	Description string     `json:"description,omitempty"`
	Enabled     bool       `json:"enabled"`
	Tags        []string   `json:"tags,omitempty"`
	Platforms   []Platform `json:"platforms,omitempty"`
	Shells      []Shell    `json:"shells,omitempty"`
	ProfileIDs  []string   `json:"profileIds,omitempty"`
	DeviceIDs   []string   `json:"deviceIds,omitempty"`
	CreatedAt   time.Time  `json:"createdAt,omitzero"`
	UpdatedAt   time.Time  `json:"updatedAt,omitzero"`
}

// TargetsPlatform reports whether a applies to p.
// An empty Platforms list means every platform.
func (a Alias) TargetsPlatform(p Platform) bool {
	if len(a.Platforms) == 0 {
		return true
	}
	for _, candidate := range a.Platforms {
		if candidate == p {
			return true
		}
	}
	return false
}

// TargetsShell reports whether a applies to sh.
// An empty Shells list means every shell.
func (a Alias) TargetsShell(sh Shell) bool {
	if len(a.Shells) == 0 {
		return true
	}
	for _, candidate := range a.Shells {
		if candidate == sh {
			return true
		}
	}
	return false
}

// TargetsProfiles reports whether a applies to a device subscribed to the given
// profiles. An empty ProfileIDs list means the alias is unconditional.
func (a Alias) TargetsProfiles(deviceProfiles []string) bool {
	if len(a.ProfileIDs) == 0 {
		return true
	}
	for _, want := range a.ProfileIDs {
		for _, has := range deviceProfiles {
			if want == has {
				return true
			}
		}
	}
	return false
}

// TargetsDevice reports whether a applies to the device with the given id.
// An empty DeviceIDs list means the alias is not pinned to specific devices.
func (a Alias) TargetsDevice(deviceID string) bool {
	if len(a.DeviceIDs) == 0 {
		return true
	}
	for _, candidate := range a.DeviceIDs {
		if candidate == deviceID {
			return true
		}
	}
	return false
}

// AppliesTo reports whether a should be rendered for dev.
//
// Every targeting dimension must agree. This is the single place resolution
// semantics live, so that a local FileSource and the server reach identical
// conclusions from identical input.
func (a Alias) AppliesTo(dev Device) bool {
	return a.Enabled &&
		a.TargetsPlatform(dev.Platform) &&
		a.TargetsShell(dev.Shell) &&
		a.TargetsProfiles(dev.ProfileIDs) &&
		a.TargetsDevice(dev.ID)
}
