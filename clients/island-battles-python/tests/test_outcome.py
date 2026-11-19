"""The client reads every outcome the server writes, including an abandoned
real-time game and the teams that resigned or left one (#347)."""

from island_battles_client import GameOutcome


def test_the_client_reads_an_abandoned_game_and_its_departures() -> None:
    outcome = GameOutcome.model_validate(
        {
            "reason": "abandoned",
            "winner_team_color": 2,
            "summary": "Blue resigned; the game was abandoned: nobody played for 30 s; Red wins with the most castles",
            "departures": [
                {
                    "team_color": 1,
                    "kind": "resigned",
                    "players": ["Sam"],
                    "turn_number": None,
                },
                {
                    "team_color": 3,
                    "kind": "left",
                    "players": ["Lee"],
                    "turn_number": 12,
                },
            ],
        }
    )
    assert outcome.reason == "abandoned"
    assert outcome.departures is not None
    assert [departure.kind for departure in outcome.departures] == ["resigned", "left"]
    assert outcome.departures[1].turn_number == 12


def test_an_outcome_saved_before_departures_still_reads() -> None:
    outcome = GameOutcome.model_validate(
        {
            "reason": "last_team_standing",
            "winner_team_color": 1,
            "summary": "Blue wins as the last team standing",
        }
    )
    assert not outcome.departures
