package main

import "testing"

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
