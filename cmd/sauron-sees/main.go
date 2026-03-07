package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"sauron-sees/internal/app"
)

func main() {
	code := run()
	os.Exit(code)
}

func run() int {
	global := flag.NewFlagSet("sauron-sees", flag.ContinueOnError)
	configPath := global.String("config", "", "path to config.toml")
	if err := global.Parse(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	args := global.Args()
	if len(args) == 0 {
		printUsage()
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cli := app.NewCLI(*configPath, os.Stdout, os.Stderr)
	if err := cli.Run(ctx, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "usage: sauron-sees [--config path] <command> [args]\n")
	fmt.Fprintf(os.Stderr, "commands: agent, close-day, weekly-summary, capture-now, doctor, install-startup, uninstall-startup\n")
}
