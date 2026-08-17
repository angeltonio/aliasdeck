// Package releasecheck binds published version labels to the client version
// embedded in the exact source checkout being built.
package releasecheck

import (
	"fmt"
	"regexp"

	"github.com/angeltonio/aliasdeck/internal/app"
)

var stableTag = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

// Validate requires a stable release tag that matches app.Version and a
// source ref naming that same tag. Requiring the tag as source_ref prevents a
// manual container dispatch from labeling an arbitrary branch or commit as a
// released version.
func Validate(version, sourceRef string) error {
	if !stableTag.MatchString(version) {
		return fmt.Errorf("release version %q must be a stable vX.Y.Z tag", version)
	}
	want := "v" + app.Version
	if version != want {
		return fmt.Errorf("release tag %q does not match client version %q (expected %q)", version, app.Version, want)
	}
	if sourceRef != version {
		return fmt.Errorf("source ref %q must equal release tag %q", sourceRef, version)
	}
	return nil
}
