"""Checks the web client's index.html, for deploy/smoke.py (#114).

This module needs no third-party packages, so the deploy tests can import it
without httpx, which smoke.py needs.
"""
from __future__ import annotations


def web_bundle_referenced(index_html: str, bundle: str) -> bool:
    """True when the page loads the script, under /play/ and with the exact file name."""
    return f'"/play/{bundle}"' in index_html
