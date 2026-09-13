package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/brotherlogic/busybar-bridge/internal/config"
	"github.com/brotherlogic/busybar-bridge/internal/runner"
)

// run parses arguments, constructs the runner, and coordinates the execution loop.
// Returns exit code 0 on normal completion or help request, and exit code 1 on error.
func run(ctx context.Context, args []string, stderr io.Writer) int {
	logger := log.New(stderr, "", log.LstdFlags)

	cfg, err := config.Parse(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		logger.Printf("configuration error: %v", err)
		return 1
	}

	r, err := runner.NewRunner(cfg)
	if err != nil {
		logger.Printf("initialization error: %v", err)
		return 1
	}

	if err := r.Run(ctx); err != nil {
		logger.Printf("runtime error: %v", err)
		return 1
	}

	return 0
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	code := run(ctx, os.Args[1:], os.Stderr)
	if code != 0 {
		stop()
		os.Exit(code)
	}
}
