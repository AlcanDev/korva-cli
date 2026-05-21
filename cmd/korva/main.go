// Command korva is the Korva CLI: a thin client that authenticates against
// the backbone via a browser device-flow and wires the local editor to the
// remote MCP endpoint.
package main

import (
	"os"

	"github.com/AlcanDev/korva-cli/internal/cmd"
)

func main() {
	os.Exit(cmd.Run(os.Args[1:]))
}
