"""People outside the roster watch a started game with the join code (#103)."""

import asyncio
from uuid import uuid4

import centrifuge
import httpx
import pytest
from integration.test_live import BASE, create
from integration.test_live import guests as guests_fixture
from integration.test_protocol import Publications, connection_client, denied_subscription, disconnect
from lobby_client import LobbyError, Scope
from test_lobby_binding import game_auth, started_lobby

guests = guests_fixture
pytestmark = pytest.mark.asyncio


class WatchEvents(Publications):
    """A watch subscription's publications, and how it ended."""

    def __init__(self):
        super().__init__()
        self.subscribed = asyncio.Event()
        self.unsubscribed: asyncio.Queue = asyncio.Queue()

    async def on_subscribed(self, ctx):
        self.subscribed.set()

    async def on_unsubscribed(self, ctx):
        self.unsubscribed.put_nowait(ctx)


async def bearer(player):
    """A player token for the game's routes, which watchers read too."""
    return await game_auth(player)


async def complete_turn(http, player, lobby_id):
    state = (await http.get(f"/game/from-lobby/{lobby_id}/generation", headers=await bearer(player))).json()
    response = await http.post("/generation/commands", headers=await bearer(player), json={
        "game_id": state["game_id"], "generation_id": state["generation_id"], "request_id": str(uuid4()),
        "commands": [], "complete": True,
    })
    assert response.status_code == 200, response.text
    return state["generation_id"]


async def test_a_watcher_outside_the_roster_follows_the_game_until_the_host_kicks_them(guests):
    alice, bob, scope = await started_lobby(guests, "island-battles")
    robin, stranger = await guests("Robin"), await guests("Stranger")
    code = (await alice.snapshot(scope)).lobby.join_code

    # A wrong code, and a code of another game, find no lobby.
    other = await create(stranger, "quiz-demo")
    for game_key, join_code in (("island-battles", "ABCDEFGH" if code != "ABCDEFGH" else "HGFEDCBA"),
                                ("island-battles", other.lobby.join_code), ("quiz-demo", code)):
        with pytest.raises(LobbyError) as refused:
            await robin.watch(game_key=game_key, join_code=join_code)
        assert (refused.value.code, refused.value.status) == ("not_found", 404)

    access = await robin.watch(game_key="island-battles", join_code=code.lower())
    assert (str(access.lobby_id), access.channel, access.member) == (
        scope.lobby_id, f"watch:island-battles:{scope.lobby_id}", False)
    # A member keeps their member access instead.
    assert (await bob.watch(game_key="island-battles", join_code=code)).member is True
    snapshot = await alice.snapshot(scope)
    assert snapshot.watcher_count == 1 and len(snapshot.members) == 2
    [listed] = await bob.watchers(scope)
    assert listed.display_name == "Robin"
    robin_id = str(listed.player_id)

    events = WatchEvents()
    watcher = await connection_client(robin.credential)
    outsider = await connection_client(stranger.credential)
    try:
        subscription = watcher.new_subscription(access.channel, events=events, recoverable=True, positioned=True)
        await subscription.subscribe()
        await asyncio.wait_for(events.subscribed.wait(), 5)
        # Only watchers and members get the watch channel, and the room
        # channel stays the members'.
        await denied_subscription(outsider, access.channel)
        await denied_subscription(watcher, snapshot.channel)

        async with httpx.AsyncClient(base_url=BASE) as http:
            watched = await http.get(f"/game/from-lobby/{scope.lobby_id}/watch", headers=await bearer(robin))
            assert watched.status_code == 200, watched.text
            state = watched.json()
            assert [team["players"] for team in state["teams"]] == [["Alice"], ["Bob"]]
            assert "player_id" not in state and "bot" not in state and "submitted" not in state
            history = await http.get(f"/game/from-lobby/{scope.lobby_id}/watch/demo", headers=await bearer(robin))
            assert history.status_code == 200 and len(history.json()["frames"]) == 1
            for path in ("watch", "watch/demo"):
                denied = await http.get(f"/game/from-lobby/{scope.lobby_id}/{path}", headers=await bearer(stranger))
                assert denied.status_code == 404, denied.text

            # The roster alone plays the turn, and the watcher gets it live.
            played = await complete_turn(http, alice, scope.lobby_id)
            await complete_turn(http, bob, scope.lobby_id)
            event = await asyncio.wait_for(events.items.get(), 10)
            assert event["type"] == "game.next_generation" and event["schema_version"] == 1
            assert event["state"]["channel"] == access.channel and event["state"]["generation_id"] > played
            assert "player_id" not in event["state"]

            # The host kicks Robin, who is unsubscribed at once and can't
            # come back with the code.
            async with httpx.AsyncClient(base_url=BASE) as raw:
                kicked = await raw.delete(
                    f"/lobby/v1/games/island-battles/lobbies/{scope.lobby_id}/watchers/{robin_id}",
                    headers={"Authorization": f"Bearer {bob.credential}"})
            assert (kicked.status_code, kicked.json()["code"]) == (403, "not_host")
            await alice.kick_watcher(scope, robin_id)
            ended = await asyncio.wait_for(events.unsubscribed.get(), 5)
            assert subscription.state == centrifuge.SubscriptionState.UNSUBSCRIBED, ended
            with pytest.raises(LobbyError) as refused:
                await robin.watch(game_key="island-battles", join_code=code)
            assert (refused.value.code, refused.value.status) == ("watcher_kicked", 403)
            assert (await http.get(f"/game/from-lobby/{scope.lobby_id}/watch", headers=await bearer(robin))).status_code == 403
            assert (await alice.snapshot(scope)).watcher_count == 0
            again = await connection_client(robin.credential)
            try:
                await denied_subscription(again, access.channel)
            finally:
                await disconnect(again)

            # Turns go on without the kicked watcher.
            await complete_turn(http, alice, scope.lobby_id)
            await complete_turn(http, bob, scope.lobby_id)
            await asyncio.sleep(1)
            assert events.items.empty()
    finally:
        await disconnect(watcher)
        await disconnect(outsider)
