package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/geek1011/BookBrowser/cli"
)

var curversion = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, curversion))
}
