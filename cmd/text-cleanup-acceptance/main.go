//go:build textcleanupacceptance

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"voiceinput/internal/session"
)

func main() {
	if len(os.Args) != 2 {
		panic("provide a fresh evidence directory")
	}
	if _, e := os.Stat(os.Args[1]); !os.IsNotExist(e) {
		panic("evidence directory must be fresh")
	}
	rows := session.AcceptTextCleanup(os.Args[1])
	b, e := json.MarshalIndent(rows, "", "  ")
	if e != nil {
		panic(e)
	}
	if e = os.WriteFile(os.Args[1]+"/results.json", b, 0600); e != nil {
		panic(e)
	}
	for _, r := range rows {
		if r["passed"] != true {
			fmt.Println(string(b))
			os.Exit(1)
		}
	}
	fmt.Printf("PASS: %d pipeline checks; %s/results.json\n", len(rows), os.Args[1])
}
