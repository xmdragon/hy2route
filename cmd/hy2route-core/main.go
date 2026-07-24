package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/xmdragon/hy2route/internal/buildinfo"
	"github.com/xmdragon/hy2route/internal/config"
	"github.com/xmdragon/hy2route/internal/control"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "version" {
		fmt.Fprintln(stdout, buildinfo.String())
		return 0
	}
	if len(args) >= 1 && args[0] == "check" {
		flags := flag.NewFlagSet("check", flag.ContinueOnError)
		flags.SetOutput(stderr)
		path := flags.String("config", "/tmp/hy2route/core.json", "configuration path")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		if _, err := config.Load(*path); err != nil {
			fmt.Fprintf(stderr, "configuration invalid: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "hy2route-core configuration is valid")
		return 0
	}
	if len(args) >= 1 && args[0] == "status" {
		flags := flag.NewFlagSet("status", flag.ContinueOnError)
		flags.SetOutput(stderr)
		path := flags.String("socket", "/var/run/hy2route-core.sock", "control socket path")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		snapshot, err := control.Request(*path, "status")
		if err != nil {
			fmt.Fprintf(stderr, "status unavailable: %v\n", err)
			return 1
		}
		if err := json.NewEncoder(stdout).Encode(snapshot); err != nil {
			fmt.Fprintf(stderr, "status output failed: %v\n", err)
			return 1
		}
		return 0
	}
	if len(args) >= 1 && args[0] == "serve" {
		flags := flag.NewFlagSet("serve", flag.ContinueOnError)
		flags.SetOutput(stderr)
		path := flags.String("config", "/tmp/hy2route/core.json", "configuration path")
		dnsOnly := flags.Bool("dns-only", false, "serve only the DNS listener")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		cfg, err := config.Load(*path)
		if err != nil {
			fmt.Fprintf(stderr, "configuration invalid: %v\n", err)
			return 1
		}
		app, err := newApplication(cfg, *dnsOnly)
		if err != nil {
			fmt.Fprintf(stderr, "startup failed: %v\n", err)
			return 1
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := app.Run(ctx); err != nil {
			fmt.Fprintf(stderr, "runtime failed: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Fprintln(stderr, "usage: hy2route-core <version|check|status|serve>")
	return 2
}
