package server

import (
	"context"
	"strings"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/store"
)

func TestResolvePublicURL(t *testing.T) {
	tests := []struct {
		name, raw, want, wantErr string
	}{
		{name: "absent"},
		{name: "remote https", raw: "https://aliases.example:8443", want: "https://aliases.example:8443"},
		{name: "loopback http", raw: "http://127.0.0.1:8088/", want: "http://127.0.0.1:8088"},
		{name: "localhost http", raw: "http://localhost:8088", want: "http://localhost:8088"},
		{name: "remote plaintext", raw: "http://aliases.example", wantErr: "requires https"},
		{name: "credentials", raw: "https://user:pass@aliases.example", wantErr: "only scheme and authority"},
		{name: "path", raw: "https://aliases.example/control", wantErr: "only scheme and authority"},
		{name: "query", raw: "https://aliases.example?x=1", wantErr: "only scheme and authority"},
		{name: "fragment", raw: "https://aliases.example#x", wantErr: "only scheme and authority"},
		{name: "relative", raw: "//aliases.example", wantErr: "absolute origin"},
		{name: "wrong scheme", raw: "ftp://aliases.example", wantErr: "scheme must be http or https"},
		{name: "bad port", raw: "https://aliases.example:99999", wantErr: "invalid port"},
		{name: "service name port", raw: "https://aliases.example:http", wantErr: "absolute origin"},
		{name: "zero port", raw: "https://aliases.example:0", wantErr: "invalid port"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolvePublicURL(Config{Getenv: func(key string) string {
				if key == publicURLEnv {
					return tt.raw
				}
				return ""
			}})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("resolvePublicURL() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tt.want == "" {
				if got != nil {
					t.Fatalf("resolvePublicURL() = %v, want nil", got)
				}
				return
			}
			if got == nil || got.String() != tt.want {
				t.Fatalf("resolvePublicURL() = %v, want %q", got, tt.want)
			}
		})
	}
}

func TestRunRejectsInvalidPublicURLBeforeOpeningStore(t *testing.T) {
	opened := false
	err := Run(context.Background(), Config{
		Getenv: func(key string) string {
			if key == publicURLEnv {
				return "http://remote.example"
			}
			return ""
		},
		OpenStore: func(context.Context) (store.Store, error) {
			opened = true
			return nil, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "requires https") {
		t.Fatalf("Run() error = %v, want invalid public URL", err)
	}
	if opened {
		t.Fatal("Run opened the store before rejecting invalid public URL")
	}
}
