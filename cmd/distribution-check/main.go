// distribution-check performs the same identity checks used by automatic updates.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"
	"voiceinput/internal/distribution"
)

func main() {
	old := flag.String("installed", "", "installed stable bundle (optional)")
	candidate := flag.String("candidate", "", "candidate bundle")
	flag.Parse()
	if *candidate == "" {
		flag.Usage()
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var err error
	if *old == "" {
		err = distribution.VerifyApp(ctx, *candidate)
	} else {
		err = distribution.VerifyUpgrade(ctx, *old, *candidate)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *old == "" {
		fmt.Println("PASS: approved stable signing identity")
	} else {
		fmt.Println("PASS: stable signing identity, permission continuity and Gatekeeper")
	}
}
