package cache

import "testing"

func TestPhase07B_ForecastCacheIdentity(t *testing.T) {
	base := ForecastKeyParams{Latitude: 21.25123, Longitude: 81.63123, Product: "hourly_forecast"}
	key := BuildForecastCacheKey(base)
	if key == "" || key != BuildForecastCacheKey(base) {
		t.Fatal("unstable key")
	}
	constrained := base
	constrained.PreferredProvider = "met_norway"
	if key == BuildForecastCacheKey(constrained) {
		t.Fatal("provider constraint ignored")
	}
	constrained = base
	constrained.PolicyMode = "COST_FIRST"
	if key == BuildForecastCacheKey(constrained) {
		t.Fatal("routing policy ignored")
	}
	constrained = base
	constrained.Latitude += 0.001
	if key == BuildForecastCacheKey(constrained) {
		t.Fatal("different forecast coordinates collided")
	}
	if p := NewPolicyEngine().EvaluatePolicy("forecast", "met_norway"); !p.IsCacheable || p.StaleTTL != p.FreshTTL || p.AllowStaleIfError || p.AllowStaleWhileRevalidate {
		t.Fatalf("forecast must be fresh-only policy: %+v", p)
	}
}
