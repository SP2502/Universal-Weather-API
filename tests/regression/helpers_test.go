package regression

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/universal-api/platform/internal/auth"
	"github.com/universal-api/platform/internal/cache"
	"github.com/universal-api/platform/internal/capability"
	"github.com/universal-api/platform/internal/database"
	"github.com/universal-api/platform/internal/health"
	"github.com/universal-api/platform/internal/normalization"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/internal/providerpolicy"
	"github.com/universal-api/platform/internal/routing"
	"github.com/universal-api/platform/internal/routing/decision"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

const key = "audit-local-client-key-not-a-secret"

type envelope struct {
	Data json.RawMessage `json:"data"`
	Meta struct {
		Provider string `json:"provider"`
		Cached   bool   `json:"cached"`
		Fallback bool   `json:"fallback"`
	} `json:"meta"`
}

func ow(lat, lon float64, epoch int64) string {
	return fmt.Sprintf(`{"coord":{"lat":%f,"lon":%f},"main":{"temp":273.15,"humidity":0},"weather":[{"id":800,"main":"Clear"}],"dt":%d}`, lat, lon, epoch)
}
func wa() string {
	return fmt.Sprintf(`{"location":{"lat":21.2,"lon":81.6,"tz_id":"Asia/Kolkata"},"current":{"temp_c":0,"humidity":0,"last_updated_epoch":%d,"condition":{"code":1000}}}`, time.Now().Unix())
}
func handler(t *testing.T, reg *provider.Registry, store cache.Store) http.Handler {
	t.Helper()
	db := database.NewMemoryStore()
	t.Cleanup(func() { db.Close() })
	db.CreateAPIKey(context.Background(), &database.APIKey{ID: "audit", KeyHash: database.HashKey(key), Scopes: []string{"weather:read"}, IsActive: true})
	eng := decision.NewDecisionEngine(reg, reg.HealthEvaluator())
	cat, e := providerpolicy.NewCatalog(providerpolicy.DefaultDocument())
	if e != nil {
		t.Fatal(e)
	}
	eng.SetPolicyCatalog(cat, "noncommercial")
	eng.SetLeanRanking(true)
	h := capability.NewWeatherHandler(reg, eng, normalization.NewWeatherNormalizer(), nil, store, cache.NewPolicyEngine())
	return routing.NewRouter(routing.RouterDependencies{HealthHandler: health.NewHandler("audit"), WeatherHandler: h, AuthMiddleware: auth.Middleware(auth.NewAuthenticator(db), db), ReleaseProfile: "lean_v1"})
}
func request(t *testing.T, h http.Handler) (int, envelope, []byte) {
	t.Helper()
	r := httptest.NewRequest("GET", "/v1/weather?lat=21.2&lon=81.6", nil)
	r.Header.Set("X-API-Key", key)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	t.Logf("CONTRACT_JSON=%s", w.Body.String())
	var e envelope
	json.Unmarshal(w.Body.Bytes(), &e)
	return w.Code, e, w.Body.Bytes()
}
func server(t *testing.T, body string, status int, cc string, calls *atomic.Int64) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if cc != "" {
			w.Header().Set("Cache-Control", cc)
		}
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(s.Close)
	return s
}
