// Command gen writes the Markdown configuration reference.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/go-faster/tgpager/internal/config"
)

func main() {
	out := flag.String("out", "CONFIG.md", "Markdown reference output path")
	schemaOut := flag.String("schema-out", "config.schema.json", "JSON Schema output path")
	flag.Parse()

	page, err := config.Reference()
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(*out, page, 0o600); err != nil {
		fail(err)
	}

	schema, err := config.JSONSchema()
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(*schemaOut, schema, 0o600); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
