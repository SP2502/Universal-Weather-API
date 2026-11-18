package decision

// RoutingPolicyMode defines the optimization strategy used by the decision engine.
type RoutingPolicyMode string

const (
	PolicyAvailabilityFirst RoutingPolicyMode = "AVAILABILITY_FIRST" // Default: Maximize reliability & health
	PolicyLatencyFirst      RoutingPolicyMode = "LATENCY_FIRST"      // Minimize response latency
	PolicyCostFirst         RoutingPolicyMode = "COST_FIRST"         // Prefer lowest cost class
	PolicyPriorityFirst     RoutingPolicyMode = "PRIORITY_FIRST"     // Strict operator-configured fallback priority
)

// RequestContext carries request-level constraints and routing preferences.
type RequestContext struct {
	Capability        string
	Product           string
	PreferredProvider string
	CountryCode       string
	Latitude          *float64
	Longitude         *float64
	AllowDegraded     bool
	PolicyMode        RoutingPolicyMode
}
