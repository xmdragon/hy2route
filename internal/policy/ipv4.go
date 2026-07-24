package policy

import "net/netip"

type ipv4Node struct {
	zero   *ipv4Node
	one    *ipv4Node
	action Action
}

type ipv4Tree struct{ root ipv4Node }

func (tree *ipv4Tree) add(prefix netip.Prefix, action Action) {
	node := &tree.root
	bytes := prefix.Masked().Addr().As4()
	for bitIndex := 0; bitIndex < prefix.Bits(); bitIndex++ {
		bit := (bytes[bitIndex/8] >> (7 - (bitIndex % 8))) & 1
		if bit == 0 {
			if node.zero == nil {
				node.zero = &ipv4Node{}
			}
			node = node.zero
		} else {
			if node.one == nil {
				node.one = &ipv4Node{}
			}
			node = node.one
		}
	}
	node.action = selectAction(node.action, action)
}

func (tree *ipv4Tree) match(addr netip.Addr) (Action, bool) {
	if !addr.Is4() || addr.Is4In6() {
		return Unknown, false
	}
	node := &tree.root
	result := node.action
	bytes := addr.As4()
	for bitIndex := 0; bitIndex < 32; bitIndex++ {
		bit := (bytes[bitIndex/8] >> (7 - (bitIndex % 8))) & 1
		if bit == 0 {
			node = node.zero
		} else {
			node = node.one
		}
		if node == nil {
			break
		}
		if node.action != Unknown {
			result = node.action
		}
	}
	return result, result != Unknown
}
