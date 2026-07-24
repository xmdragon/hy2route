package failover

import (
	"testing"
	"time"
)

func TestControllerUsesThresholdCooldownAndRecovery(t *testing.T) {
	now := time.Unix(100, 0)
	controller := New(Config{Failures: 3, Successes: 2, Cooldown: 30 * time.Second}, func() time.Time { return now })
	controller.RecordFailure()
	controller.RecordFailure()
	if controller.Mode() != Proxy {
		t.Fatal(controller.Mode())
	}
	controller.RecordFailure()
	if controller.Mode() != DirectCooldown {
		t.Fatal(controller.Mode())
	}
	now = now.Add(30 * time.Second)
	controller.RecordSuccess()
	if controller.Mode() != DirectRecovery {
		t.Fatal(controller.Mode())
	}
	controller.RecordSuccess()
	if controller.Mode() != Proxy {
		t.Fatal(controller.Mode())
	}
}
