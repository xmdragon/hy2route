package buildinfo

import "testing"

func TestStringContainsVersionAndCommit(t *testing.T) {
	Version, Commit = "0.2.0-dev", "abc123"
	if got := String(); got != "hy2route-core 0.2.0-dev (abc123)" {
		t.Fatalf("String() = %q", got)
	}
}
