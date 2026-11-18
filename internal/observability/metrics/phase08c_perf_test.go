package metrics

import (
	"fmt"
	"testing"
)

func TestPhase08CLabelsEscapingAndStableOrder(t *testing.T) {
	cases := []struct {
		labels []Label
		want   string
	}{
		{nil, ""},
		{[]Label{{Name: "z", Value: "test"}}, `z="test"`},
		{[]Label{{Name: "z", Value: "Z"}, {Name: "a", Value: "A"}}, `a="A",z="Z"`},
		{[]Label{{Name: "x", Value: "a\\b\"c\nd"}}, `x="a\\b\"c\nd"`},
		{[]Label{{Name: "a", Value: "1"}, {Name: "a", Value: "2"}}, `a="1",a="2"`},
	}
	for i, tc := range cases {
		if got := labelsKey(tc.labels); got != tc.want {
			t.Errorf("case %d: got %q, want %q", i, got, tc.want)
		}
	}
	labels := make([]Label, 12)
	for i := range labels {
		labels[i] = Label{Name: fmt.Sprintf("label_%02d", 12-i), Value: fmt.Sprintf("value_%d", i)}
	}
	if labelsKey(labels) == "" {
		t.Fatal("large label list lost")
	}
}
func BenchmarkPhase08CMetricLabels(b *testing.B) {
	labels := []Label{{Name: "status_class", Value: "2xx"}, {Name: "capability", Value: "weather"}, {Name: "method", Value: "GET"}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if labelsKey(labels) == "" {
			b.Fatal("empty")
		}
	}
}
