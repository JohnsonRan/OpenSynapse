package main

import (
	"encoding/json"
	"testing"
)

func TestImportConfig(t *testing.T) {
	if _, err := importConfig([]byte(`{"foo":1}`)); err == nil {
		t.Fatal("accepted a file that is not a settings file")
	}
	src := defaultConfig()
	src.Language = "ja"
	src.Macros = []Macro{{ID: "a", Name: "ok", Mode: "once"}, {ID: "b", Name: "", Mode: "once"}}
	src.Bindings = []Binding{{Family: "F", Button: 5, Macro: "a", Trigger: firstTrigger}, {Family: "F", Button: 4, Macro: "b", Trigger: firstTrigger + 1}}
	b, _ := json.Marshal(src)
	c, err := importConfig(b)
	if err != nil || c.Language != "ja" || len(c.Macros) != 1 || len(c.Bindings) != 1 || c.Bindings[0].Macro != "a" {
		t.Fatalf("%v %+v", err, c)
	}
}
