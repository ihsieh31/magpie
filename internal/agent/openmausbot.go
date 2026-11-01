package agent

// OpenMausBot (openmausbot.com) is a desktop chat app of bots, each one an
// engine (Claude, Codex, OpenCode, an OpenAI-compatible endpoint…) with a
// model of its own. It keeps its settings in ~/.openmausbot/config.json
// (mode 0600), its engines under "instances", each {driver, displayName,
// enabled, environment, config}; the built-in "openai-compat" driver takes
// any number of instances, each an OpenAI-compatible endpoint
// (docs/custom-engines.md, server/drivers/openai-compat.ts):
//
//	"instances": {
//	  "magpie": {
//	    "driver": "openai-compat", "displayName": "Magpie", "enabled": true,
//	    "config": {"url": "http://127.0.0.1:3425/v1",
//	               "apiKeyEnv": "MAGPIE_API_KEY", "key": "magpie-openmausbot",
//	               "model": "<provider>/<model>"}
//	  }
//	}
//
// magpie adds that one instance, "magpie": the driver lists the gateway's
// /v1/models in a bot's model picker and starts a bot on config.model,
// which is the field magpie sets. config.key is read before any variable;
// apiKeyEnv names a variable of magpie's own, so a key the user saved for
// OpenMausBot's own OpenAI-compatible engine is never sent to magpie.
// OpenMausBot's User-Agent is Node's own, so its requests are told apart
// by their key.
//
// With no "instances", or none in it, OpenMausBot runs its built-in fleet
// (instanceConfigs in server/config.ts); with any, it runs those alone,
// plus the rest of the fleet only when one of claude, grok or codex is
// there (PRODUCT_FLEET_ADDITIONS). So magpie's instance never goes into an
// empty map alone, which would leave every bot on Claude or Codex without
// its engine: the fleet's other engines go in with it, as OpenMausBot's own
// setup does (server/cli-setup.ts), and come out again with it while they
// are as magpie wrote them.
//
// OpenMausBot reads its instances at start-up and, while it runs, writes
// the whole map back from what it read whenever an engine is changed in
// the app (persistProviderInstance, saveConfig's replaceInstances): a
// change of magpie's made meanwhile would be lost, or an instance magpie
// took away put back, key and all. So magpie changes nothing while it runs.
// What the user set on magpie's instance in the app (its icon, a reply
// wait) stays through a change; a "magpie" instance the user wrote by hand
// is replaced by magpie's on Connect and goes on Disconnect, as Empryo's
// provider does.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/yetone/magpie/internal/edit"
)

// openMausBotApp is OpenMausBot's macOS bundle.
var openMausBotApp = desktopApp{id: "com.openmausbot.app", names: []string{"OpenMausBot.app"}}

// openMausBotProc is OpenMausBot's process: the Mac app's executable, and
// the Linux build's and the headless CLI's `openmausbot` (`openmausbot
// serve`, through node too), not a folder of that name in a command line.
var openMausBotProc = []string{`OpenMausBot\.app/Contents/MacOS/OpenMausBot( |$)`, `(^|/)openmausbot( |$)`}

// openMausBotRunning says OpenMausBot is open where magpie can tell; on
// Windows, where Running can't, no, and the notice asks to quit it first.
var openMausBotRunning = func() bool {
	return runtime.GOOS != "windows" && Running(openMausBotProc...)
}

// errOpenMausBotRunning is a change refused while OpenMausBot runs.
var errOpenMausBotRunning = errors.New("OpenMausBot writes its engines back over what magpie writes now: quit OpenMausBot, then try again")

// openMausBotFleet is the part of OpenMausBot's built-in fleet a saved map
// doesn't get back on its own (DEFAULT_FLEET less PRODUCT_FLEET_ADDITIONS,
// server/instance-fleets.ts); with claude among them, OpenMausBot adds the
// rest as it loads.
var openMausBotFleet = map[string]any{
	"grok":        map[string]any{"driver": "grokAgent"},
	"kimi":        map[string]any{"driver": "kimiAgent"},
	"droid":       map[string]any{"driver": "droidAgent"},
	"claude":      map[string]any{"driver": "claudeAgent"},
	"codex":       map[string]any{"driver": "codex"},
	"antigravity": map[string]any{"driver": "antigravityAgent"},
	"opencodeGo":  map[string]any{"driver": "opencodeGo"},
}

const (
	openMausBotInstances = "instances"
	// openMausBotInstance is the key of magpie's engine in config.json.
	openMausBotInstance = openMausBotInstances + "." + magpieID
)

