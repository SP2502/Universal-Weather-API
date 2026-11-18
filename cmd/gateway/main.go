package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/universal-api/platform/internal/audit"
	"github.com/universal-api/platform/internal/auth"
	"github.com/universal-api/platform/internal/cache"
	"github.com/universal-api/platform/internal/capability"
	"github.com/universal-api/platform/internal/config"
	"github.com/universal-api/platform/internal/database"
	"github.com/universal-api/platform/internal/georesolve"
	"github.com/universal-api/platform/internal/health"
	"github.com/universal-api/platform/internal/http/middleware"
	"github.com/universal-api/platform/internal/normalization"
	"github.com/universal-api/platform/internal/observability/logging"
	"github.com/universal-api/platform/internal/observability/metrics"
	"github.com/universal-api/platform/internal/observability/tracing"
	"github.com/universal-api/platform/internal/platformenv"
	"github.com/universal-api/platform/internal/platformlog"
	"github.com/universal-api/platform/internal/platformsvc"
	"github.com/universal-api/platform/internal/providerpolicy"
	"github.com/universal-api/platform/internal/quota"
	"github.com/universal-api/platform/internal/routing"
	"github.com/universal-api/platform/internal/routing/decision"
	"github.com/universal-api/platform/internal/usage"
)

const windowsServiceName = "UniversalWeatherAPI"

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Printf("gateway: %v", err)
		os.Exit(1)
	}
}

