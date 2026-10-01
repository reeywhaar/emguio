// Command emguio serves a webmail client over the mail servers its users name.
package main

import (
	"os"
	"time"

	"emguio/internal/cli"
)

func main() {
	// Everything stored and logged is UTC.
	time.Local = time.UTC
	os.Exit(cli.Execute())
}
