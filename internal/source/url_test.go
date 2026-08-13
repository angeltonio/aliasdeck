package source

import (
	"strings"
	"testing"
)

// TestValidateServerURL pins server-source spec's "HTTPS Required Unless
// Loopback or Explicit Opt-Out" scenarios verbatim, plus the unparseable and
// non-http-scheme rejections task 7.1 adds.
func TestValidateServerURL(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		allowHTTP bool
		wantErr   bool
	}{
		{
			name: "https is always accepted",
			raw:  "https://aliases.example.com",
		},
		{
			name: "loopback http is allowed without the opt-out",
			raw:  "http://127.0.0.1:8080",
		},
		{
			name: "loopback IPv6 http is allowed",
			raw:  "http://[::1]:8080",
		},
		{
			name: "localhost by name is allowed",
			raw:  "http://localhost:8080",
		},
		{
			name:    "remote http is refused without the opt-out",
			raw:     "http://aliases.example.com",
			wantErr: true,
		},
		{
			name:      "remote http is accepted with the explicit opt-out",
			raw:       "http://aliases.example.com",
			allowHTTP: true,
		},
		{
			name:    "unparseable URL is rejected",
			raw:     "http://[::1",
			wantErr: true,
		},
		{
			name:    "non-http scheme is rejected",
			raw:     "ftp://aliases.example.com",
			wantErr: true,
		},
		{
			name:    "empty scheme is rejected",
			raw:     "aliases.example.com",
			wantErr: true,
		},
		{
			name:      "opt-out does not rescue a non-http scheme",
			raw:       "ftp://aliases.example.com",
			allowHTTP: true,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateServerURL(tt.raw, tt.allowHTTP)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateServerURL(%q, %v) error = %v, wantErr %v", tt.raw, tt.allowHTTP, err, tt.wantErr)
			}
		})
	}
}

// TestValidateServerURLNamesTheInsecureURL pins the "fails before any
// request is sent, naming the insecure URL" half of the scenario — an error
// string a caller cannot act on is as good as no error at all.
func TestValidateServerURLNamesTheInsecureURL(t *testing.T) {
	const raw = "http://aliases.example.com"
	err := ValidateServerURL(raw, false)
	if err == nil {
		t.Fatal("ValidateServerURL() = nil, want an error for a remote http URL")
	}
	if got := err.Error(); !strings.Contains(got, raw) {
		t.Errorf("ValidateServerURL() error = %q, want it to name %q", got, raw)
	}
}
