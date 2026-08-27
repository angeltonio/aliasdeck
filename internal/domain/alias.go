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

// TargetingMiss names the first reason an alias does not apply to a device,
// or MissNone when it does.
//
// It is a value rather than a sentence so every caller can phrase it for its
// own audience: the CLI prints English next to `list`, and the web UI has to
// say the same thing in two languages. What must not differ between them is
// which dimension actually failed.
type TargetingMiss string

const (
	MissNone     TargetingMiss = ""
	MissDisabled TargetingMiss = "disabled"
	MissPlatform TargetingMiss = "platform"
	MissShell    TargetingMiss = "shell"
	MissProfile  TargetingMiss = "profile"
	MissDevice   TargetingMiss = "device"
)

// Miss reports why a does not apply to dev, in the order the dimensions are
// evaluated, or MissNone when every dimension agrees.
//
// The order is the answer to "why is this alias not arriving?", so it is
// deliberate rather than incidental: a disabled alias is reported as disabled
// even when its targeting would also have excluded the device, because
// re-enabling it is the first thing an operator would try.
func (a Alias) Miss(dev Device) TargetingMiss {
	switch {
	case !a.Enabled:
		return MissDisabled
	case !a.TargetsPlatform(dev.Platform):
		return MissPlatform
	case !a.TargetsShell(dev.Shell):
		return MissShell
	case !a.TargetsProfiles(dev.ProfileIDs):
		return MissProfile
	case !a.TargetsDevice(dev.ID):
		return MissDevice
	default:
		return MissNone
	}
}

// AppliesTo reports whether a should be rendered for dev.
//
// Every targeting dimension must agree. This is the single place resolution
// semantics live, so that a local FileSource and the server reach identical
// conclusions from identical input — which is also why it is expressed
// through Miss rather than repeating the same five checks: two spellings of
// one predicate are two chances for them to disagree.
func (a Alias) AppliesTo(dev Device) bool {
	return a.Miss(dev) == MissNone
}
