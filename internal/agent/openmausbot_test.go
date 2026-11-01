package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/gateway"
)

// OpenMausBot may be open on the machine the tests run on: each test says
// whether it runs.
func init() { openMausBotRunning = func() bool { return false } }

func TestOpenMausBot(t *testing.T) {
	home, _ := museHome(t)
	path := filepath.Join(home, ".openmausbot", "config.json")
	os.MkdirAll(filepath.Dir(path), 0o700)
	// a user's own: engines of theirs, the workspace OpenAI-compatible key
	// and a profile
	orig := `{
  "profile": {"name": "me"},
  "openaiCompat": {"key": "sk-users-own", "url": "https://openrouter.ai/api/v1"},
  "instances": {
    "claude": {"driver": "claudeAgent"},
    "openrouter": {"driver": "openai-compat", "displayName": "OpenRouter", "access": "api"}
  }
}
`
	os.WriteFile(path, []byte(orig), 0o644)
	read := func() map[string]any {
		t.Helper()
		var m map[string]any
		b, _ := os.ReadFile(path)
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("%v\n%s", err, b)
		}
		return m
	}
	var want map[string]any
	json.Unmarshal([]byte(orig), &want)

	a := openMausBot(home)
	if a.Path != path {
		t.Fatalf("path: %s", a.Path)
	}
	f := a.Field("model")
	if f.Get() != "" || a.Check() != "" {
		t.Fatalf("own: %q %q", f.Get(), a.Check())
	}
	var vals []string
	for _, o := range f.Options(a.Values()) {
		vals = append(vals, o.Value)
	}
	if !strings.Contains(strings.Join(vals, " "), "magpie/deepseek/pro") {
		t.Fatalf("options: %v", vals)
	}

	for range 2 { // twice: one magpie engine
		if err := f.Set("magpie/deepseek/pro"); err != nil {
			t.Fatal(err)
		}
	}
	m := read()
	ins := m["instances"].(map[string]any)
	wantMine := map[string]any{
		"driver": "openai-compat", "displayName": "Magpie", "enabled": true,
		"config": map[string]any{
			"url": gatewayV1(), "apiKeyEnv": "MAGPIE_API_KEY", "key": gateway.TokenFor("openmausbot"), "model": "deepseek/pro",
		},
	}
	if len(ins) != 3 || !reflect.DeepEqual(ins["magpie"], wantMine) ||
		!reflect.DeepEqual(ins["openrouter"], want["instances"].(map[string]any)["openrouter"]) ||
		!reflect.DeepEqual(m["openaiCompat"], want["openaiCompat"]) {
		t.Fatalf("set: %v", m)
	}
	if f.Get() != "magpie/deepseek/pro" || a.Check() != "" {
		t.Fatalf("on: %q %q", f.Get(), a.Check())
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode: %v", st.Mode())
	}

	// an icon the user gave magpie's engine in the app stays through a switch
	b, _ := os.ReadFile(path)
	os.WriteFile(path, []byte(strings.Replace(string(b), `"displayName"`, `"icon": {"kind": "preset", "preset": "openai"}, "displayName"`, 1)), 0o600)
	if err := f.Set("magpie/deepseek/flash"); err != nil {
		t.Fatal(err)
	}
	mine := read()["instances"].(map[string]any)["magpie"].(map[string]any)
	if mine["icon"] == nil || mine["config"].(map[string]any)["model"] != "deepseek/flash" {
		t.Fatalf("switch: %v", mine)
	}

	// magpie's engine pointed elsewhere
	b, _ = os.ReadFile(path)
	os.WriteFile(path, []byte(strings.Replace(string(b), gatewayV1(), "http://elsewhere/v1", 1)), 0o600)
	if !strings.Contains(a.Check(), "http://elsewhere/v1") {
		t.Fatalf("check: %q", a.Check())
	}
	os.WriteFile(path, b, 0o600)

	// stepping out puts back what the user had
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	if m := read(); !reflect.DeepEqual(m, want) {
		t.Fatalf("restore: %v", m)
	}
	if f.Get() != "" {
		t.Fatalf("off: %q", f.Get())
	}

	// a file magpie makes is private, as OpenMausBot keeps it
	os.Remove(path)
	if err := f.Set("magpie/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("new mode: %v", st.Mode())
	}
}

