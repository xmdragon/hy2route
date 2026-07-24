package buildinfo

import "fmt"

var (
	Version = "0.2.0-dev"
	Commit  = "unknown"
)

func String() string {
	return fmt.Sprintf("hy2route-core %s (%s)", Version, Commit)
}
