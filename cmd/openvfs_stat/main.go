package main

import (
	"os"

	"github.com/spf13/cobra"
)

// Upstream vfsstat/main.cpp takes exactly one positional argument: the path
// to inspect. There are no flags; parsing stays entirely in RunStat.
func main() {
	root := &cobra.Command{
		Use:                "openvfs_stat",
		DisableFlagParsing: true,
		SilenceUsage:       true,
		SilenceErrors:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			argv := append([]string{os.Args[0]}, args...)
			if code := runStat(argv); code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}

	if err := root.Execute(); err != nil {
		os.Exit(-1)
	}
}
