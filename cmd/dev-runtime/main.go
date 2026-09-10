package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/PluxelJS/Proxy-LLM-API/internal/cli"
)

func main() {
	syscall.Umask(0077)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	os.Exit(cli.Main(ctx, os.Args[1:]))
}
