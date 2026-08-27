package domain

import "testing"

func macDevice() Device {
	return Device{ID: "device-1", Platform: PlatformMacOS, Shell: ShellZsh, ProfileIDs: []string{"laptops"}}
}

func TestMissNamesTheFirstFailingDimension(t *testing.T) {
	dev := macDevice()

	for _, tc := range []struct {
		name  string
		alias Alias
		want  TargetingMiss
	}{
		{"applies", Alias{Enabled: true}, MissNone},
		{"disabled", Alias{}, MissDisabled},
		{"wrong platform", Alias{Enabled: true, Platforms: []Platform{PlatformLinux}}, MissPlatform},
		{"wrong shell", Alias{Enabled: true, Shells: []Shell{ShellBash}}, MissShell},
		{"no shared profile", Alias{Enabled: true, ProfileIDs: []string{"servers"}}, MissProfile},
		{"pinned elsewhere", Alias{Enabled: true, DeviceIDs: []string{"device-2"}}, MissDevice},
		{"targeted at this device", Alias{Enabled: true, DeviceIDs: []string{"device-1"}}, MissNone},
		// Profile targeting is an OR: sharing any one listed profile is
		// enough, so this applies even though "servers" does not match.
		{"shares one of several profiles", Alias{Enabled: true, ProfileIDs: []string{"servers", "laptops"}}, MissNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.alias.Miss(dev); got != tc.want {
				t.Fatalf("Miss() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestMissReportsDisabledAheadOfTargeting pins the ordering choice. A
// disabled alias whose targeting would also exclude the device must report
// disabled, because enabling it is the first thing an operator would try.
func TestMissReportsDisabledAheadOfTargeting(t *testing.T) {
	a := Alias{Platforms: []Platform{PlatformWindows}, Shells: []Shell{ShellBash}}
	if got := a.Miss(macDevice()); got != MissDisabled {
		t.Fatalf("Miss() = %q, want %q", got, MissDisabled)
	}
}

// TestAppliesToAgreesWithMiss is the guard that keeps the two from drifting:
// AppliesTo is defined through Miss, and this fails the moment either grows
// a condition the other does not have.
func TestAppliesToAgreesWithMiss(t *testing.T) {
	dev := macDevice()

	aliases := []Alias{
		{Enabled: true},
		{},
		{Enabled: true, Platforms: []Platform{PlatformMacOS}},
		{Enabled: true, Platforms: []Platform{PlatformLinux}},
		{Enabled: true, Shells: []Shell{ShellZsh}},
		{Enabled: true, Shells: []Shell{ShellPowerShell}},
		{Enabled: true, ProfileIDs: []string{"laptops"}},
		{Enabled: true, ProfileIDs: []string{"servers"}},
		{Enabled: true, DeviceIDs: []string{"device-1"}},
		{Enabled: true, DeviceIDs: []string{"device-9"}},
		{Enabled: true, Platforms: []Platform{PlatformMacOS}, Shells: []Shell{ShellZsh}, ProfileIDs: []string{"laptops"}, DeviceIDs: []string{"device-1"}},
	}
	for i, a := range aliases {
		applies, miss := a.AppliesTo(dev), a.Miss(dev)
		if applies != (miss == MissNone) {
			t.Fatalf("alias %d: AppliesTo() = %v but Miss() = %q", i, applies, miss)
		}
	}
}
