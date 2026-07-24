//go:build linux

package firewall

import (
	"context"
	"fmt"
	"net/netip"
	"os/exec"
	"sync"
	"time"

	"github.com/google/nftables"
)

type NftSetClient struct {
	mu                    sync.Mutex
	table                 *nftables.Table
	direct, inspect, core *nftables.Set
	states                map[netip.Addr]SetState
}

func NewNftSetClient(tableName string) *NftSetClient {
	table := &nftables.Table{Name: tableName, Family: nftables.TableFamilyINet}
	return &NftSetClient{table: table, direct: &nftables.Set{Table: table, Name: "direct4", KeyType: nftables.TypeIPAddr}, inspect: &nftables.Set{Table: table, Name: "inspect4", KeyType: nftables.TypeIPAddr}, core: &nftables.Set{Table: table, Name: "core_state", KeyType: nftables.TypeMark, DataType: nftables.TypeVerdict, IsMap: true}, states: make(map[netip.Addr]SetState)}
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
func key(ip netip.Addr) []byte { return append([]byte(nil), ip.AsSlice()...) }
func (c *NftSetClient) Replace(ctx context.Context, updates []SetUpdate) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	n, err := nftables.New()
	if err != nil {
		return err
	}
	for _, u := range updates {
		k := key(u.IP)
		if old := c.states[u.IP]; old == SetDirect {
			_ = n.SetDeleteElements(c.direct, []nftables.SetElement{{Key: k}})
		} else if old == SetInspect {
			_ = n.SetDeleteElements(c.inspect, []nftables.SetElement{{Key: k}})
		}
		if u.State == SetDirect {
			if err := n.SetAddElements(c.direct, []nftables.SetElement{{Key: k, Timeout: clampTTL(u.TTL)}}); err != nil {
				return err
			}
		} else if u.State == SetInspect {
			if err := n.SetAddElements(c.inspect, []nftables.SetElement{{Key: k, Timeout: clampTTL(u.TTL)}}); err != nil {
				return err
			}
		}
		c.states[u.IP] = u.State
	}
	return n.Flush()
}
func (c *NftSetClient) Heartbeat(ctx context.Context, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	command := exec.CommandContext(ctx, "nft", fmt.Sprintf("add element inet %s core_state { 0x00000001 timeout %s : jump active }", c.table.Name, clampTTL(ttl)))
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("nft heartbeat: %w: %s", err, output)
	}
	return nil
}
