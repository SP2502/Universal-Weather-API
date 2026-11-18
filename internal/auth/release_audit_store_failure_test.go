package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/universal-api/platform/internal/database"
)

type failingKeyLookup struct {
	database.Store
	failure error
}

func (f failingKeyLookup) GetAPIKeyByHash(context.Context, string) (*database.APIKey, error) {
	return nil, f.failure
}

func TestReleaseAuditAuthStoreErrorsAreNot401(t *testing.T) {
	cases := []struct {
		name    string
		failure error
		status  int
		code    string
	}{
		{"not found", database.ErrNotFound, http.StatusUnauthorized, "UNAUTHORIZED"},
		{"transient outage", errors.New("postgres connection password=SENSITIVE_TOKEN"), http.StatusServiceUnavailable, "AUTH_BACKEND_UNAVAILABLE"},
		{"revoked", fmtWrapped(database.ErrKeyRevoked), http.StatusUnauthorized, "KEY_REVOKED"},
		{"expired", fmtWrapped(database.ErrKeyExpired), http.StatusUnauthorized, "KEY_EXPIRED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := failingKeyLookup{Store: database.NewMemoryStore(), failure: tc.failure}
			handler := MiddlewareWithLastUsed(NewAuthenticator(store), nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("reached protected handler") }))
			req := httptest.NewRequest(http.MethodGet, "/v1/weather", nil)
			req.Header.Set("X-API-Key", "SYNTHETIC_SECRET")
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code != tc.status {
				t.Fatalf("status %d, expected %d: %s", rr.Code, tc.status, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), tc.code) {
				t.Fatalf("wrong code: %s", rr.Body.String())
			}
			if strings.Contains(rr.Body.String(), "SENSITIVE_TOKEN") || strings.Contains(rr.Body.String(), "SYNTHETIC_SECRET") {
				t.Fatalf("credential or storage error leaked: %s", rr.Body.String())
			}
			if tc.status == http.StatusServiceUnavailable {
				if rr.Header().Get("Retry-After") != "1" || !strings.Contains(rr.Body.String(), `"retryable":true`) {
					t.Fatalf("service availability response missing retry semantics: %s", rr.Body.String())
				}
			}
		})
	}
}

func fmtWrapped(err error) error { return errors.Join(errors.New("wrapper"), err) }
