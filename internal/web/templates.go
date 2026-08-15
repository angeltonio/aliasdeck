package web

import (
	"embed"
	"html/template"
)

// templateFS and staticFS are the two embedded trees this prototype ships:
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
	login      *template.Template
	setup      *template.Template
	aliases    *template.Template
	aliasPanel *template.Template
	devices    *template.Template
	devicesAdd *template.Template
	mintResult *template.Template
}

func loadTemplates() (*pageTemplates, error) {
	parse := func(files ...string) (*template.Template, error) {
		return template.ParseFS(templateFS, files...)
	}

	login, err := parse("templates/login.html")
	if err != nil {
		return nil, err
	}
	setup, err := parse("templates/setup.html")
	if err != nil {
		return nil, err
	}
	aliases, err := parse("templates/base.html", "templates/alias_panel.html", "templates/aliases.html")
	if err != nil {
		return nil, err
	}
	aliasPanel, err := parse("templates/alias_panel.html")
	if err != nil {
		return nil, err
	}
	devices, err := parse("templates/base.html", "templates/devices.html")
	if err != nil {
		return nil, err
	}
	devicesAdd, err := parse("templates/base.html", "templates/devices_add.html")
	if err != nil {
		return nil, err
	}
	mintResult, err := parse("templates/device_mint_result.html")
	if err != nil {
		return nil, err
	}

	return &pageTemplates{
		login:      login,
		setup:      setup,
		aliases:    aliases,
		aliasPanel: aliasPanel,
		devices:    devices,
		devicesAdd: devicesAdd,
		mintResult: mintResult,
	}, nil
}