// Parse operator options independently of provider settings so Windows SCM and
// systemd can pass only paths, never credentials, through process arguments.
func run(args []string) error {
	fs := flag.NewFlagSet("gateway", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	envFile := fs.String("env-file", "", "Optional KEY=value config file; process env has priority")
	checkConfig := fs.Bool("check-config", false, "Validate configuration and exit without connecting")
	service := fs.Bool("service", false, "Run under Windows Service Control Manager")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if *envFile != "" {
		if err := platformenv.LoadFile(*envFile); err != nil {
			return err
		}
	}
	if err := validatePhase06Activation(); err != nil {
		return err
	}
	profile, profileErr := releaseProfile()
	if profileErr != nil {
		return profileErr
	}
	if err := validateLeanOptions(profile); err != nil {
		return err
	}
	if _, err := requiredCapabilities(os.Getenv("APP_REQUIRED_CAPABILITIES")); err != nil {
		return err
	}
	if _, err := parseGeocodeCacheAllowlist(os.Getenv("APP_GEOCODE_CACHE_ALLOWED_PROVIDERS")); err != nil {
		return err
	}
	if err := validateDevClientKeyConfiguration(); err != nil {
		return err
	}
	if *checkConfig {
		if _, err := middleware.ParseTrustedProxies(os.Getenv("APP_TRUSTED_PROXY_CIDRS")); err != nil {
			return err
		}
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		enabled, metricsErr := metricsModeAllowed(os.Getenv("APP_METRICS_MODE"), cfg.BindHost)
		if metricsErr != nil {
			return metricsErr
		}
		if enabled && len(os.Getenv("APP_METRICS_TOKEN")) < 32 {
			return errors.New("APP_METRICS_TOKEN must contain at least 32 characters when metrics are enabled")
		}
		// Inspect exactly the same credential-dependent provider registry as startup.
		// Constructors are side-effect-free: no database, Redis, DNS, or provider calls.
		budgets, err := quota.ParseBudgets(os.Getenv("APP_PROVIDER_QUOTAS_JSON"))
		if err != nil {
			return err
		}
		if cfg.Environment == "production" || cfg.Environment == "staging" {
			preflightSecrets, err := configuredSecretStore(cfg)
			if err != nil {
				return errors.New("unable to initialize provider secret source")
			}
			preflightRegistry, configuredWeather := registerConfiguredProviders(preflightSecrets, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err := validateProviderQuotaCoverage(cfg, preflightRegistry, budgets, cfg.CacheDriver == "redis"); err != nil {
				return err
			}
			required, err := requiredCapabilities(os.Getenv("APP_REQUIRED_CAPABILITIES"))
			if err != nil {
				return err
			}
			if requires(required, "weather") && configuredWeather == 0 {
				return errors.New("no configured weather provider")
			}
			policies, err := configuredPolicies()
			if err != nil {
				return err
			}
			if _, _, ok := configuredProviderChecker(preflightRegistry, policies, deploymentUse(), required).Check(); !ok {
				return errors.New("no policy-permitted providers configured for one or more required capabilities")
			}
		}

		if mode := os.Getenv("APP_DEPLOYMENT_USE"); mode != "" && mode != "unspecified" && mode != "internal" && mode != "noncommercial" && mode != "commercial" {
			return errors.New("invalid APP_DEPLOYMENT_USE")
		}
		if mode := os.Getenv("APP_WEATHER_PLACE_RESOLVER"); mode != "" && mode != "disabled" && mode != "strict" {
			return errors.New("invalid APP_WEATHER_PLACE_RESOLVER")
		}
		if path := os.Getenv("APP_PROVIDER_POLICY_FILE"); path != "" {
			if _, err := providerpolicy.LoadFile(path); err != nil {
				return fmt.Errorf("provider policy: %w", err)
			}
		}
		if path := os.Getenv("APP_COUNTRY_POLYGONS_FILE"); path != "" {
			if _, err := providerpolicy.LoadCountryIndex(path); err != nil {
				return fmt.Errorf("country index: %w", err)
			}
		}
		fmt.Printf("Configuration valid: environment=%s, bind=%s, database=%s, cache=%s\n", cfg.Environment, cfg.ListenAddress(), cfg.DBDriver, cfg.CacheDriver)
		return nil
	}
	if *service {
		return platformsvc.RunService(windowsServiceName, runGateway)
	}
	return platformsvc.RunConsole(runGateway)
}

func runGateway(ctx context.Context, ready func()) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := validatePhase06Activation(); err != nil {
		return err
	}
	profile, profileErr := releaseProfile()
	if profileErr != nil {
		return profileErr
	}
	if err := validateLeanOptions(profile); err != nil {
		return err
	}
	if _, err := requiredCapabilities(os.Getenv("APP_REQUIRED_CAPABILITIES")); err != nil {
		return err
	}

	var output io.Writer = os.Stdout
	if cfg.LogFile != "" {
		file, err := platformlog.OpenRotatingFile(cfg.LogFile, 10<<20, 5)
		if err != nil {
			return err
		}
		defer file.Close()
		output = file
	}
	logger := logging.NewLogger(cfg.LogLevel, output)
	// Some existing adapter and capability paths log through slog.Default().
	// Install the same redaction boundary for those paths.
	slog.SetDefault(logger)
	auditLogger := audit.NewAuditLogger(output)
	logger.Info("Universal API Platform Gateway initializing", "env", cfg.Environment, "version", cfg.Version)

	// 1. Build the same secret resolution chain used by offline preflight.
	secretStore, err := configuredSecretStore(cfg)
	if err != nil {
		return errors.New("unable to initialize provider secret source")
	}
	logger.Info("Configured secret store", "driver", cfg.SecretDriver)

	// 2. Initialize durable production auth persistence. Never silently replace
	// an explicitly configured PostgreSQL backend with an empty memory store.
	store, err := initializeStore(cfg)
	if err != nil {
		logger.Error("Required database initialization failed; refusing to start")
		return errors.New("required authentication storage unavailable")
	}
	defer store.Close()
	logger.Info("Authentication storage initialized", "driver", cfg.DBDriver)

	if devKey := os.Getenv("APP_DEV_CLIENT_KEY"); devKey != "" {
		// Deliberate opt-in only. Never start with a known key or expose a
		// developer key on a non-loopback listener (including Docker 0.0.0.0).
		if err := validateDevClientKeyConfiguration(); err != nil {
			return err
		}
		_ = store.CreateAPIKey(context.Background(), &database.APIKey{
			ID:        "key-dev-1",
			Name:      "Development Key",
			KeyPrefix: "uap_live_",
			KeyHash:   database.HashKey(devKey),
			Scopes:    []string{"weather:read", "geocode:read", "forecast:read", "air-quality:read", "history:read", "astronomy:read", "marine:read", "alerts:read"},
			IsActive:  true,
			CreatedAt: time.Now().UTC(),
		})
		logger.Info("Seeded development API key", "key_id", "key-dev-1", "prefix", "uap_live_", "scopes", "weather:read, geocode:read, forecast:read, air-quality:read, history:read, astronomy:read, marine:read, alerts:read")
		auditLogger.Log(context.Background(), audit.Event{
			Type:     audit.EventAPIKeyCreated,
			ActorID:  "system_bootstrap",
			Details:  "Seeded development API key key-dev-1",
			Severity: "INFO",
		})
	}

	// 3. Register only providers with their required credentials configured.
	required, err := requiredCapabilities(os.Getenv("APP_REQUIRED_CAPABILITIES"))
	if err != nil {
		return err
	}
	registry, weatherCount := registerConfiguredProviders(secretStore, logger)
	if (cfg.Environment == "production" || cfg.Environment == "staging") && requires(required, "weather") && weatherCount == 0 {
		logger.Error("No configured weather provider; refusing to start in production/staging")
		return errors.New("no configured weather provider")
	}

	weatherNormalizer := normalization.NewWeatherNormalizer()
	geocodingNormalizer := normalization.NewGeocodingNormalizer()

	// 4. Initialize Cache Store (Distributed Redis vs Sharded Memory)
	var cacheStore cache.Store
	var redisStore *cache.RedisStore
	if cfg.CacheDriver == "redis" {
		var err error
		redisStore, err = cache.NewRedisStore(cache.RedisOptions{
			Addr:         cfg.RedisURL,
			Password:     cfg.RedisPassword,
			DB:           cfg.RedisDB,
			FailOpen:     false,
			Timeout:      3 * time.Second,
			MaxIdleConns: 10,
		})
		if err != nil {
			if cfg.Environment == "production" || cfg.Environment == "staging" {
				return fmt.Errorf("required shared Redis state unavailable: %w", err)
			}
			logger.Warn("Redis initialization failed in development; using local cache/limits", "error", err)
			cacheStore = cache.NewMemoryStore(cache.DefaultMemoryStoreOptions())
		} else {
			cacheStore = redisStore
			logger.Info("Connected to distributed Redis cache store", "addr", cfg.RedisURL)
		}
	} else {
		cacheStore = cache.NewMemoryStore(cache.DefaultMemoryStoreOptions())
		logger.Info("Using sharded in-memory LRU cache store")
	}

	defer cacheStore.Close()
	cachePolicy := cache.NewPolicyEngine()
	decisionEngine := decision.NewDecisionEngine(registry, registry.HealthEvaluator())
	decisionEngine.SetLeanRanking(profile == leanV1)
	policies, err := configuredPolicies()
	if err != nil {
		return err
	}
	deploymentUse := deploymentUse()
	decisionEngine.SetPolicyCatalog(policies, deploymentUse)
	configuredChecker := configuredProviderChecker(registry, policies, deploymentUse, required)
	if cfg.Environment == "production" || cfg.Environment == "staging" {
		if _, _, ok := configuredChecker.Check(); !ok {
			return errors.New("no policy-permitted providers configured for one or more required capabilities")
		}
	}

	if countryFile := os.Getenv("APP_COUNTRY_POLYGONS_FILE"); countryFile != "" {
		idx, loadErr := providerpolicy.LoadCountryIndex(countryFile)
		if loadErr != nil {
			return fmt.Errorf("country polygon index: %w", loadErr)
		}
		decisionEngine.SetCountryIndex(idx)
		logger.Info("Operator-supplied country polygon lookup configured", "country_count", len(idx.Countries))
	}
	logger.Info("Provider policy checks enabled", "deployment_use", deploymentUse, "custom_policy", os.Getenv("APP_PROVIDER_POLICY_FILE") != "")

	var cleanupCtx context.Context
	var cleanupCancel context.CancelFunc
	defer func() {
		if cleanupCancel != nil {
			cleanupCancel()
		}
	}()
	authenticator := auth.NewAuthenticator(store)
	lastUsed := auth.NewLastUsedWriter(store, 30*time.Second, 2048)
	defer func() {
		ctx := cleanupCtx
		if ctx == nil {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
		}
		if closeErr := lastUsed.CloseContext(ctx); closeErr != nil {
			logger.Warn("last-used write drain failed", "error", closeErr)
		}
	}()
	authMiddleware := auth.MiddlewareWithLastUsed(authenticator, lastUsed.Record)

	// The live handlers must use the bounded writer; the legacy Tracker would
	// create unbounded goroutines and would not be drained on shutdown.
	usageTracker := usage.NewBatchTracker(store, usage.DefaultBatchTrackerOptions())
	defer func() {
		ctx := cleanupCtx
		if ctx == nil {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
		}
		if err := usageTracker.CloseContext(ctx); err != nil {
			logger.Error("Usage tracking did not fully drain", "error", err)
		}
	}()

	weatherHandler := capability.NewWeatherHandler(registry, decisionEngine, weatherNormalizer, usageTracker, cacheStore, cachePolicy)
	geocodingHandler := capability.NewGeocodingHandler(registry, decisionEngine, geocodingNormalizer, usageTracker)
	// Geocoding cache is opt-in because third-party coordinate storage and
	// reuse depend on per-account provider terms.
	if allowed, allowErr := parseGeocodeCacheAllowlist(os.Getenv("APP_GEOCODE_CACHE_ALLOWED_PROVIDERS")); allowErr != nil {
		return allowErr
	} else if len(allowed) > 0 {
		geocodingHandler.ConfigureCache(cacheStore, allowed, 10*time.Minute)
	}
	forecastHandler := capability.NewForecastHandler(registry, decisionEngine, usageTracker)
	forecastHandler.ConfigureCache(cacheStore, cachePolicy)
	airQualityHandler := capability.NewAirQualityHandler(registry, decisionEngine, usageTracker)
	extendedHandler := capability.NewExtendedHandler(registry, decisionEngine, usageTracker)
	placeResolverMode := os.Getenv("APP_WEATHER_PLACE_RESOLVER")
	var placeResolver *georesolve.Resolver
	switch placeResolverMode {
	case "", "disabled":
	case "strict":
		eligibleGeo, _, selectErr := decisionEngine.SelectProviders(context.Background(), decision.RequestContext{Capability: "geocode", Product: "forward_geocoding", AllowDegraded: true})
		if selectErr != nil || len(eligibleGeo) == 0 {
			return errors.New("strict place-name resolver requires a policy-permitted forward geocoding provider")
		}
		placeResolver = georesolve.New(registry, decisionEngine)
		weatherHandler.SetPlaceResolver(placeResolver)
		forecastHandler.SetPlaceResolver(placeResolver)
		logger.Info("Strict place-name resolution enabled; eligible geocoder API calls may consume quota")
	default:
		return errors.New("APP_WEATHER_PLACE_RESOLVER must be disabled or strict")
	}

	// Optional hard provider-attempt limits. Unknown providers are NOT assigned
	// invented free quotas; require operator-reviewed values. Production
	// quotas require Redis so a second server cannot reset the allowance.
	budgets, err := quota.ParseBudgets(os.Getenv("APP_PROVIDER_QUOTAS_JSON"))
	if err != nil {
		return fmt.Errorf("provider quota configuration: %w", err)
	}
	production := cfg.Environment == "production" || cfg.Environment == "staging"
	if err := validateProviderQuotaCoverage(cfg, registry, budgets, redisStore != nil); err != nil {
		return err
	}

	if len(budgets) > 0 || production {
		quotaManager := quota.NewManager(budgets, redisStore)
		quotaManager.SetStrictUnknown(production)
		weatherHandler.SetQuotaGate(quotaManager)
		geocodingHandler.SetQuotaGate(quotaManager)
		forecastHandler.SetQuotaGate(quotaManager)
		airQualityHandler.SetQuotaGate(quotaManager)
		extendedHandler.SetQuotaGate(quotaManager)
		if placeResolver != nil {
			placeResolver.SetQuotaGate(quotaManager)
		}
		logger.Info("Enabled configured upstream request budget guards", "provider_count", len(budgets), "shared", redisStore != nil)
	}

	// 5. Initialize Production Health Checkers
	dbChecker := health.NewDatabaseChecker(store)
	cChecker := health.NewCacheChecker(cacheStore)
	healthHandler := health.NewHandler(cfg.Version, dbChecker, cChecker, configuredChecker)

	tracer := tracing.DefaultTracer
	pMetrics := metrics.DefaultMetrics

	metricsEnabled, metricsErr := metricsModeAllowed(os.Getenv("APP_METRICS_MODE"), cfg.BindHost)
	if metricsErr != nil {
		return metricsErr
	}
	var metricHandler http.Handler
	if metricsEnabled {
		token := os.Getenv("APP_METRICS_TOKEN")
		if len(token) < 32 {
			return errors.New("APP_METRICS_TOKEN must contain at least 32 characters when loopback metrics are enabled")
		}
		metricHandler = middleware.MetricsBearer(metrics.Handler(pMetrics.Registry), token)
	}
	appRouter := routing.NewRouter(routing.RouterDependencies{
		HealthHandler:     healthHandler,
		MetricsHandler:    metricHandler,
		ReleaseProfile:    profile,
		WeatherHandler:    weatherHandler,
		GeocodingHandler:  geocodingHandler,
		ForecastHandler:   forecastHandler,
		AirQualityHandler: airQualityHandler,
		ExtendedHandler:   extendedHandler,
		AuthMiddleware:    authMiddleware,
	})

	// 6. Rate Limiter (Distributed if Redis available, otherwise in-memory)
	var rateLimiter *middleware.RateLimiter
	if redisStore != nil {
		rateLimiter = middleware.NewDistributedRateLimiter(100, 200, redisStore)
	} else {
		rateLimiter = middleware.NewRateLimiter(100, 200)
	}

	trustedProxies, proxyErr := middleware.ParseTrustedProxies(os.Getenv("APP_TRUSTED_PROXY_CIDRS"))
	if proxyErr != nil {
		return proxyErr
	}
	rateLimiter.SetTrustedProxies(trustedProxies)

	// 7. Assemble defensive HTTP middleware pipeline
	var handler http.Handler = appRouter
	handler = rateLimiter.ClientAdmission(handler)
	handler = middleware.MaxBodySize(64 * 1024)(handler)
	handler = middleware.CORS(middleware.DefaultCORSConfig())(handler)
	handler = middleware.SecurityHeaders(handler)
	handler = middleware.Logger(logger)(handler)
	handler = middleware.RequestID(handler)
	handler = middleware.Telemetry(tracer, pMetrics)(handler)
	handler = middleware.Recovery(logger)(handler)

	httpServer := &http.Server{
		Addr:              cfg.ListenAddress(),
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		MaxHeaderBytes:    cfg.MaxHeaderBytes,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}

	// Bind synchronously so a port conflict is a startup failure, not an
	// unhandled goroutine exit that leaves the service manager confused.
	listener, err := net.Listen("tcp", cfg.ListenAddress())
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.ListenAddress(), err)
	}
	defer listener.Close()
	errChan := make(chan error, 1)
	go func() { errChan <- httpServer.Serve(listener) }()
	logger.Info("Gateway listening", "address", cfg.ListenAddress())
	ready()

	select {
	case serveErr := <-errChan:
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return fmt.Errorf("http server terminated unexpectedly: %w", serveErr)
		}
		if ctx.Err() == nil {
			return errors.New("http server stopped without a shutdown request")
		}
	case <-ctx.Done():
		logger.Info("Gateway shutdown requested")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	cleanupCtx = shutdownCtx
	// Registered before worker defers so cancellation follows their drain.
	cleanupCancel = cancel
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		_ = httpServer.Close()
		return fmt.Errorf("HTTP graceful shutdown exceeded deadline: %w", err)
	}
	logger.Info("Universal API Platform Gateway stopped cleanly")
	return nil

}

