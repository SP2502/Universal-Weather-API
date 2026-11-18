package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/universal-api/platform/internal/quota"
)

// validatePhase06Activation is intentionally side-effect free: --check-config
// runs without contacting a database, Redis, or a weather provider.
func validatePhase06Activation() error {
	names := []string{"APP_ENABLE_GEOAPIFY", "APP_ENABLE_MET_NORWAY", "APP_ENABLE_OPEN_METEO_FORECAST", "APP_ENABLE_OPEN_METEO_GEOCODING", "APP_ENABLE_OPEN_METEO_AIR_QUALITY", "APP_ENABLE_US_NWS", "APP_ENABLE_BRIGHT_SKY", "APP_ENABLE_OPEN_METEO_HISTORY", "APP_ENABLE_OPEN_METEO_MARINE", "APP_ENABLE_SUNRISE_SUNSET", "APP_ENABLE_US_NWS_ALERTS"}
	for _, name := range names {
		switch os.Getenv(name) {
		case "", "false", "true":
		default:
			return fmt.Errorf("%s must be true or false", name)
		}
	}
	if os.Getenv("APP_ENABLE_US_NWS") == "true" || os.Getenv("APP_ENABLE_US_NWS_ALERTS") == "true" {
		agent := strings.TrimSpace(os.Getenv("APP_NWS_USER_AGENT"))
		if len(agent) < 12 || len(agent) > 256 || strings.ContainsAny(agent, "\n\r") || (!strings.Contains(agent, "@") && !strings.Contains(agent, "https://")) {
			return errors.New("uS NWS requires identifying APP_NWS_USER_AGENT with operator contact")
		}
	}
	// Country-restricted active alerts cannot route from coordinates until an
	// operator-reviewed country polygon index is actually configured.
	if os.Getenv("APP_ENABLE_US_NWS_ALERTS") == "true" && strings.TrimSpace(os.Getenv("APP_COUNTRY_POLYGONS_FILE")) == "" {
		return errors.New("uS NWS alerts require APP_COUNTRY_POLYGONS_FILE with verified US boundaries")
	}
	met := os.Getenv("APP_ENABLE_MET_NORWAY") == "true"
	geo := os.Getenv("APP_ENABLE_GEOAPIFY") == "true"
	omForecast := os.Getenv("APP_ENABLE_OPEN_METEO_FORECAST") == "true"
	omGeo := os.Getenv("APP_ENABLE_OPEN_METEO_GEOCODING") == "true"
	omAQ := os.Getenv("APP_ENABLE_OPEN_METEO_AIR_QUALITY") == "true"
	omHistory := os.Getenv("APP_ENABLE_OPEN_METEO_HISTORY") == "true"
	omMarine := os.Getenv("APP_ENABLE_OPEN_METEO_MARINE") == "true"
	if met {
		agent := strings.TrimSpace(os.Getenv("APP_MET_NORWAY_USER_AGENT"))
		if len(agent) < 12 || len(agent) > 256 || strings.ContainsAny(agent, "\n\r") || (!strings.Contains(agent, "@") && !strings.Contains(agent, "https://")) {
			return errors.New("mET Norway requires an identifying APP_MET_NORWAY_USER_AGENT with operator contact")
		}
	}
	if (omForecast || omGeo || omAQ || omHistory || omMarine) && os.Getenv("APP_DEPLOYMENT_USE") != "noncommercial" {
		return errors.New("open-Meteo hosted free integration requires APP_DEPLOYMENT_USE=noncommercial")
	}
	budgets, err := quota.ParseBudgets(os.Getenv("APP_PROVIDER_QUOTAS_JSON"))
	if err != nil {
		return err
	}
	if geo {
		b, ok := budgets["geoapify"]
		if !ok || b.PerDay <= 0 || b.PerDay > 2850 || b.PerMinute <= 0 || b.PerMinute > 240 || b.PerSecond > 4 {
			return errors.New("geoapify requires an upstream quota budget with per_day <= 2850 and per_minute <= 240")
		}
	}
	// When multiple hosted Open-Meteo adapters are active, require a shared
	// quota. A single adapter remains compatible with the existing budget.
	enabledCount := 0
	for _, active := range []bool{omForecast, omGeo, omAQ, omHistory, omMarine} {
		if active {
			enabledCount++
		}
	}
	if enabledCount > 1 {
		group := ""
		for _, id := range []struct {
			name    string
			enabled bool
		}{{"open_meteo_forecast", omForecast}, {"open_meteo_geocoding", omGeo}, {"open_meteo_air_quality", omAQ}, {"open_meteo_history", omHistory}, {"open_meteo_marine", omMarine}} {
			if !id.enabled {
				continue
			}
			b, ok := budgets[id.name]
			if !ok || b.Group == "" {
				return errors.New("hosted Open-Meteo adapters require a shared quota group")
			}
			if group != "" && b.Group != group {
				return errors.New("hosted Open-Meteo adapters must use the same quota group")
			}
			group = b.Group
		}
	}

	if omForecast || omGeo || omAQ || omHistory || omMarine {
		for _, entry := range []struct {
			id      string
			enabled bool
		}{{"open_meteo_forecast", omForecast}, {"open_meteo_geocoding", omGeo}, {"open_meteo_air_quality", omAQ}, {"open_meteo_history", omHistory}, {"open_meteo_marine", omMarine}} {
			if !entry.enabled {
				continue
			}
			b, ok := budgets[entry.id]
			if !ok || b.PerDay <= 0 || b.PerDay > 4400 || b.PerMinute <= 0 || b.PerMinute > 250 {
				return errors.New("open-Meteo hosted integration requires per-provider per_day <= 4400 and per_minute <= 250")
			}
		}
	}
	// Neither archive range requests nor marine model queries have a fixed
	// vendor cost of one HTTP request. Require an explicitly reviewed weighted
	// charge, rather than accidentally counting such calls as 1 generic unit.
	for _, item := range []struct {
		id      string
		enabled bool
	}{{"open_meteo_history", omHistory}, {"open_meteo_marine", omMarine}} {
		if !item.enabled {
			continue
		}
		b, ok := budgets[item.id]
		if !ok || b.Unit != "weighted_calls" || (b.CostMilli == 0 && len(b.ProductCostsMilli) == 0) {
			return fmt.Errorf("%s requires weighted_calls and an operator-reviewed quota cost", item.id)
		}
	}
	return nil
}
