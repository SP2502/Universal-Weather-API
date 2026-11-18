package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
)

// MetricsBearer protects even an accidentally reverse-proxied loopback metrics
// endpoint. Disable the endpoint by default; never accept auth via URL query.
func MetricsBearer(next http.Handler, token string) http.Handler {
	expected := sha256.Sum256([]byte(token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		supplied := r.Header.Get("Authorization")
		if len(supplied) < 7 || supplied[:7] != "Bearer " {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		actual := sha256.Sum256([]byte(supplied[7:]))
		if subtle.ConstantTimeCompare(actual[:], expected[:]) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
