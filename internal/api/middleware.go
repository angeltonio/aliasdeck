package api

import "net/http"

// maxBodyBytes is the request-body ceiling (design's Bounded Operations
// table, "Request body"): it matches the existing 1 MiB config cap the
// client side already enforces, so a request body larger than what the
// client itself would ever produce is rejected before the server buffers
// any meaningful fraction of it.
const maxBodyBytes = 1 << 20

// withMaxBytes wraps r.Body in http.MaxBytesReader so any read past
// maxBodyBytes fails immediately with a *http.MaxBytesError instead of
// continuing to buffer — this is what makes "oversized body rejected
// before it is fully read" true rather than merely "rejected after being
// fully read and then measured".
func withMaxBytes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next.ServeHTTP(w, r)
	})
}
