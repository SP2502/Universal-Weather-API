package platformenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFileAndEnvironmentPrecedence(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "gateway.env")
	os.WriteFile(p, []byte("# comment\nAPP_TEST_ENV_FILE=from-file\nAPP_TEST_ENV_PRECEDENCE=from-file\nAPP_TEST_ENV_QUOTES=\"a b\"\n"), 0600)
	t.Setenv("APP_TEST_ENV_PRECEDENCE", "from-env")
	if err := LoadFile(p); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("APP_TEST_ENV_FILE"); got != "from-file" {
		t.Fatalf("got %q", got)
	}
	if got := os.Getenv("APP_TEST_ENV_PRECEDENCE"); got != "from-env" {
		t.Fatalf("precedence: %q", got)
	}
	if got := os.Getenv("APP_TEST_ENV_QUOTES"); got != "a b" {
		t.Fatalf("quotes: %q", got)
	}
	os.Unsetenv("APP_TEST_ENV_FILE")
	os.Unsetenv("APP_TEST_ENV_QUOTES")
}
func TestLoadFileRejectsErrorsAtomically(t *testing.T) {
	for _, body := range []string{"APP_TEST_ATOMIC=hello\nwrong key=bad\n", "APP_TEST_ATOMIC=hello\nAPP_TEST_ATOMIC=duplicate\n", "APP_TEST_ATOMIC=\"bad\\q\"\n"} {
		p := filepath.Join(t.TempDir(), "bad.env")
		if err := os.WriteFile(p, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if err := LoadFile(p); err == nil {
			t.Fatalf("expected failure: %q", body)
		}
		if v := os.Getenv("APP_TEST_ATOMIC"); strings.TrimSpace(v) != "" {
			t.Fatalf("file partially applied: %s", v)
		}
	}
}
