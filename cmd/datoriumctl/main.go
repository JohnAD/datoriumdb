// Command datoriumctl is the DatoriumDB administrative CLI. All behavior
// lives in internal/ctl so it can be unit- and integration-tested; this
// file is a thin process entry point.
package main

import (
	"os"

	"github.com/JohnAD/datoriumdb/internal/ctl"
)

func main() {
	os.Exit(ctl.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
