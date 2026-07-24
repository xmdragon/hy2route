package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/xmdragon/hy2route/internal/dataset"
	"github.com/xmdragon/hy2route/internal/policy"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("hy2route-data", flag.ContinueOnError)
	domainsPath := flags.String("domains", "", "domain source file")
	ipv4Path := flags.String("ipv4", "", "IPv4 source file")
	outputPath := flags.String("output", "", "compiled data output")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *domainsPath == "" || *ipv4Path == "" || *outputPath == "" {
		return errors.New("--domains, --ipv4, and --output are required")
	}
	domains, err := loadDomains(*domainsPath)
	if err != nil {
		return err
	}
	prefixes, err := loadPrefixes(*ipv4Path)
	if err != nil {
		return err
	}
	return writeData(*outputPath, dataset.Data{Domains: domains, Prefixes: prefixes})
}

func loadDomains(path string) ([]dataset.Domain, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open domains: %w", err)
	}
	defer file.Close()
	seen := make(map[string]struct{})
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		normalized, err := policy.NormalizeDomain(line)
		if err != nil {
			return nil, fmt.Errorf("domain %q: %w", line, err)
		}
		seen[normalized] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read domains: %w", err)
	}
	result := make([]dataset.Domain, 0, len(seen))
	for domain := range seen {
		result = append(result, dataset.Domain{Name: domain})
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Name < result[right].Name })
	return result, nil
}

func loadPrefixes(path string) ([]netip.Prefix, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open IPv4 prefixes: %w", err)
	}
	defer file.Close()
	seen := make(map[netip.Prefix]struct{})
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		prefix, err := netip.ParsePrefix(line)
		if err != nil || !prefix.Addr().Is4() || prefix.Addr().Is4In6() {
			return nil, fmt.Errorf("IPv4 prefix %q is invalid", line)
		}
		seen[prefix.Masked()] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read IPv4 prefixes: %w", err)
	}
	result := make([]netip.Prefix, 0, len(seen))
	for prefix := range seen {
		result = append(result, prefix)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].String() < result[right].String() })
	return result, nil
}

func writeData(path string, data dataset.Data) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".hy2route-data-*")
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := dataset.Write(temporary, data); err != nil {
		temporary.Close()
		return fmt.Errorf("write output: %w", err)
	}
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return fmt.Errorf("set output mode: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close output: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("install output: %w", err)
	}
	return nil
}