// With no engines saved, OpenMausBot runs its built-in fleet: magpie's
// engine goes in with that fleet, not alone (which would leave bots on
// Claude or Codex with no engine), and both come out on Disconnect.
func TestOpenMausBotFleet(t *testing.T) {
	home, _ := museHome(t)
	path := filepath.Join(home, ".openmausbot", "config.json")
	os.MkdirAll(filepath.Dir(path), 0o700)
	for _, orig := range []string{`{"profile": {"name": "me"}}`, `{"profile": {"name": "me"}, "instances": {}}`} {
		os.WriteFile(path, []byte(orig), 0o644)
		f := openMausBot(home).Field("model")
		if err := f.Set("magpie/deepseek/pro"); err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		b, _ := os.ReadFile(path)
		json.Unmarshal(b, &m)
		ins, _ := m["instances"].(map[string]any)
		for _, id := range []string{"magpie", "claude", "codex", "grok", "antigravity"} {
			if ins[id] == nil {
				t.Fatalf("%s: no %s: %s", orig, id, b)
			}
		}
		// a key in the file: private, as OpenMausBot keeps it
		if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
			t.Fatalf("mode: %v", st.Mode())
		}
		if err := f.Set(""); err != nil {
			t.Fatal(err)
		}
		b, _ = os.ReadFile(path)
		m = nil
		json.Unmarshal(b, &m)
		if _, ok := m["instances"]; ok || m["profile"] == nil {
			t.Fatalf("%s: restore: %s", orig, b)
		}
	}

	// an engine the user changed in the app since stays, with the fleet
	os.WriteFile(path, []byte(`{}`), 0o600)
	f := openMausBot(home).Field("model")
	if err := f.Set("magpie/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	if err := edit.SetJSON(path, edit.KV{Path: "instances.claude.displayName", Value: "Mine"}); err != nil {
		t.Fatal(err)
	}
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	b, _ := os.ReadFile(path)
	json.Unmarshal(b, &m)
	ins, _ := m["instances"].(map[string]any)
	if ins == nil || ins["magpie"] != nil || ins["claude"] == nil {
		t.Fatalf("changed fleet: %s", b)
	}
}

// OpenMausBot writes its engines back from memory while it runs, so magpie
// changes nothing then; an engine turned off in the app isn't connected.
func TestOpenMausBotRunningAndOff(t *testing.T) {
	home, _ := museHome(t)
	path := filepath.Join(home, ".openmausbot", "config.json")
	os.MkdirAll(filepath.Dir(path), 0o700)
	orig := `{"instances": {"claude": {"driver": "claudeAgent"}}}`
	os.WriteFile(path, []byte(orig), 0o600)
	a := openMausBot(home)
	f := a.Field("model")

	openMausBotRunning = func() bool { return true }
	t.Cleanup(func() { openMausBotRunning = func() bool { return false } })
	if err := f.Set("magpie/deepseek/pro"); err != errOpenMausBotRunning {
		t.Fatalf("running: %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != orig {
		t.Fatalf("written while running: %s", b)
	}
	// nothing of magpie's there: stepping out has nothing to refuse
	if err := f.Set(""); err != nil {
		t.Fatalf("out, nothing there: %v", err)
	}

	openMausBotRunning = func() bool { return false }
	if err := f.Set("magpie/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	openMausBotRunning = func() bool { return true }
	if err := f.Set(""); err != errOpenMausBotRunning {
		t.Fatalf("out while running: %v", err)
	}
	openMausBotRunning = func() bool { return false }

	// turned off in the app: not connected, and a connect turns it on
	if err := edit.SetJSON(path, edit.KV{Path: "instances.magpie.enabled", Value: false}); err != nil {
		t.Fatal(err)
	}
	if f.Get() != "" || a.Check() != "" {
		t.Fatalf("off: %q %q", f.Get(), a.Check())
	}
	if err := f.Set("magpie/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	if f.Get() != "magpie/deepseek/pro" {
		t.Fatalf("on again: %q", f.Get())
	}
}

func TestOpenMausBotProcess(t *testing.T) {
	for cmd, want := range map[string]bool{
		"/Applications/OpenMausBot.app/Contents/MacOS/OpenMausBot":                   true,
		"/opt/OpenMausBot/openmausbot --no-sandbox":                                  true,
		"node /usr/local/bin/openmausbot serve":                                      true,
		"vim /Users/me/src/OpenMausBot/server/config.ts":                             false,
		"node /Users/me/src/OpenMausBot/node_modules/.bin/vite":                      false,
		"/Applications/OpenMausBot.app/Contents/Frameworks/OpenMausBot Helper.app/x": false,
	} {
		got := false
		for _, p := range openMausBotProc {
			if regexp.MustCompile(p).MatchString(cmd) {
				got = true
			}
		}
		if got != want {
			t.Errorf("%q: %v", cmd, got)
		}
	}
}
