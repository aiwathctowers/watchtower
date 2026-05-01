// scripts/smoke-remind — one-shot helper that opens the given watchtower.db
// and runs db.NotifyDueTargets exactly once. Used for manual smoke tests of
// the reminder pipeline; not part of any production binary.
//
// Usage:
//
//	go run ./scripts/smoke-remind --db /path/to/watchtower.db
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"watchtower/internal/db"
)

func main() {
	dbPath := flag.String("db", "", "path to watchtower.db")
	flag.Parse()
	if *dbPath == "" {
		fmt.Fprintln(os.Stderr, "--db required")
		os.Exit(2)
	}
	d, err := db.Open(*dbPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer d.Close()
	n, err := d.NotifyDueTargets(time.Now().UTC())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("surfaced %d target(s)\n", n)
}
