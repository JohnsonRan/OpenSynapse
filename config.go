package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Rule raises the polling rate while an app is in the foreground.
type Rule struct {
	Exe  string `json:"exe"` // file name, e.g. cs2.exe (case-insensitive)
	Rate int    `json:"rate"`
}

// SmartConfig is the smart polling-rate switching setup.
type SmartConfig struct {
	Enabled     bool   `json:"enabled"`
	DefaultRate int    `json:"defaultRate"` // used when no rule matches
	Rules       []Rule `json:"rules"`
}

type config struct {
	Smart SmartConfig `json:"smart"`
}

var (
	cfgMu sync.Mutex
	cfg   = config{Smart: SmartConfig{DefaultRate: 1000, Rules: []Rule{{Exe: "cs2.exe", Rate: 8000}}}}
)

func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "OpenSynapse", "config.json")
}

func loadConfig() {
	b, err := os.ReadFile(configPath())
	if err != nil {
		return // first run: defaults
	}
	cfgMu.Lock()
	defer cfgMu.Unlock()
	_ = json.Unmarshal(b, &cfg)
}

func saveConfig() error {
	cfgMu.Lock()
	b, _ := json.MarshalIndent(cfg, "", "  ")
	cfgMu.Unlock()
	p := configPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p) // atomic: a crash never leaves a half-written config
}

func validRate(hz int) bool {
	switch hz {
	case 125, 500, 1000, 2000, 4000, 8000:
		return true
	}
	return false
}

func (s *SmartConfig) normalize() error {
	if !validRate(s.DefaultRate) {
		return errors.New("invalid default rate")
	}
	out := s.Rules[:0]
	for _, r := range s.Rules {
		r.Exe = strings.ToLower(strings.TrimSpace(r.Exe))
		if r.Exe == "" {
			continue
		}
		if strings.ContainsAny(r.Exe, `\/`) {
			r.Exe = filepath.Base(r.Exe)
		}
		if !validRate(r.Rate) {
			return errors.New("invalid rate for " + r.Exe)
		}
		out = append(out, r)
	}
	s.Rules = out
	return nil
}
