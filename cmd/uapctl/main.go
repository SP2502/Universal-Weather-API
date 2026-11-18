// uapctl is an offline administrator command. It is intentionally not an HTTP
// endpoint: possession of an API key must not grant key-creation privileges.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/universal-api/platform/internal/auth"
	"github.com/universal-api/platform/internal/database"
	"github.com/universal-api/platform/internal/providerpolicy"
)

const usage = "Usage: uapctl create-key --name NAME [--scopes weather:read,geocode:read,forecast:read,air-quality:read,history:read,astronomy:read,marine:read,alerts:read] [--expires-in 2160h] OR uapctl doctor [--env-file PATH] [--json] OR uapctl validate-policy --file PATH"

var allowedScopes = map[string]bool{
	"weather:read": true, "geocode:read": true, "forecast:read": true,
	"air-quality:read": true, "history:read": true, "astronomy:read": true,
	"marine:read": true, "alerts:read": true,
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "uapctl:", err)
		os.Exit(1)
	}
}

func parseKeyArgs(args []string) (string, []string, time.Duration, error) {
	if len(args) < 1 || args[0] != "create-key" {
		return "", nil, 0, fmt.Errorf("invalid command: %s", usage)
	}
	fs := flag.NewFlagSet("create-key", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	name := fs.String("name", "", "Administrative label")
	rawScopes := fs.String("scopes", "weather:read,geocode:read,forecast:read", "Allowed scopes")
	expiresIn := fs.Duration("expires-in", 90*24*time.Hour, "API-key validity")
	if err := fs.Parse(args[1:]); err != nil {
		return "", nil, 0, fmt.Errorf("invalid arguments: %w; %s", err, usage)
	}
	if len(fs.Args()) != 0 {
		return "", nil, 0, fmt.Errorf("unexpected positional arguments; %s", usage)
	}
	cleanedName := strings.TrimSpace(*name)
	if len(cleanedName) < 1 || len(cleanedName) > 255 {
		return "", nil, 0, errors.New("--name must contain 1–255 characters")
	}
	if *expiresIn < time.Hour || *expiresIn > 365*24*time.Hour {
		return "", nil, 0, errors.New("--expires-in must be between 1h and 8760h")
	}
	scopes := make([]string, 0, 2)
	seen := map[string]bool{}
	for _, v := range strings.Split(*rawScopes, ",") {
		scope := strings.TrimSpace(v)
		if !allowedScopes[scope] {
			return "", nil, 0, fmt.Errorf("invalid scope %q (not a supported read scope)", scope)
		}
		if seen[scope] {
			return "", nil, 0, fmt.Errorf("duplicate scope %q", scope)
		}
		seen[scope] = true
		scopes = append(scopes, scope)
	}
	if len(scopes) == 0 {
		return "", nil, 0, errors.New("at least one scope is required")
	}
	return cleanedName, scopes, *expiresIn, nil
}

func run(args []string, output io.Writer) error {
	if len(args) > 0 && args[0] == "doctor" {
		return runDoctor(args[1:], output)
	}
	if len(args) > 0 && args[0] == "validate-policy" {
		fs := flag.NewFlagSet("validate-policy", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		path := fs.String("file", "", "Provider policy JSON document")
		countryPath := fs.String("country-polygons", "", "Optional operator-sourced country polygon index")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *path == "" || len(fs.Args()) > 0 {
			return errors.New("validate-policy requires --file PATH")
		}
		catalog, err := providerpolicy.LoadFile(*path)
		if err != nil {
			return err
		}
		if *countryPath != "" {
			if _, err := providerpolicy.LoadCountryIndex(*countryPath); err != nil {
				return err
			}
		}
		_, err = fmt.Fprintf(output, "Valid provider policy (openweather eligible for current weather under commercial use: %t); no live providers contacted\n", catalog.PotentiallyEligible("openweather", providerpolicy.ProductCurrentWeather, "commercial"))
		return err
	}
	name, scopes, expiresIn, err := parseKeyArgs(args)
	if err != nil {
		return err
	}
	dsn := os.Getenv("APP_DATABASE_URL")
	if dsn == "" {
		return errors.New("aPP_DATABASE_URL is required; keys must be provisioned into PostgreSQL")
	}
	store, err := database.OpenPostgres(database.DefaultPostgresOptions(dsn))
	if err != nil {
		// Do not print errors that could contain connection strings or passwords.
		return errors.New("cannot open PostgreSQL store; verify connectivity, credentials and migrations")
	}
	defer store.Close()

	pair, err := auth.GenerateKey("uap_live_")
	if err != nil {
		return err
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return fmt.Errorf("generate key identifier: %w", err)
	}
	now := time.Now().UTC()
	expiry := now.Add(expiresIn)
	keyID := "key-" + hex.EncodeToString(idBytes)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := store.CreateAPIKey(ctx, &database.APIKey{
		ID: keyID, Name: name, KeyPrefix: pair.KeyPrefix, KeyHash: pair.KeyHash,
		Scopes: scopes, IsActive: true, CreatedAt: now, ExpiresAt: &expiry,
	}); err != nil {
		return errors.New("could not provision API key (database insert failed)")
	}

	// Only this single line on stdout contains the secret; stderr contains the
	// identifying ID so an accidentally lost output can be revoked by an admin.
	fmt.Fprintf(os.Stderr, "Created key %s; expires %s. Store the secret securely; it cannot be retrieved again.\n", keyID, expiry.Format(time.RFC3339))
	if _, err := fmt.Fprintln(output, pair.PlaintextKey); err != nil {
		return fmt.Errorf("secret output failed; revoke key ID %s before retrying: %w", keyID, err)
	}
	return nil
}
