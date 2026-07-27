package firewall

import (
	"fmt"
	"strings"
	"time"
)

type heartbeatEntry struct {
	set   string
	chain string
}

func buildHeartbeatBatch(table string, ttl time.Duration, entries []heartbeatEntry) string {
	var batch strings.Builder
	for _, entry := range entries {
		fmt.Fprintf(&batch, "flush map inet %s %s\n", table, entry.set)
		fmt.Fprintf(
			&batch,
			"add element inet %s %s { 0x00000001 timeout %s : jump %s }\n",
			table,
			entry.set,
			clampTTL(ttl),
			entry.chain,
		)
	}
	return batch.String()
}

func clampTTL(ttl time.Duration) time.Duration {
	if ttl < time.Second {
		return time.Second
	}
	if ttl > 24*time.Hour {
		return 24 * time.Hour
	}
	return ttl
}
