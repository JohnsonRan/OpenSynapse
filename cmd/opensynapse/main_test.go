package main

import (
	"os"
	"testing"
)

// TestMain keeps every test off the real settings: anything that saves the
// config without its own temporary APPDATA would replace the user's.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "opensynapse-test-")
	if err != nil {
		panic(err)
	}
	os.Setenv("APPDATA", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
