// Package version exposes build metadata for the Korva CLI.
package version

// Version is the semantic version of the CLI.
// It is overridden at build time via -ldflags.
var Version = "0.0.0-dev"
