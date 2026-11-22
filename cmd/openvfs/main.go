package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

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
			options, ok := processArgs(argv, cmd.OutOrStdout(), cmd.ErrOrStderr())
			if !ok {
				os.Exit(-1) // upstream main returns -1
			}

			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			ready := make(chan error, 1)
			if err := initialize(ctx, options, ready); err != nil {
				if e := <-ready; e != nil {
					cmd.PrintErrln(e)
				}
				os.Exit(-1)
			}
			return nil
		},
	}

	if err := root.Execute(); err != nil {
		os.Exit(-1)
	}
}
