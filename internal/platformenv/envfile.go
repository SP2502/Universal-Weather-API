// Package platformenv loads optional per-instance environment files for
// unattended Windows services and local console operation. Existing process
// environment takes precedence; file contents are never logged.
package platformenv

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

// LoadFile accepts KEY=value and double-quoted values. It deliberately does
// not expand shell expressions, invoke commands, or silently ignore bad lines.
func LoadFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("read configuration file: %w", err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	seen := make(map[string]bool)
	lineNo := 0
	// Parse completely before changing the environment, so a broken file never
	// partially overrides configuration.
	parsed := make(map[string]string)
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if lineNo == 1 {
			line = strings.TrimPrefix(line, "\ufeff")
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || !validKey(key) {
			return fmt.Errorf("invalid configuration key on line %d", lineNo)
		}
		if seen[key] {
			return fmt.Errorf("duplicate configuration key %q on line %d", key, lineNo)
		}
		seen[key] = true
		val = strings.TrimSpace(val)
		if strings.HasPrefix(val, "\"") {
			decoded, err := strconv.Unquote(val)
			if err != nil {
				return fmt.Errorf("invalid quoted value on line %d", lineNo)
			}
			val = decoded
		} else if strings.HasPrefix(val, "'") && strings.HasSuffix(val, "'") && len(val) >= 2 {
			val = val[1 : len(val)-1]
		}
		if !utf8.ValidString(val) || strings.ContainsRune(val, '\x00') {
			return fmt.Errorf("invalid value on line %d", lineNo)
		}
		parsed[key] = val
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read configuration file: %w", err)
	}
	for key, val := range parsed {
		if _, ok := os.LookupEnv(key); !ok {
			if err := os.Setenv(key, val); err != nil {
				return fmt.Errorf("apply configuration key %q: %w", key, err)
			}
		}
	}
	return nil
}

func validKey(key string) bool {
	if key == "" {
		return false
	}
	for i, c := range key {
		if c == '_' || (c >= 'A' && c <= 'Z') || (i > 0 && c >= '0' && c <= '9') {
			continue
		}
		return false
	}
	return true
}
