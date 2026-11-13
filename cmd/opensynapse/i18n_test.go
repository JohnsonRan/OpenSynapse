package main

import (
	"testing"
	"time"
)

// Every language must have every key; tr would show an empty string.
func TestMessageKeys(t *testing.T) {
	for lang, m := range messages {
		for k := range messages["zh-CN"] {
			if m[k] == "" {
				t.Errorf("%s: missing %s", lang, k)
			}
		}
		if len(m) != len(messages["zh-CN"]) {
			t.Errorf("%s: %d keys, zh-CN has %d", lang, len(m), len(messages["zh-CN"]))
		}
	}
}

// The Windows display language is asked for once per systemLanguageAge, and
// a change shows after that.
func TestSystemLanguageIsCachedForAWhile(t *testing.T) {
	cfgMu.Lock()
	saved := cfg.Language
	cfg.Language = ""
	cfgMu.Unlock()
	origRead := preferredLanguages
	reads, langs := 0, []string{"fr-FR", "ja-JP", "en-US"}
	preferredLanguages = func() []string { reads++; return langs }
	forget := func() {
		systemLang.Lock()
		systemLang.at = time.Time{}
		systemLang.Unlock()
	}
	forget()
	t.Cleanup(func() {
		preferredLanguages = origRead
		forget()
		cfgMu.Lock()
		cfg.Language = saved
		cfgMu.Unlock()
	})

	for range 3 {
		if got := language(); got != "ja" {
			t.Fatalf("language %q, want the first display language the app has", got)
		}
	}
	if reads != 1 {
		t.Fatalf("asked Windows %d times, want once", reads)
	}
	langs = []string{"zh-TW"}
	if got := language(); got != "ja" {
		t.Fatalf("language %q before the cached one is old", got)
	}
	systemLang.Lock()
	systemLang.at = time.Now().Add(-systemLanguageAge - time.Second)
	systemLang.Unlock()
	if got := language(); got != "zh-CN" || reads != 2 {
		t.Fatalf("language %q after %d reads, want the changed one after a second read", got, reads)
	}
}

// tr with the language following the system, as the scene switcher calls it
// for the tray status every second.
func BenchmarkTrSystemLanguage(b *testing.B) {
	cfgMu.Lock()
	saved := cfg.Language
	cfg.Language = ""
	cfgMu.Unlock()
	b.Cleanup(func() {
		cfgMu.Lock()
		cfg.Language = saved
		cfgMu.Unlock()
	})
	b.ReportAllocs()
	for b.Loop() {
		tr("scene.game")
	}
}
