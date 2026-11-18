package main

import (
	"fmt"
	"strings"
)

// requiredCapabilities defaults to the historical current-weather service.
// Forecast-only deployments must explicitly opt in, not silently weaken
// readiness requirements when weather credentials are missing.
func requiredCapabilities(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return []string{"weather"}, nil
	}
	seen := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		cap := strings.TrimSpace(part)
		if cap != "weather" && cap != "forecast" && cap != "air_quality" && cap != "history" && cap != "astronomy" && cap != "marine" && cap != "alerts" {
			return nil, fmt.Errorf("APP_REQUIRED_CAPABILITIES accepts only weather,forecast,air_quality,history,astronomy,marine,alerts")
		}
		if seen[cap] {
			return nil, fmt.Errorf("APP_REQUIRED_CAPABILITIES contains duplicate %s", cap)
		}
		seen[cap] = true
	}
	var result []string
	for _, cap := range []string{"weather", "forecast", "air_quality", "history", "astronomy", "marine", "alerts"} {
		if seen[cap] {
			result = append(result, cap)
		}
	}
	return result, nil
}

func requires(required []string, capability string) bool {
	for _, value := range required {
		if value == capability {
			return true
		}
	}
	return false
}
