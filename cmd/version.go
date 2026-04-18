package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// version is populated at build time via
// `-ldflags "-X github.com/sapihav/exa-cli/cmd.version=vX.Y.Z"`.
// Defaults to "dev" for `go install` / local builds.
var version = "dev"

// Version returns the compiled-in version string.
func Version() string { return version }

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the CLI version as JSON",
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := json.Marshal(map[string]string{
			"schema_version": "1",
			"provider":       "exa",
			"command":        "version",
			"version":        version,
		})
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