// initializeStore intentionally does not implement a best-effort production
// downgrade. In development memory mode is an explicit configuration choice.
func initializeStore(cfg *config.Config) (database.Store, error) {
	if cfg.DBDriver == "postgres" {
		if cfg.DatabaseURL == "" {
			return nil, fmt.Errorf("postgres storage requires APP_DATABASE_URL")
		}
		return database.OpenPostgres(database.DefaultPostgresOptions(cfg.DatabaseURL))
	}
	if cfg.Environment == "production" || cfg.Environment == "staging" {
		return nil, fmt.Errorf("production/staging requires durable authentication storage")
	}
	return database.NewMemoryStore(), nil
}

// validateDevClientKeyConfiguration is deliberately independent of backend
// availability, so --check-config catches unsafe developer credentials.
func validateDevClientKeyConfiguration() error {
	key := os.Getenv("APP_DEV_CLIENT_KEY")
	if key == "" {
		return nil
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.Environment != "development" || cfg.DBDriver != "memory" || !isLoopbackBind(cfg.BindHost) {
		return errors.New("APP_DEV_CLIENT_KEY requires development, memory storage, and a loopback bind address")
	}
	if len(key) < 24 || len(key) > 512 {
		return errors.New("APP_DEV_CLIENT_KEY must contain 24-512 characters")
	}
	return nil
}

func isLoopbackBind(host string) bool {
	ip := net.ParseIP(host)
	return strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback())
}
