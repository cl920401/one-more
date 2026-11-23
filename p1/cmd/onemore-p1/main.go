package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	p1 "onemore.local/p1"
)

func main() {
	scenario := flag.String("scenario", "all", "all, normal, scope, cancel, revoke, reject, queued-revoke, queued-control; built-in synthetic data only")
	decision := flag.String("decision", "", "explicit synthetic employee decision: 采用, 改后采用, 退回; default pending")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "no file or endpoint arguments are supported")
		os.Exit(2)
	}
	report, err := p1.RunDemo(*scenario, *decision)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintln(os.Stderr, "output failed")
		os.Exit(1)
	}
}
