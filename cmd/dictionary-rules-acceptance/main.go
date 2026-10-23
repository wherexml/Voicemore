// Command dictionary-rules-acceptance exercises the production persisted
// dictionary lifecycle. Every write is confined to a fresh output directory.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"voiceinput/internal/dict"
)

func main() {
	if len(os.Args) != 2 {
		panic("usage: dictionary-rules-acceptance NEW_OUTPUT_DIRECTORY")
	}
	out := os.Args[1]
	must(os.Mkdir(out, 0700))
	var results []map[string]any
	all := true
	check := func(name string, ok bool) {
		results = append(results, map[string]any{"case": name, "passed": ok})
		all = all && ok
	}
	d, err := dict.Open(out)
	must(err)
	must(d.Add(dict.Entry{Kind: dict.KindRule, Wrong: "mark", Term: "mock", Context: []string{" 产品 ", "产品", ""}, Source: "manual"}))
	first := d.Entries("")[0]
	check("normalize context", reflect.DeepEqual(first.Context, []string{"产品"}))
	applied, _ := d.Apply("产品 mark marketing")
	check("scoped word replacement", applied == "产品 mock marketing")
	applied, _ = d.Apply("mark marketing")
	check("context does not match", applied == "mark marketing")
	must(d.Add(dict.Entry{Kind: dict.KindRule, Wrong: "clod", Term: "Claude", Source: "manual"}))
	must(d.Update(first.ID, dict.Entry{Kind: dict.KindRule, Wrong: "mark", Term: "Mock", Context: []string{"界面"}}))
	reopened, err := dict.Open(out)
	must(err)
	edited := reopened.Entries("")[0]
	check("edit keeps id time order and source", edited.ID == first.ID && edited.CreatedAt.Equal(first.CreatedAt) && edited.Source == first.Source && len(reopened.Entries("")) == 2)
	applied, _ = reopened.Apply("界面 mark marketing")
	check("edited rule persists and applies", applied == "界面 Mock marketing")
	applied, _ = reopened.Apply("产品 mark")
	check("old scope no longer applies", applied == "产品 mark")
	before, _ := os.ReadFile(filepath.Join(out, "dictionary.json"))
	for _, e := range []dict.Entry{
		{Kind: dict.KindRule, Wrong: "", Term: "mock"},
		{Kind: dict.KindRule, Wrong: "same", Term: "same"},
		{Kind: dict.KindRule, Wrong: "a\nb", Term: "mock"},
		{Kind: dict.KindRule, Wrong: "clod", Term: "Other"},
		{Kind: dict.KindRule, Wrong: "底", Term: "顶"},
	} {
		check("invalid update rejected", d.Update(first.ID, e) != nil)
	}
	after, _ := os.ReadFile(filepath.Join(out, "dictionary.json"))
	check("invalid edits preserve bytes", string(before) == string(after))
	check("missing entry rejected", d.Update("missing", dict.Entry{Term: "test"}) != nil)
	must(d.Add(dict.Entry{Kind: dict.KindRule, Wrong: "底", Term: "顶", Context: []string{"放到底部的位置"}, Source: "manual"}))
	applied, _ = d.Apply("放到底部的位置，其他底部不变")
	check("single Han local phrase", applied == "放到顶部的位置，其他底部不变")
	// Force the actual atomic persistence path to fail, while leaving its saved file intact.
	failureDir := filepath.Join(out, "unwritable")
	must(os.Mkdir(failureDir, 0700))
	f, err := dict.Open(failureDir)
	must(err)
	must(f.Add(dict.Entry{Term: "original"}))
	id := f.Entries("")[0].ID
	must(os.Rename(failureDir, failureDir+"-saved"))
	must(os.WriteFile(failureDir, []byte("block directory"), 0600))
	check("save failure reported", f.Update(id, dict.Entry{Term: "replacement"}) != nil)
	check("save failure preserves in-memory entry", f.Entries("")[0].Term == "original")
	must(os.Remove(failureDir))
	must(os.Rename(failureDir+"-saved", failureDir))
	f, err = dict.Open(failureDir)
	must(err)
	check("save failure preserves disk entry", f.Entries("")[0].Term == "original")
	// Legacy suggestions are read from disk; filtering must not mutate the file.
	legacy := filepath.Join(out, "legacy")
	must(os.Mkdir(legacy, 0700))
	candidates := []dict.Candidate{
		{ID: "ime", Wrong: "呀", Right: "，ying'g'shi", Sentence: "我今天都没用这个 API 呀", Source: "observed-edit"},
		{ID: "rewrite", Wrong: "还行", Right: "与Codex协作", Sentence: "还行", Source: "observed-edit"},
		{ID: "tone", Wrong: "呢", Right: "啊", Sentence: "具体是哪方面比较强呢？", Source: "observed-edit"},
		{ID: "valid", Wrong: "mark", Right: "mock", Sentence: "这个产品的 mark 地址", Source: "observed-edit"},
		{ID: "spacing", Wrong: "V六", Right: "V6", Sentence: "把 V 六的背景改成流动的风格吧", Source: "observed-edit"},
		{ID: "missing-source", Wrong: "no号", Right: "know-how", Sentence: "旧版只保留了长句开头，未保存修改位置", Source: "observed-edit"},
		{ID: "explicit", Wrong: "杰森", Right: "json", Sentence: "杰森", Source: "history-revision"},
		{ID: "ignored", Wrong: "mork", Right: "mock", Sentence: "这个产品的 mork 地址", Source: "observed-edit", Ignored: true},
	}
	b, _ := json.Marshal(map[string]any{"entries": []dict.Entry{{ID: "confirmed", Kind: dict.KindRule, Wrong: "呢", Term: "啊", Context: []string{"好呢"}, Source: "manual"}}, "candidates": candidates})
	must(os.WriteFile(filepath.Join(legacy, "dictionary.json"), b, 0600))
	l, err := dict.Open(legacy)
	must(err)
	pending := l.Pending()
	check("legacy noise hidden and valid suggestions retained", len(pending) == 4 && pending[0].ID == "valid" && pending[1].ID == "spacing" && pending[2].ID == "missing-source" && pending[3].ID == "explicit")
	after, _ = os.ReadFile(filepath.Join(legacy, "dictionary.json"))
	check("read filtering preserves file and confirmed rule", string(after) == string(b) && len(l.Entries("")) == 1)
	must(l.Accept("valid", dict.KindRule, "mark", "mock", nil))
	l, err = dict.Open(legacy)
	must(err)
	applied, _ = l.Apply("mark marketing")
	check("accept reopen apply", applied == "mock marketing" && len(l.Pending()) == 3)
	n, _ := l.Propose(strings.Repeat("字", 1025), "词", "history-revision", "large")
	check("bounded diff", n == 0)
	must(d.Delete(first.ID))
	d, err = dict.Open(out)
	must(err)
	applied, _ = d.Apply("界面 mark")
	check("delete persists and stops replacement", applied == "界面 mark")
	b, _ = json.MarshalIndent(map[string]any{"passed": all, "scope": "production dictionary persistence and apply; synthetic inputs, native UI separate", "checks": results}, "", "  ")
	must(os.WriteFile(filepath.Join(out, "results.json"), b, 0600))
	fmt.Println(string(b))
	if !all {
		os.Exit(1)
	}
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}
