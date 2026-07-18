#!/usr/bin/env python3
"""Generate the packaged nftables China IPv4 set from APNIC data."""

from __future__ import annotations

import argparse
import ipaddress
import pathlib
import urllib.request


APNIC_URL = "https://ftp.apnic.net/stats/apnic/delegated-apnic-latest"


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--output",
        type=pathlib.Path,
        default=pathlib.Path(__file__).parents[1]
        / "files/usr/share/hy2route/china4.nft",
    )
    args = parser.parse_args()

    with urllib.request.urlopen(APNIC_URL, timeout=30) as response:
        rows = response.read().decode("ascii").splitlines()

    snapshot_date = "unknown"
    for row in rows:
        fields = row.split("|")
        if len(fields) >= 3 and fields[:2] == ["2", "apnic"]:
            raw_date = fields[2]
            if len(raw_date) == 8 and raw_date.isdigit():
                snapshot_date = (
                    f"{raw_date[:4]}-{raw_date[4:6]}-{raw_date[6:8]}"
                )
            break

    networks: list[ipaddress.IPv4Network] = []
    for row in rows:
        fields = row.split("|")
        if len(fields) < 7 or fields[1:3] != ["CN", "ipv4"]:
            continue
        start = ipaddress.IPv4Address(fields[3])
        count = int(fields[4])
        end = ipaddress.IPv4Address(int(start) + count - 1)
        networks.extend(ipaddress.summarize_address_range(start, end))

    collapsed = list(ipaddress.collapse_addresses(networks))
    lines = [
        "# Generated from APNIC delegated-apnic-latest.",
        f"# APNIC snapshot: {snapshot_date}.",
        "# Do not edit manually.",
        "add element inet hy2route china4 {",
    ]
    for index in range(0, len(collapsed), 8):
        chunk = ", ".join(str(net) for net in collapsed[index : index + 8])
        suffix = "," if index + 8 < len(collapsed) else ""
        lines.append(f"\t{chunk}{suffix}")
    lines.append("}")
    args.output.parent.mkdir(parents=True, exist_ok=True)
    with args.output.open("w", encoding="ascii", newline="\n") as output:
        output.write("\n".join(lines) + "\n")
    print(f"wrote {len(collapsed)} collapsed IPv4 prefixes to {args.output}")


if __name__ == "__main__":
    main()
