"""Handles connection to the centrifugo server."""

import asyncio

import centrifuge
from websockets import connect

from client.config import settings


async def run_client(client: centrifuge.Client) -> None:
    """"""
    await client.connect()


async def disconnect(client: centrifuge.Client, received_signal):
    await client.disconnect()
    for task in asyncio.all_tasks():
        if task is asyncio.current_task():
            continue

        task.cancel()


def get_client() -> centrifuge.Client:
    client = centrifuge.Client(settings.centrifugo_connection_url)
    asyncio.ensure_future(run_client(client))
    return client
