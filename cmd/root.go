// Package cmd wires the Cobra command tree for the exa CLI.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Exit codes per README / CLI-tools-ROADMAP §2.
const (
	ExitSuccess     = 0
	ExitAPIError    = 1
	ExitConfigError = 2
	ExitNetworkErr  = 3
)

// Globals bound to persistent flags on the root command.
var (
	flagPretty  bool
	flagVerbose bool
	flagQuiet   bool
	flagOut     string
)

// rootCmd is the top-level `exa` command. It does nothing on its own; a
// subcommand must be given.
var rootCmd = &cobra.Command{
	Use:           "exa",
	Short:         "Thin CLI wrapper for the Exa AI search API",
	Long:          "exa is a thin Go CLI that wraps the Exa AI HTTP API.\n\nM1 ships the `search` subcommand. See https://docs.exa.ai for the underlying API.",
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command and returns an exit code.
// Keeping this separate from os.Exit lets tests (and future main.go) decide
// how to terminate.
func Execute() int {
	if err := rootCmd.Execute(); err != nil {
		// Subcommands already print their own errors to stderr; we only
		// handle the exit code here. If a cobra-level error surfaces
		// (e.g., unknown flag), print it.
		if _, ok := err.(*exitCodeError); !ok {
			fmt.Fprintln(os.Stderr, "error:", err)
			return ExitConfigError
		}
		return err.(*exitCodeError).code
	}
	return ExitSuccess
}

// exitCodeError lets a subcommand's RunE tell Execute which exit code to use
// without printing its own message twice. The message has already been
// written to stderr by the time this is returned.
type exitCodeError struct {
	code int
	msg  string
}

func (e *exitCodeError) Error() string { return e.msg }

func init() {
	rootCmd.PersistentFlags().BoolVar(&flagPretty, "pretty", false, "indent JSON output")
	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "verbose logging to stderr")
	rootCmd.PersistentFlags().BoolVarP(&flagQuiet, "quiet", "q", false, "suppress stderr logging")
	rootCmd.PersistentFlags().StringVarP(&flagOut, "out", "o", "", "write JSON output to file instead of stdout")
}
