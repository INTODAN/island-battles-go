"""Fixtures shared by the offline and live conformance tiers."""

import os

import pytest_asyncio

from island_conformance.adapter import AdapterProcess

# The live tier needs the stack started by scripts/test-client-conformance.sh.
if "LOBBY_TEST_API_URL" not in os.environ:
    collect_ignore = ["live"]


@pytest_asyncio.fixture
async def adapter():
    async with AdapterProcess() as process:
        yield process
