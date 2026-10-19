// Configuration and installation-state acceptance using isolated real files.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"voiceinput/internal/config"
	"voiceinput/internal/localasr"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func main() {
	if len(os.Args) != 2 {
		panic("usage: config-state-acceptance NEW_OUTPUT_DIRECTORY")
	}
	root := os.Args[1]
	must(os.Mkdir(root, 0700))
	rows := []map[string]any{}
	record := func(name string, pass bool) { rows = append(rows, map[string]any{"case": name, "passed": pass}) }
	settingsDir := filepath.Join(root, "settings")
	must(os.Mkdir(settingsDir, 0700))
	st, err := config.Open(settingsDir)
	must(err)
	record("fresh settings retain default presets", len(st.Get().AutoSend.Apps) > 0)
	s := st.Get()
	s.AutoSend.MigrateAIApplications()
	s.AutoSend.Enabled = true
	s.AutoSend.Apps = nil
	must(st.Set(s))
	st, err = config.Open(settingsDir)
	must(err)
	s = st.Get()
	s.AutoSend.MigrateAIApplications()
	record("remove all apps survives save reopen and startup migration", len(s.AutoSend.Apps) == 0 && s.AutoSend.Shortcut("com.openai.chat") == "")
	must(config.WriteJSON(filepath.Join(settingsDir, "settings.json"), map[string]any{"schemaVersion": 6, "autoSend": map[string]any{"aiCatalogVersion": 1, "enabled": true, "default": "enter"}}))
	st, err = config.Open(settingsDir)
	must(err)
	s = st.Get()
	s.AutoSend.MigrateAIApplications()
	record("legacy omitted empty list stays empty", len(s.AutoSend.Apps) == 0)
	s.AutoSend.Apps = []config.AutoSendApp{{BundleID: "com.openai.chat", Shortcut: "off"}}
	must(st.Set(s))
	st, err = config.Open(settingsDir)
	must(err)
	record("explicit off and single app survive reopen", len(st.Get().AutoSend.Apps) == 1 && st.Get().AutoSend.Shortcut("com.openai.chat") == "")
	var manifest struct {
		Revision string `json:"revision"`
		Files    []struct {
			File  string `json:"file"`
			Bytes int64  `json:"bytes"`
		} `json:"files"`
	}
	raw, err := os.ReadFile("internal/localasr/assets/model-manifest.json")
	must(err)
	must(json.Unmarshal(raw, &manifest))
	modelRoot := filepath.Join(root, "local-asr")
	must(os.Mkdir(modelRoot, 0700))
	must(os.WriteFile(filepath.Join(modelRoot, "installed.json"), []byte(manifest.Revision), 0600))
	record("marker without files is absent", !localasr.New(modelRoot, "").Snapshot().Installed)
	// Sparse placeholders exercise metadata only, never inference or checksum success.
	for _, entry := range manifest.Files {
		path := filepath.Join(modelRoot, "model", entry.File)
		must(os.MkdirAll(filepath.Dir(path), 0700))
		f, err := os.Create(path)
		must(err)
		must(f.Truncate(entry.Bytes))
		must(f.Close())
	}
	record("complete sized file layout retains installed state", localasr.New(modelRoot, "").Snapshot().Installed)
	first := filepath.Join(modelRoot, "model", manifest.Files[0].File)
	must(os.Truncate(first, 0))
	record("truncated file is absent", !localasr.New(modelRoot, "").Snapshot().Installed)
	must(os.Remove(first))
	must(os.Mkdir(first, 0700))
	record("directory replacing model file is absent", !localasr.New(modelRoot, "").Snapshot().Installed)
	must(os.RemoveAll(filepath.Join(modelRoot, "model")))
	b, err := json.MarshalIndent(rows, "", "  ")
	must(err)
	must(os.WriteFile(filepath.Join(root, "results.json"), b, 0600))
	fmt.Println(string(b))
	for _, row := range rows {
		if row["passed"] != true {
			os.Exit(1)
		}
	}
}