func openMausBot(home string) *Agent {
	dir := filepath.Join(home, ".openmausbot")
	path := filepath.Join(dir, "config.json")
	// keyFleet marks a fleet magpie wrote in with its instance, keyNew a
	// file magpie made: each goes again with magpie's instance
	keyFleet := "openmausbot:" + path + ":fleet"
	keyNew := "openmausbot:" + path + ":new"
	get := func(k string) string { v, _ := edit.GetJSON(path, openMausBotInstance+"."+k); return v }
	// on: magpie's engine is there, the OpenAI-compatible driver, not
	// turned off in the app
	on := func() bool { return get("driver") == "openai-compat" && get("enabled") != "false" }
	// instances is the map of engines in the file, nil for none
	instances := func() map[string]json.RawMessage {
		raw, err := edit.Read(path)
		if err != nil {
			return nil
		}
		var m map[string]json.RawMessage
		if r := gjson.GetBytes(raw, openMausBotInstances); r.IsObject() && json.Unmarshal([]byte(r.Raw), &m) == nil {
			return m
		}
		return nil
	}
	put := func(ref string) error {
		want := map[string]any{
			"driver": "openai-compat", "displayName": "Magpie", "enabled": true,
			"config": map[string]any{
				"url": gatewayV1(), "apiKeyEnv": "MAGPIE_API_KEY", "key": agentKey("openmausbot"), "model": ref,
			},
		}
		if !isFile(path) {
			forget(keyNew)
			stash(map[string]string{keyNew: "1"})
		}
		kvs := []edit.KV{{Path: openMausBotInstance, Value: theirsKept(path, openMausBotInstance, func() any { return want })()}}
		if len(instances()) == 0 {
			// the built-in fleet goes in beside magpie's engine
			forget(keyFleet)
			stash(map[string]string{keyFleet: "1"})
			fleet := map[string]any{magpieID: kvs[0].Value}
			for id, e := range openMausBotFleet {
				fleet[id] = e
			}
			kvs = []edit.KV{{Path: openMausBotInstances, Value: fleet}}
		}
		if err := edit.SetJSON(path, kvs...); err != nil {
			return err
		}
		// the file holds a key now: OpenMausBot keeps it private
		if st, err := os.Stat(path); err == nil && st.Mode().Perm()&0o077 != 0 {
			return os.Chmod(path, 0o600)
		}
		return nil
	}
	// out takes magpie's engine away, and the fleet and file magpie wrote
	// with it while they are as magpie wrote them
	out := func() error {
		fleet, made := unstash(keyFleet) != "", unstash(keyNew) != ""
		if !isFile(path) {
			return nil
		}
		if _, ok := instances()[magpieID]; ok {
			if err := edit.DelJSON(path, openMausBotInstance); err != nil {
				return err
			}
		}
		if left := instances(); left != nil && (len(left) == 0 || fleet && openMausBotFleetOnly(left)) {
			if err := edit.DelJSON(path, openMausBotInstances); err != nil {
				return err
			}
		}
		if made {
			if raw, err := edit.Read(path); err == nil && len(gjson.ParseBytes(raw).Map()) == 0 {
				return edit.Remove(path)
			}
		}
		return nil
	}
	return atomic(&Agent{
		ID: "openmausbot", Name: "OpenMausBot", Icon: "openmausbot", Aliases: []string{"mausbot"}, Spelled: prefixed, NoNames: true,
		Bin: "openmausbot", Dir: dir, Path: path,
		// the app on a Mac, its data folder anywhere (the server and the
		// Linux and Windows builds make it as they start)
		detect: func() bool { return macBundle(openMausBotApp) || isDir(dir) },
		Notice: func() string {
			if runtime.GOOS == "windows" || Running(openMausBotProc...) {
				return noticeOpenMausBot.String()
			}
			return ""
		},
		Check: func() string {
			if !on() {
				return ""
			}
			return wiringOff("OpenMausBot", path, func(k string) (string, bool) {
				return edit.GetJSON(path, openMausBotInstance+".config."+k)
			}, "url", gatewayV1())
		},
		Fields: []Field{{
			Key: "model", Label: "model",
			Get: func() string {
				if m := get("config.model"); on() && m != "" {
					return magpieID + "/" + m
				}
				return ""
			},
			Set: func(v string) error {
				ref, ok := strings.CutPrefix(v, magpieID+"/")
				ok = ok && isMagpie(ref)
				if _, has := instances()[magpieID]; !ok && !has {
					return nil // nothing of magpie's to take away
				}
				if openMausBotRunning() {
					return errOpenMausBotRunning
				}
				if ok {
					return put(ref)
				}
				// OpenMausBot's own engines have no one model to go back
				// to: each bot has its own, so magpie's engine just goes
				return out()
			},
			Options: func(map[string]string) []Option { return withVia(nil, "openmausbot", magpieID+"/") },
		}},
	}, path)
}

// openMausBotFleetOnly says the engines left are the fleet as magpie wrote
// it, none changed or added in the app.
func openMausBotFleetOnly(left map[string]json.RawMessage) bool {
	if len(left) != len(openMausBotFleet) {
		return false
	}
	for id, e := range openMausBotFleet {
		var have any
		if json.Unmarshal(left[id], &have) != nil {
			return false
		}
		b, _ := json.Marshal(e)
		var want any
		json.Unmarshal(b, &want)
		if !reflect.DeepEqual(have, want) {
			return false
		}
	}
	return true
}

// what OpenMausBot says after a change (notice.go)
var (
	noticeOpenMausBot = newNotice("OpenMausBot keeps its engines in memory while it runs and writes them all back when one changes — quit OpenMausBot before changing this, then open it and pick Magpie in a bot's model picker.")
)
