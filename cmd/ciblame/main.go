// Command ciblame attributes GitHub Actions minutes and cost to the AI
// agents, bots and humans that triggered them.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/amlaban65/ciblame/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Main(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
