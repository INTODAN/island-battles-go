from http import HTTPStatus
from typing import Any
from urllib.parse import quote
from uuid import UUID

import httpx

from ... import errors
from ...client import AuthenticatedClient, Client
from ...models.error_model import ErrorModel
from ...models.generation_state import GenerationState
from ...types import UNSET, Response, Unset


def _get_kwargs(
    lobby_id: UUID,
    *,
    authorization: str | Unset = UNSET,
) -> dict[str, Any]:
    headers: dict[str, Any] = {}
    if not isinstance(authorization, Unset):
        headers["Authorization"] = authorization

    _kwargs: dict[str, Any] = {
        "method": "get",
        "url": "/game/from-lobby/{lobby_id}/generation".format(
            lobby_id=quote(str(lobby_id), safe=""),
        ),
    }

    _kwargs["headers"] = headers
    return _kwargs


def _parse_response(
    *, client: AuthenticatedClient | Client, response: httpx.Response
) -> ErrorModel | GenerationState | None:
    if response.status_code == 200:
        response_200 = GenerationState.from_dict(response.json())

        return response_200

    if response.status_code == 400:
        response_400 = ErrorModel.from_dict(response.json())

        return response_400

    if response.status_code == 401:
        response_401 = ErrorModel.from_dict(response.json())

        return response_401

    if response.status_code == 403:
        response_403 = ErrorModel.from_dict(response.json())

        return response_403

    if response.status_code == 404:
        response_404 = ErrorModel.from_dict(response.json())

        return response_404

    if response.status_code == 409:
        response_409 = ErrorModel.from_dict(response.json())

        return response_409

    if response.status_code == 422:
        response_422 = ErrorModel.from_dict(response.json())

        return response_422

    if response.status_code == 500:
        response_500 = ErrorModel.from_dict(response.json())

        return response_500

    if response.status_code == 503:
        response_503 = ErrorModel.from_dict(response.json())

        return response_503

    if client.raise_on_unexpected_status:
        raise errors.UnexpectedStatus(response.status_code, response.content)
    else:
        return None


def _build_response(
    *, client: AuthenticatedClient | Client, response: httpx.Response
) -> Response[ErrorModel | GenerationState]:
    return Response(
        status_code=HTTPStatus(response.status_code),
        content=response.content,
        headers=response.headers,
        parsed=_parse_response(client=client, response=response),
    )


def sync_detailed(
    lobby_id: UUID,
    *,
    client: AuthenticatedClient,
    authorization: str | Unset = UNSET,
) -> Response[ErrorModel | GenerationState]:
    """Read the current generation for a roster player

     Requires a current authorized lobby snapshot and frozen roster membership. Returns persistent castle
    IDs, troop state, recorded clashes with per-participant before/after counts, and your turn
    completion flag.

    Args:
        lobby_id (UUID):
        authorization (str | Unset):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[ErrorModel | GenerationState]
    """

    kwargs = _get_kwargs(
        lobby_id=lobby_id,
        authorization=authorization,
    )

    response = client.get_httpx_client().request(
        **kwargs,
    )

    return _build_response(client=client, response=response)


def sync(
    lobby_id: UUID,
    *,
    client: AuthenticatedClient,
    authorization: str | Unset = UNSET,
) -> ErrorModel | GenerationState | None:
    """Read the current generation for a roster player

     Requires a current authorized lobby snapshot and frozen roster membership. Returns persistent castle
    IDs, troop state, recorded clashes with per-participant before/after counts, and your turn
    completion flag.

    Args:
        lobby_id (UUID):
        authorization (str | Unset):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        ErrorModel | GenerationState
    """

    return sync_detailed(
        lobby_id=lobby_id,
        client=client,
        authorization=authorization,
    ).parsed


async def asyncio_detailed(
    lobby_id: UUID,
    *,
    client: AuthenticatedClient,
    authorization: str | Unset = UNSET,
) -> Response[ErrorModel | GenerationState]:
    """Read the current generation for a roster player

     Requires a current authorized lobby snapshot and frozen roster membership. Returns persistent castle
    IDs, troop state, recorded clashes with per-participant before/after counts, and your turn
    completion flag.

    Args:
        lobby_id (UUID):
        authorization (str | Unset):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[ErrorModel | GenerationState]
    """

    kwargs = _get_kwargs(
        lobby_id=lobby_id,
        authorization=authorization,
    )

    response = await client.get_async_httpx_client().request(**kwargs)

    return _build_response(client=client, response=response)


async def asyncio(
    lobby_id: UUID,
    *,
    client: AuthenticatedClient,
    authorization: str | Unset = UNSET,
) -> ErrorModel | GenerationState | None:
    """Read the current generation for a roster player

     Requires a current authorized lobby snapshot and frozen roster membership. Returns persistent castle
    IDs, troop state, recorded clashes with per-participant before/after counts, and your turn
    completion flag.

    Args:
        lobby_id (UUID):
        authorization (str | Unset):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        ErrorModel | GenerationState
    """

    return (
        await asyncio_detailed(
            lobby_id=lobby_id,
            client=client,
            authorization=authorization,
        )
    ).parsed
