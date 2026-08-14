package web

import (
	"bytes"
	"strings"
	"testing"
)

func TestMintCommandIsSingleSafeFlow(t *testing.T) {
	got := mintCommand("https://aliasdeck.test", "TOKEN_VALUE")
	want := `aliasdeck init --yes && aliasdeck register --url 'https://aliasdeck.test' --token 'TOKEN_VALUE' && aliasdeck sync && . "${ALIASDECK_HOME:-${XDG_CONFIG_HOME:-$HOME/.config}/aliasdeck}/aliases.${SHELL##*/}"`
	if got != want {
		t.Fatalf("mintCommand() = %q, want %q", got, want)
	}

	quoted := shellQuote("value'with-quote")
	if quoted != `'value'\''with-quote'` {
		t.Fatalf("shellQuote() = %q, want a safely escaped single-quoted value", quoted)
	}

	if got := mintCommand("https://aliasdeck.test/$(touch pwned)", "TOKEN_VALUE"); !strings.Contains(got, "--url 'https://aliasdeck.test/$(touch pwned)'") {
		t.Fatalf("mintCommand() did not quote URL shell syntax: %q", got)
	}
}

func TestMintResultRendersCopyableCommand(t *testing.T) {
	templates, err := loadTemplates()
	if err != nil {
		t.Fatalf("loadTemplates() returned an error: %v", err)
	}

	var rendered bytes.Buffer
	err = templates.mintResult.ExecuteTemplate(&rendered, "device_mint_result", mintResultData{
		Command:   mintCommand("https://aliasdeck.test", "TOKEN_VALUE"),
		ExpiresAt: "2030-01-01 00:00:00 UTC",
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate() returned an error: %v", err)
	}

	output := rendered.String()
	command := `aliasdeck init --yes &amp;&amp; aliasdeck register --url &#39;https://aliasdeck.test&#39; --token &#39;TOKEN_VALUE&#39; &amp;&amp; aliasdeck sync &amp;&amp; . &#34;${ALIASDECK_HOME:-${XDG_CONFIG_HOME:-$HOME/.config}/aliasdeck}/aliases.${SHELL##*/}&#34;`
	if !strings.Contains(output, command) {
		t.Fatalf("rendered mint result does not contain the safe command: %q", output)
	}
	if strings.Contains(output, "aliasdeck register --url") && strings.Contains(output, "\naliasdeck sync") {
		t.Fatal("rendered mint result still presents registration and sync as separate commands")
	}
}
