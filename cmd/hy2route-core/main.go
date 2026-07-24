package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/xmdragon/hy2route/internal/buildinfo"
	"github.com/xmdragon/hy2route/internal/config"
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
	fmt.Fprintln(stderr, "usage: hy2route-core <version|check|serve>")
	return 2
}
