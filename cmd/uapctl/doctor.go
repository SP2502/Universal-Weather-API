package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strings"

	"github.com/universal-api/platform/internal/config"
	"github.com/universal-api/platform/internal/platformenv"
	"github.com/universal-api/platform/internal/providerpolicy"
	"github.com/universal-api/platform/internal/quota"
)

// doctor is deliberately offline: no upstream requests, database connection,
// DNS resolution, credentials in output, or mutations to the user's services.
// A successful report is NOT a deployment acceptance test.
type doctorCheck struct {
	Name   string `json:"name"`
	Result string `json:"result"`
	Detail string `json:"detail"`
}
type doctorReport struct {
	Mode                    string        `json:"mode"`
	Environment             string        `json:"environment"`
	Checks                  []doctorCheck `json:"checks"`
	LiveIntegrationVerified bool          `json:"live_integration_verified"`
}

func runDoctor(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	envFile := fs.String("env-file", "", "Optional configuration file")
	outputJSON := fs.Bool("json", false, "Output machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("doctor arguments: %w", err)
	}
	if len(fs.Args()) > 0 {
		return errors.New("unexpected doctor positional arguments")
	}
	if *envFile != "" {
		if err := platformenv.LoadFile(*envFile); err != nil {
			return errors.New("unable to load environment file; check path and syntax")
		}
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration invalid: %w", err)
	}
	report := doctorReport{Mode: "offline", Environment: cfg.Environment, LiveIntegrationVerified: false}
	add := func(name, result, detail string) {
		report.Checks = append(report.Checks, doctorCheck{name, result, detail})
	}
	add("configuration", "pass", "Gateway configuration parsed successfully")
	if policy := os.Getenv("APP_PROVIDER_POLICY_FILE"); policy != "" {
		if _, err := providerpolicy.LoadFile(policy); err != nil {
			add("provider_policy", "fail", "Policy file invalid or unavailable")
		} else {
			add("provider_policy", "pass", "Configured provider policy parses")
		}
	} else {
		add("provider_policy", "warning", "Default catalog is used; verify upstream permissions")
	}
	if geo := os.Getenv("APP_COUNTRY_POLYGONS_FILE"); geo != "" {
		if _, err := providerpolicy.LoadCountryIndex(geo); err != nil {
			add("country_boundaries", "fail", "Country boundary data invalid or unavailable")
		} else {
			add("country_boundaries", "pass", "Country boundary index parses")
		}
	} else {
		add("country_boundaries", "warning", "No verified country polygons configured")
	}
	budgets, err := quota.ParseBudgets(os.Getenv("APP_PROVIDER_QUOTAS_JSON"))
	if err != nil {
		add("provider_quotas", "fail", "Quota rules invalid")
	} else if len(budgets) == 0 {
		if cfg.Environment == "production" || cfg.Environment == "staging" {
			add("provider_quotas", "fail", "Production/staging requires explicit upstream provider budgets")
		} else {
			add("provider_quotas", "warning", "No upstream quota limits configured")
		}
	} else {
		add("provider_quotas", "pass", fmt.Sprintf("%d configured upstream budget rules", len(budgets)))
	}
	if cfg.Environment == "production" || cfg.Environment == "staging" {
		if cfg.CacheDriver != "redis" {
			add("shared_quotas", "fail", "Production/staging requires shared Redis for provider quota enforcement")
		} else if err != nil || len(budgets) == 0 {
			add("shared_quotas", "fail", "Production/staging quota protection is incomplete")
		} else {
			add("shared_quotas", "pass", "Redis and quotas declared; connectivity and provider coverage not tested")
		}
	}
	if key := os.Getenv("APP_DEV_CLIENT_KEY"); key != "" {
		if cfg.Environment != "development" || cfg.DBDriver != "memory" || !isLoopbackBind(cfg.BindHost) || len(key) < 24 || len(key) > 512 {
			add("developer_key", "fail", "Developer key requires loopback-only development and memory storage")
		} else {
			add("developer_key", "warning", "Operator-supplied development key configured; do not expose externally")
		}
	} else {
		add("developer_key", "pass", "No development bootstrap key configured")
	}
	add("live_acceptance", "pending", "PostgreSQL, Redis, provider and native service tests were not executed")
	failed := false
	for _, item := range report.Checks {
		if item.Result == "fail" {
			failed = true
		}
	}
	if *outputJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(out, "UWA offline doctor — %s (no network or database operations)\n", strings.ToUpper(cfg.Environment)); err != nil {
			return err
		}
		for _, c := range report.Checks {
			if _, err := fmt.Fprintf(out, "[%s] %s: %s\n", strings.ToUpper(c.Result), c.Name, c.Detail); err != nil {
				return err
			}
		}
	}
	if failed {
		return errors.New("offline doctor found configuration blockers; see report")
	}
	return nil
}

func isLoopbackBind(host string) bool {
	ip := net.ParseIP(host)
	return strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback())
}
