package provider

type Policy struct {
	AllowDegraded    bool
	MinHealthScore   float64
	PreferredCountry string
}

func DefaultPolicy() Policy {
	return Policy{
		AllowDegraded:  true,
		MinHealthScore: 0.1,
	}
}

// FilterEligible filters a list of providers according to eligibility policy and health evaluation.
func FilterEligible[T Provider](providers []T, healthEvaluator *HealthEvaluator, policy Policy) []T {
	var eligible []T

	for _, p := range providers {
		m := p.Manifest()

		// 1. Lifecycle status filter
		if m.Status == StatusDisabled || m.Status == StatusRemoved {
			continue
		}
		if m.Status == StatusDegraded && !policy.AllowDegraded {
			continue
		}

		// 2. Health score filter
		if healthEvaluator != nil {
			health := healthEvaluator.GetHealth(m.ID)
			if health.Score < policy.MinHealthScore {
				continue
			}
		}

		eligible = append(eligible, p)
	}

	return eligible
}
