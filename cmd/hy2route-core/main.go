package main

import (
	"fmt"
	"os"

	"github.com/xmdragon/hy2route/internal/buildinfo"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "version" {
		fmt.Println(buildinfo.String())
		return
	}
	fmt.Fprintln(os.Stderr, "usage: hy2route-core <version|check|serve>")
	os.Exit(2)
}
