package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/geo-suite/geovisor/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := cli.RunContext(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(cli.ExitCode(err))
	}
}
