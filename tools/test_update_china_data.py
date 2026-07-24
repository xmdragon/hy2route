#!/usr/bin/env python3
"""Offline tests for the reproducible China routing-data generators."""

from __future__ import annotations

import importlib.util
import pathlib
import unittest


ROOT = pathlib.Path(__file__).parents[1]


def load_module(name: str, filename: str):
    spec = importlib.util.spec_from_file_location(name, ROOT / "tools" / filename)
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class ChinaDataGeneratorTests(unittest.TestCase):
    def test_domain_extraction_normalizes_and_filters(self) -> None:
        domains = load_module("china_domains", "update_china_domains.py")
        rows = [
            "# generated",
            "server=/WeChat.COM/114.114.114.114",
            "server=/wx.qq.com/114.114.114.114",
            "server=/*.wildcard.example/114.114.114.114",
            "server=/unicode.例子/114.114.114.114",
            "address=/ignored.example/127.0.0.1",
        ]
        self.assertEqual(domains.extract_domains(rows), ["wechat.com", "wx.qq.com"])

    def test_apnic_extraction_collapses_ipv4_ranges(self) -> None:
        china4 = load_module("china4", "update_china4.py")
        rows = [
            "2|apnic|20260724|0|0|0|0|0|0|0|0|0|0|0|0|0|0|0",
            "apnic|CN|ipv4|203.0.113.0|128|20260724|allocated",
            "apnic|CN|ipv4|203.0.113.128|128|20260724|allocated",
            "apnic|US|ipv4|198.51.100.0|256|20260724|allocated",
        ]
        self.assertEqual([str(item) for item in china4.extract_china_prefixes(rows)], ["203.0.113.0/24"])


if __name__ == "__main__":
    unittest.main()
