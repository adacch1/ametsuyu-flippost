package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Redirect usage persistence to a temp file for all tests.
func TestMain(m *testing.M) {
	d, _ := os.MkdirTemp("", "zf5usage")
	usagePath = filepath.Join(d, "usage.json")
	code := m.Run()
	os.RemoveAll(d)
	os.Exit(code)
}
