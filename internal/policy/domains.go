package policy

import (
	"errors"
	"strings"
)

type domainNode struct {
	children map[string]*domainNode
	suffix   Action
	exact    Action
}

type domainTree struct{ root domainNode }

func NormalizeDomain(value string) (string, error) {
	value = strings.TrimSuffix(strings.ToLower(value), ".")
	if value == "" || len(value) > 253 {
		return "", errors.New("domain is empty or too long")
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("domain has an invalid label")
		}
		for _, char := range label {
			if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-') {
				return "", errors.New("domain is not ASCII DNS syntax")
			}
		}
	}
	return value, nil
}

func (tree *domainTree) add(domain string, exact bool, action Action) {
	node := &tree.root
	labels := strings.Split(domain, ".")
	for index := len(labels) - 1; index >= 0; index-- {
		if node.children == nil {
			node.children = make(map[string]*domainNode)
		}
		child := node.children[labels[index]]
		if child == nil {
			child = &domainNode{}
			node.children[labels[index]] = child
		}
		node = child
	}
	if exact {
		node.exact = selectAction(node.exact, action)
	} else {
		node.suffix = selectAction(node.suffix, action)
	}
}

func (tree *domainTree) match(domain string) (Action, bool) {
	node := &tree.root
	labels := strings.Split(domain, ".")
	var result Action
	for index := len(labels) - 1; index >= 0; index-- {
		if node.children == nil || node.children[labels[index]] == nil {
			break
		}
		node = node.children[labels[index]]
		if node.suffix != Unknown {
			result = selectAction(result, node.suffix)
		}
		if index == 0 && node.exact != Unknown {
			result = selectAction(result, node.exact)
		}
	}
	return result, result != Unknown
}

func selectAction(current, candidate Action) Action {
	if current == Proxy || candidate == Proxy {
		return Proxy
	}
	if candidate != Unknown {
		return candidate
	}
	return current
}
