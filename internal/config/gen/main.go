// Command gen writes the Markdown configuration reference.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/go-faster/tgpager/internal/config"
)

func main() {
	out := flag.String("out", "CONFIG.md", "output path")
	flag.Parse()

	page, err := config.Reference()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, page, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
