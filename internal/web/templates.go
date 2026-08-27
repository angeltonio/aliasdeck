package web

import (
	"embed"
	"html/template"
)

// templateFS and staticFS are the two embedded trees the web UI ships:
// every HTML template and the vendored htmx + hand-written stylesheet,
// baked into the binary via go:embed — no Node, no bundler, nothing read
// from disk at runtime.
//
//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

// pageTemplates is every parsed template set this package's handlers
// execute against. html/template's ParseFS names a template after the
// {{define}} blocks it finds, so each set below is parsed independently
// to avoid two files defining "content" (aliases.html and devices.html,
// for instance) colliding in the same *template.Template.
type pageTemplates struct {
	login        *template.Template
	setup        *template.Template
	aliases      *template.Template
	aliasPanel   *template.Template
	profiles     *template.Template
	profilePanel *template.Template
	devices      *template.Template
	devicePanel  *template.Template
	preview      *template.Template
	devicesAdd   *template.Template
	mintResult   *template.Template
}

func loadTemplates() (*pageTemplates, error) {
	parse := func(files ...string) (*template.Template, error) {
		return template.New("pages").Funcs(template.FuncMap{
			"tr": func(lang language, key string, values ...any) string {
				if len(values) == 0 {
					return translate(lang, key)
				}
				return formatted(lang, key, values...)
			},
		}).ParseFS(templateFS, files...)
	}

	login, err := parse("templates/language_selector.html", "templates/login.html")
	if err != nil {
		return nil, err
	}
	setup, err := parse("templates/language_selector.html", "templates/setup.html")
	if err != nil {
		return nil, err
	}
	aliases, err := parse("templates/language_selector.html", "templates/base.html", "templates/alias_panel.html", "templates/aliases.html")
	if err != nil {
		return nil, err
	}
	aliasPanel, err := parse("templates/alias_panel.html")
	if err != nil {
		return nil, err
	}
	profiles, err := parse("templates/language_selector.html", "templates/base.html", "templates/profile_panel.html", "templates/profiles.html")
	if err != nil {
		return nil, err
	}
	profilePanel, err := parse("templates/profile_panel.html")
	if err != nil {
		return nil, err
	}
	devices, err := parse("templates/language_selector.html", "templates/base.html", "templates/device_panel.html", "templates/devices.html")
	if err != nil {
		return nil, err
	}
	devicePanel, err := parse("templates/device_panel.html")
	if err != nil {
		return nil, err
	}
	preview, err := parse("templates/language_selector.html", "templates/base.html", "templates/preview.html")
	if err != nil {
		return nil, err
	}
	devicesAdd, err := parse("templates/language_selector.html", "templates/base.html", "templates/devices_add.html")
	if err != nil {
		return nil, err
	}
	mintResult, err := parse("templates/device_mint_result.html")
	if err != nil {
		return nil, err
	}

	return &pageTemplates{
		login:        login,
		setup:        setup,
		aliases:      aliases,
		aliasPanel:   aliasPanel,
		profiles:     profiles,
		profilePanel: profilePanel,
		devices:      devices,
		devicePanel:  devicePanel,
		preview:      preview,
		devicesAdd:   devicesAdd,
		mintResult:   mintResult,
	}, nil
}
