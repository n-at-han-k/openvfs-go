package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"example.com/project/packages/openvfs"

	"github.com/spf13/cobra"
)

// One Cobra command per upstream binary. Flag parsing is delegated wholesale
// to ProcessArgs, the transpilation of openvfsfuse/main.cpp::processArgs, so
// the accepted options are exactly the upstream ones.
func main() {
	root := &cobra.Command{
		Use:                "openvfs",
		DisableFlagParsing: true,
		SilenceUsage:       true,
		SilenceErrors:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			argv := append([]string{os.Args[0]}, args...)

			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			if code := openvfs.Main(ctx, argv); code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}

	if err := root.Execute(); err != nil {
		os.Exit(-1)
	}
}
