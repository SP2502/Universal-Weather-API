package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMetricsBearer(t *testing.T) {
	secret := "super-long-operator-token-abcdefghij12345"
	h := MetricsBearer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }), secret)
	for _, tc := range []struct {
		header string
		code   int
	}{
		{"", 401}, {"Bearer wrong", 401}, {"Bearer " + secret, 200}, {"Basic " + secret, 401},
	} {
		req := httptest.NewRequest("GET", "/metrics", nil)
		req.Header.Set("Authorization", tc.header)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != tc.code {
			t.Errorf("header=%q status=%d want=%d", tc.header, w.Code, tc.code)
		}
	}
}
