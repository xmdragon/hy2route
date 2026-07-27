package firewall

import (
	"testing"
	"time"
)

func TestBuildHeartbeatBatchRefreshesVerdictMapsAtomically(t *testing.T) {
	entries := []heartbeatEntry{
		{set: "core_state", chain: "active"},
		{set: "output_state", chain: "output_active"},
	}
	want := "" +
		"flush map inet hy2route core_state\n" +
		"add element inet hy2route core_state { 0x00000001 timeout 10s : jump active }\n" +
		"flush map inet hy2route output_state\n" +
		"add element inet hy2route output_state { 0x00000001 timeout 10s : jump output_active }\n"

	if got := buildHeartbeatBatch("hy2route", 10*time.Second, entries); got != want {
		t.Fatalf("heartbeat batch mismatch:\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildHeartbeatBatchClampsTTL(t *testing.T) {
	entries := []heartbeatEntry{{set: "core_state", chain: "active"}}
	want := "" +
		"flush map inet hy2route core_state\n" +
		"add element inet hy2route core_state { 0x00000001 timeout 1s : jump active }\n"

	if got := buildHeartbeatBatch("hy2route", time.Millisecond, entries); got != want {
		t.Fatalf("heartbeat batch mismatch:\n%s\nwant:\n%s", got, want)
	}
}
