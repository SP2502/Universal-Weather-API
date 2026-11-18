package cache

import "testing"

func TestPhase07C_GeocodeIdentity(t *testing.T) {
	a := GeocodeV2KeyParams{Query: "Raipur", Country: "IN", Mode: "forward", Limit: 1}
	k := BuildGeocodeV2Key(a)
	a.Limit = 2
	if k == BuildGeocodeV2Key(a) {
		t.Fatal("limit not part of identity")
	}
	a.Limit = 1
	a.PreferredProvider = "geoapify"
	if k == BuildGeocodeV2Key(a) {
		t.Fatal("provider not part of identity")
	}
	a.PreferredProvider = ""
	a.Policy = "COST_FIRST"
	if k == BuildGeocodeV2Key(a) {
		t.Fatal("policy not part of identity")
	}
}
