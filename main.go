// exa is a thin CLI wrapper for the Exa AI search API.
//
// See `exa search --help` for usage. This package is intentionally tiny —
// all command wiring lives in ./cmd and HTTP logic in ./internal/client.
package main

import (
	"os"

	"github.com/sapihav/exa-cli/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
