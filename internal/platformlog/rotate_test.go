package platformlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotatingFileBoundsAndClose(t *testing.T) {
	p := filepath.Join(t.TempDir(), "gateway.log")
	w, err := OpenRotatingFile(p, 4096, 2)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 25; i++ {
		if _, err := w.Write([]byte(strings.Repeat("a", 300) + "\n")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(p + ".1"); err != nil {
		t.Fatalf("expected rotated log: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("x")); !os.IsNotExist(err) && err != os.ErrClosed {
		t.Fatalf("expected closed error, got %v", err)
	}
}
