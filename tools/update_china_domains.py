#!/usr/bin/env python3
"""Download and normalize the China-domain source used by hy2route-core."""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import pathlib
import re
import urllib.request


SOURCE = (
    "https://raw.githubusercontent.com/felixonmars/"
    "dnsmasq-china-list/master/accelerated-domains.china.conf"
)
SERVER_LINE = re.compile(r"^server=/([^/]+)/[^/]+$")


def extract_domains(rows: list[str]) -> list[str]:
    domains: set[str] = set()
    for row in rows:
        match = SERVER_LINE.match(row.strip())
        if not match:
            continue
        domain = match.group(1).lower()
        if valid_domain(domain):
            domains.add(domain)
    return sorted(domains)


def valid_domain(domain: str) -> bool:
    if not domain or len(domain) > 253 or "*" in domain:
        return False
    for label in domain.split("."):
        if not label or len(label) > 63 or label.startswith("-") or label.endswith("-"):
            return False
        if any(char not in "abcdefghijklmnopqrstuvwxyz0123456789-" for char in label):
            return False
    return True


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--output",
        type=pathlib.Path,
        default=pathlib.Path(__file__).parents[1] / "data/china-domains.txt",
    )
    args = parser.parse_args()

    with urllib.request.urlopen(SOURCE, timeout=30) as response:
        raw = response.read()
    domains = extract_domains(raw.decode("utf-8").splitlines())
    if not domains:
        raise RuntimeError("source yielded no valid China domains")
    retrieved = dt.datetime.now(dt.timezone.utc).date().isoformat()
    source_hash = hashlib.sha256(raw).hexdigest()
    lines = [
        f"# Source: {SOURCE}",
        f"# Retrieved UTC: {retrieved}",
        f"# Source SHA-256: {source_hash}",
        f"# Domain count: {len(domains)}",
        *domains,
    ]
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text("\n".join(lines) + "\n", encoding="ascii", newline="\n")
    print(f"wrote {len(domains)} domains to {args.output}")


if __name__ == "__main__":
    main()
