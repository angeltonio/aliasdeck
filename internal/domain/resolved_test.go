package domain

import "testing"

func TestAliasTargetingDefaultsToEverywhere(t *testing.T) {
	dev := Device{
		ID:         "dev-1",
		Platform:   PlatformLinux,
		Shell:      ShellBash,
		ProfileIDs: []string{"work"},
	}

	bare := Alias{Name: "x", Command: "echo x", Enabled: true}

	if !bare.AppliesTo(dev) {
		t.Fatal("an alias with no targeting must apply everywhere; " +
			"the zero value has to be the useful one")
	}
}

func TestAliasTargeting(t *testing.T) {
	dev := Device{
		ID:         "dev-1",
		Platform:   PlatformMacOS,
		Shell:      ShellZsh,
		ProfileIDs: []string{"development", "homelab"},
	}

	tests := []struct {
		name  string
		alias Alias
		want  bool
	}{
		{
			name:  "matching platform",
			alias: Alias{Enabled: true, Platforms: []Platform{PlatformMacOS}},
			want:  true,
		},
		{
			name:  "other platform",
			alias: Alias{Enabled: true, Platforms: []Platform{PlatformWindows}},
			want:  false,
		},
		{
			name:  "matching shell",
			alias: Alias{Enabled: true, Shells: []Shell{ShellZsh, ShellBash}},
			want:  true,
		},
		{
			name:  "other shell",
			alias: Alias{Enabled: true, Shells: []Shell{ShellPowerShell}},
			want:  false,
		},
		{
			name:  "one matching profile is enough",
			alias: Alias{Enabled: true, ProfileIDs: []string{"work", "homelab"}},
			want:  true,
		},
		{
			name:  "no matching profile",
			alias: Alias{Enabled: true, ProfileIDs: []string{"work"}},
			want:  false,
		},
		{
			name:  "pinned to this device",
			alias: Alias{Enabled: true, DeviceIDs: []string{"dev-1"}},
			want:  true,
		},
		{
			name:  "pinned to another device",
			alias: Alias{Enabled: true, DeviceIDs: []string{"dev-2"}},
			want:  false,
		},
		{
			name:  "disabled overrides every match",
			alias: Alias{Enabled: false, Platforms: []Platform{PlatformMacOS}},
			want:  false,
		},
		{
			name: "every dimension must agree",
			alias: Alias{
				Enabled:    true,
				Platforms:  []Platform{PlatformMacOS},
				Shells:     []Shell{ShellZsh},
				ProfileIDs: []string{"development"},
				DeviceIDs:  []string{"dev-2"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.alias.AppliesTo(dev); got != tt.want {
				t.Errorf("AppliesTo() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveFiltersAndSorts(t *testing.T) {
	dev := Device{
		Platform:   PlatformMacOS,
		Shell:      ShellZsh,
		ProfileIDs: []string{"development"},
	}

	cfg := Resolve(dev, []Alias{
		{Name: "zzz", Command: "echo z", Enabled: true},
		{Name: "aaa", Command: "echo a", Enabled: true},
		{Name: "windows_only", Command: "echo w", Enabled: true, Platforms: []Platform{PlatformWindows}},
		{Name: "off", Command: "echo o", Enabled: false},
		{Name: "mmm", Command: "echo m", Enabled: true, ProfileIDs: []string{"development"}},
	})

	want := []string{"aaa", "mmm", "zzz"}
	if len(cfg.Aliases) != len(want) {
		t.Fatalf("resolved %d aliases, want %d: %+v", len(cfg.Aliases), len(want), cfg.Aliases)
	}
	for i, name := range want {
		if cfg.Aliases[i].Name != name {
			t.Errorf("alias %d = %q, want %q", i, cfg.Aliases[i].Name, name)
		}
	}
	if cfg.Revision == "" {
		t.Error("Resolve must compute a revision")
	}
}

func TestRevisionTracksRenderedContent(t *testing.T) {
	dev := Device{Platform: PlatformMacOS, Shell: ShellZsh}
	base := []Alias{{Name: "a", Command: "echo a", Enabled: true}}

	original := Resolve(dev, base)

	t.Run("stable for identical input", func(t *testing.T) {
		if again := Resolve(dev, base); again.Revision != original.Revision {
			t.Errorf("revision changed for identical input: %s vs %s",
				original.Revision, again.Revision)
		}
	})

	t.Run("insensitive to input ordering", func(t *testing.T) {
		two := []Alias{
			{Name: "a", Command: "echo a", Enabled: true},
			{Name: "b", Command: "echo b", Enabled: true},
		}
		reversed := []Alias{two[1], two[0]}

		if Resolve(dev, two).Revision != Resolve(dev, reversed).Revision {
			t.Error("revision must not depend on the order aliases were declared in")
		}
	})

	t.Run("changes when a command changes", func(t *testing.T) {
		changed := Resolve(dev, []Alias{{Name: "a", Command: "echo b", Enabled: true}})
		if changed.Revision == original.Revision {
			t.Error("revision did not change after the command changed")
		}
	})

	t.Run("ignores fields that do not reach the file", func(t *testing.T) {
		tagged := Resolve(dev, []Alias{{
			Name: "a", Command: "echo a", Enabled: true,
			Tags: []string{"docker"},
			ID:   "some-uuid",
		}})
		if tagged.Revision != original.Revision {
			t.Error("tags and IDs do not affect rendered output, so they must not " +
				"change the revision and trigger a pointless rewrite")
		}
	})

	t.Run("changes with the target shell", func(t *testing.T) {
		other := Resolve(Device{Platform: PlatformLinux, Shell: ShellBash}, base)
		if other.Revision == original.Revision {
			t.Error("the same aliases render differently per shell, so the revision must differ")
		}
	})
}
