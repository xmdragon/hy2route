package failover

import (
	"sync"
	"time"
)

type Mode uint8

const (
	Proxy Mode = iota
	DirectCooldown
	DirectRecovery
)

type Config struct {
	Failures, Successes int
	Cooldown            time.Duration
}
type Controller struct {
	mu                  sync.Mutex
	cfg                 Config
	now                 func() time.Time
	mode                Mode
	failures, successes int
	until               time.Time
}

func New(cfg Config, now func() time.Time) *Controller {
	if cfg.Failures < 1 {
		cfg.Failures = 1
	}
	if cfg.Successes < 1 {
		cfg.Successes = 1
	}
	if now == nil {
		now = time.Now
	}
	return &Controller{cfg: cfg, now: now}
}
func (c *Controller) Mode() Mode {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.mode == DirectCooldown && !c.now().Before(c.until) {
		c.mode = DirectRecovery
	}
	return c.mode
}
func (c *Controller) RecordFailure() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.successes = 0
	if c.mode == Proxy {
		c.failures++
		if c.failures >= c.cfg.Failures {
			c.mode = DirectCooldown
			c.until = c.now().Add(c.cfg.Cooldown)
		}
	} else {
		c.mode = DirectCooldown
		c.until = c.now().Add(c.cfg.Cooldown)
	}
}
func (c *Controller) RecordSuccess() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.mode == DirectCooldown && !c.now().Before(c.until) {
		c.mode = DirectRecovery
	}
	if c.mode == DirectRecovery {
		c.successes++
		if c.successes >= c.cfg.Successes {
			c.mode = Proxy
			c.failures = 0
			c.successes = 0
		}
	}
}
