// Command voice-quality-acceptance checks persisted correction candidates using
// predefined failure cases. It uses only an isolated dictionary directory.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"voiceinput/internal/dict"
)

func main() {
	if len(os.Args) != 2 {
		panic("usage: voice-quality-acceptance NEW_OUTPUT_DIRECTORY")
	}
	out := os.Args[1]
	if err := os.Mkdir(out, 0700); err != nil {
		panic(err)
	}
	cases := []struct {
		Name, Before, After, Source string
		Want                        int
	}{
		{"confirmed_term", "这个产品的 mark 的 demo 地址是多少？", "这个产品的 mock 的 demo 地址是多少？", "observed-edit", 1},
		{"chinese_to_english", "怎么连这个杰森都识别不了呢？", "怎么连这个 json 都识别不了呢？", "observed-edit", 1},
		{"ime_snapshot", "这个效果还可以呀", "这个效果还可以，ying'g'shi", "observed-edit", 0},
		{"short_whole_replacement", "还行", "与Codex协作", "observed-edit", 0},
		{"short_sentence_rewrite", "这个有电吗？", "好了吧，n", "observed-edit", 0},
		{"brief_anchored_term", "用mark打开", "用mock打开", "observed-edit", 1},
		{"single_character", "放到底部的位置", "放到顶部的位置", "observed-edit", 1},
		{"explicit_whole_term", "杰森", "json", "history-revision", 1},
		{"valid_punctuation_term", "用 C 语言做这个", "用 C++ 语言做这个", "observed-edit", 0},
	}
	var results []map[string]any
	all := true
	for _, tc := range cases {
		dir := filepath.Join(out, tc.Name)
		if e := os.Mkdir(dir, 0700); e != nil {
			panic(e)
		}
		d, e := dict.Open(dir)
		if e != nil {
			panic(e)
		}
		n, note := d.Propose(tc.Before, tc.After, tc.Source, tc.Name)
		reopened, e := dict.Open(dir)
		if e != nil {
			panic(e)
		}
		pending := reopened.Pending()
		ok := n == tc.Want && len(pending) == tc.Want
		all = all && ok
		results = append(results, map[string]any{"case": tc.Name, "before": tc.Before, "after": tc.After, "want": tc.Want, "passed": ok, "note": note, "persisted": pending})
	}
	b, _ := json.MarshalIndent(map[string]any{"passed": all, "scope": "isolated proposal and persistence, not native IME capture", "cases": results}, "", "  ")
	if e := os.WriteFile(filepath.Join(out, "results.json"), b, 0600); e != nil {
		panic(e)
	}
	if !all {
		os.Exit(1)
	}
}
