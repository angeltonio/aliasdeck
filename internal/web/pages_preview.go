package web

import (
	"errors"
	"net/http"

	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/store"
)

// previewRow is one alias as it stands for one device: whether that device
// receives it, and when it does not, which targeting dimension excluded it.
type previewRow struct {
	Name    string
	Command string
	Applies bool
	// Reason is already localized. The decision of which dimension failed is
	// domain.Alias.Miss's; this only phrases it.
	Reason string
}

type previewPageData struct {
	pageData
	Title  string
	Active string
	Device deviceRow
	Rows   []previewRow
	// Receives counts the rows a device actually gets, so the page can lead
	// with the number rather than making an operator count table rows.
	Receives int
}

// handleDevicePreview answers the question the targeting matrix makes hard:
// which aliases does this machine actually get, and why is that one missing?
//
// Targeting has five dimensions — enabled, platform, shell, group, and a
// per-device pin — so an alias can be absent for reasons that are invisible
// from either the alias list or the device list alone. This page resolves the
// whole set against one device and reports the excluded ones with their
// cause, rather than only listing what survives.
//
// It never writes and never triggers a sync: it answers what the next sync
// would deliver, using the same predicate the sync itself applies.
func (a *webapp) handleDevicePreview(w http.ResponseWriter, r *http.Request) {
	lang := requestLanguage(r)

	dev, err := a.store.Devices().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, translate(lang, "error.device_missing"), http.StatusNotFound)
			return
		}
		http.Error(w, translate(lang, "error.device_load"), http.StatusInternalServerError)
		return
	}

	aliases, err := a.store.Aliases().List(r.Context())
	if err != nil {
		http.Error(w, translate(lang, "error.alias_load"), http.StatusInternalServerError)
		return
	}

	// SortAliases is what resolution itself orders by, so the preview lists
	// them in the order the generated file would.
	sorted := make([]domain.Alias, len(aliases))
	copy(sorted, aliases)
	domain.SortAliases(sorted)

	rows := make([]previewRow, 0, len(sorted))
	receives := 0
	for _, al := range sorted {
		miss := al.Miss(dev)
		row := previewRow{Name: al.Name, Command: al.Command, Applies: miss == domain.MissNone}
		if row.Applies {
			receives++
		} else {
			row.Reason = localizeMiss(lang, miss, dev)
		}
		rows = append(rows, row)
	}

	view := pageDataFor(r)
	a.writePage(w, r, http.StatusOK, a.tmpl.preview, "base", previewPageData{
		pageData: view,
		Title:    formatted(view.Lang, "preview.title", dev.Name),
		Active:   "devices",
		Device:   deviceRow{ID: dev.ID, Name: dev.Name, Platform: string(dev.Platform), Shell: string(dev.Shell)},
		Rows:     rows,
		Receives: receives,
	})
}

// localizeMiss phrases one domain.TargetingMiss for the operator, naming the
// concrete value that excluded the device where there is one — "not targeted
// at macos" is actionable in a way "wrong platform" is not.
func localizeMiss(lang language, miss domain.TargetingMiss, dev domain.Device) string {
	switch miss {
	case domain.MissDisabled:
		return translate(lang, "preview.miss_disabled")
	case domain.MissPlatform:
		return formatted(lang, "preview.miss_platform", string(dev.Platform))
	case domain.MissShell:
		return formatted(lang, "preview.miss_shell", string(dev.Shell))
	case domain.MissProfile:
		return translate(lang, "preview.miss_profile")
	case domain.MissDevice:
		return translate(lang, "preview.miss_device")
	default:
		return ""
	}
}
