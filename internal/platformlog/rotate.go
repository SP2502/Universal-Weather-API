// Package platformlog provides a bounded local log for unattended Windows
// services without an external logging agent. Linux/systemd normally uses
// stdout and the journal instead.
package platformlog

import (
	"fmt"
	"os"
	"sync"
)

type RotatingFile struct {
	mu      sync.Mutex
	path    string
	f       *os.File
	size    int64
	maxSize int64
	backups int
}

// OpenRotatingFile rotates before a write would exceed maxSize. One unusually
// long write is accepted atomically and rotated before the next one.
func OpenRotatingFile(path string, maxSize int64, backups int) (*RotatingFile, error) {
	if path == "" || maxSize < 4096 || backups < 1 || backups > 100 {
		return nil, fmt.Errorf("invalid logging configuration")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
	if err != nil {
		return nil, fmt.Errorf("open service log: %w", err)
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	return &RotatingFile{path: path, f: f, size: fi.Size(), maxSize: maxSize, backups: backups}, nil
}
func (w *RotatingFile) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return 0, os.ErrClosed
	}
	if w.size > 0 && w.size+int64(len(p)) > w.maxSize {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}
func (w *RotatingFile) rotate() error {
	if err := w.f.Close(); err != nil {
		return err
	}
	w.f = nil
	// Windows cannot rename an open log; all operations happen under the mutex.
	for i := w.backups - 1; i >= 1; i-- {
		old := fmt.Sprintf("%s.%d", w.path, i)
		next := fmt.Sprintf("%s.%d", w.path, i+1)
		if err := os.Rename(old, next); err != nil && !os.IsNotExist(err) {
			return w.reopen(err)
		}
	}
	old := w.path + ".1"
	// Windows rename cannot replace an existing destination.
	if err := os.Remove(old); err != nil && !os.IsNotExist(err) {
		return w.reopen(err)
	}
	if err := os.Rename(w.path, old); err != nil && !os.IsNotExist(err) {
		return w.reopen(err)
	}
	return w.reopen(nil)
}
func (w *RotatingFile) reopen(previous error) error {
	f, err := os.OpenFile(w.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
	if err != nil {
		return fmt.Errorf("reopen service log: %w (prior error: %v)", err, previous)
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w.f = f
	w.size = fi.Size()
	return previous
}
func (w *RotatingFile) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}
