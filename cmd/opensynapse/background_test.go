package main

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

// A panicking round must not end the process: guard reports it, logs it under
// the job's name, and the caller's loop goes on.
func TestGuardContainsPanic(t *testing.T) {
	var out bytes.Buffer
	old := log.Writer()
	log.SetOutput(&out)
	t.Cleanup(func() { log.SetOutput(old) })
	rounds := 0
	for range 2 {
		if guard("test job", func() { rounds++; var keys [2]int; _ = keys[rounds+1] }) {
			t.Fatal("guard reported a panicking round as done")
		}
	}
	if rounds != 2 {
		t.Fatalf("%d rounds ran, want 2", rounds)
	}
	if got := out.String(); !strings.Contains(got, "test job: panic:") || !strings.Contains(got, "index out of range") {
		t.Fatalf("log %q; want the job's name and the panic", got)
	}
	if !guard("test job", func() {}) {
		t.Fatal("guard reported a round that returned as failed")
	}
}

// A contained panic must not leave the settings lock held: every other job
// and every call from the page would wait on it for good.
func TestRememberKeyboardReleasesTheLockOnPanic(t *testing.T) {
	old := log.Writer()
	log.SetOutput(&bytes.Buffer{})
	cfgMu.Lock()
	saved := cfg.Keyboards
	cfgMu.Unlock()
	t.Cleanup(func() {
		log.SetOutput(old)
		cfgMu.Lock()
		cfg.Keyboards = saved
		cfgMu.Unlock()
	})
	guard("test job", func() {
		_ = rememberKeyboard("k", func(*KeyboardMemory) { panic("in change") })
	})
	held := !cfgMu.TryLock()
	cfgMu.Unlock() // also when it was left held: the other tests need it
	if held {
		t.Fatal("cfgMu is still held after the panic")
	}
}
