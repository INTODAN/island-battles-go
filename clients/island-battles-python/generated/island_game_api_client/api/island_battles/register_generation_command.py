from http import HTTPStatus
from typing import Any

import httpx

from ... import errors
from ...client import AuthenticatedClient, Client
from ...models.error_model import ErrorModel
from ...models.generation_state import GenerationState
from ...models.register_generation_commands import RegisterGenerationCommands
from ...types import UNSET, Response, Unset


def _get_kwargs(
    *,
    body: RegisterGenerationCommands,
    authorization: str | Unset = UNSET,
) -> dict[str, Any]:
    headers: dict[str, Any] = {}
    if not isinstance(authorization, Unset):
        headers["Authorization"] = authorization

    _kwargs: dict[str, Any] = {
        "method": "post",
        "url": "/generation/commands",
    }

    _kwargs["json"] = body.to_dict()

    headers["Content-Type"] = "application/json"

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
    *,
    client: AuthenticatedClient,
    body: RegisterGenerationCommands,
    authorization: str | Unset = UNSET,
) -> Response[ErrorModel | GenerationState]:
    """Register an atomic player command batch and optionally complete the turn

     Only owned send_soldier_group commands are accepted. Command parameters require nonnegative integer
    castle IDs and a positive integer soldier amount; unknown commands and malformed payloads fail
    schema validation with 422. Complete defaults to true; an empty batch can complete a turn. The last
    frozen roster completion advances exactly once. Retry the same request UUID and payload safely;
    changed payloads and stale new requests conflict.

    Args:
        authorization (str | Unset):
        body (RegisterGenerationCommands):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[ErrorModel | GenerationState]
    """

    kwargs = _get_kwargs(
        body=body,
        authorization=authorization,
    )

    response = client.get_httpx_client().request(
        **kwargs,
    )

    return _build_response(client=client, response=response)


def sync(
    *,
    client: AuthenticatedClient,
    body: RegisterGenerationCommands,
    authorization: str | Unset = UNSET,
) -> ErrorModel | GenerationState | None:
    """Register an atomic player command batch and optionally complete the turn

     Only owned send_soldier_group commands are accepted. Command parameters require nonnegative integer
    castle IDs and a positive integer soldier amount; unknown commands and malformed payloads fail
    schema validation with 422. Complete defaults to true; an empty batch can complete a turn. The last
    frozen roster completion advances exactly once. Retry the same request UUID and payload safely;
    changed payloads and stale new requests conflict.

    Args:
        authorization (str | Unset):
        body (RegisterGenerationCommands):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        ErrorModel | GenerationState
    """

    return sync_detailed(
        client=client,
        body=body,
        authorization=authorization,
    ).parsed


async def asyncio_detailed(
    *,
    client: AuthenticatedClient,
    body: RegisterGenerationCommands,
    authorization: str | Unset = UNSET,
) -> Response[ErrorModel | GenerationState]:
    """Register an atomic player command batch and optionally complete the turn

     Only owned send_soldier_group commands are accepted. Command parameters require nonnegative integer
    castle IDs and a positive integer soldier amount; unknown commands and malformed payloads fail
    schema validation with 422. Complete defaults to true; an empty batch can complete a turn. The last
    frozen roster completion advances exactly once. Retry the same request UUID and payload safely;
    changed payloads and stale new requests conflict.

    Args:
        authorization (str | Unset):
        body (RegisterGenerationCommands):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[ErrorModel | GenerationState]
    """

    kwargs = _get_kwargs(
        body=body,
        authorization=authorization,
    )

    response = await client.get_async_httpx_client().request(**kwargs)

    return _build_response(client=client, response=response)


async def asyncio(
    *,
    client: AuthenticatedClient,
    body: RegisterGenerationCommands,
    authorization: str | Unset = UNSET,
) -> ErrorModel | GenerationState | None:
    """Register an atomic player command batch and optionally complete the turn

     Only owned send_soldier_group commands are accepted. Command parameters require nonnegative integer
    castle IDs and a positive integer soldier amount; unknown commands and malformed payloads fail
    schema validation with 422. Complete defaults to true; an empty batch can complete a turn. The last
    frozen roster completion advances exactly once. Retry the same request UUID and payload safely;
    changed payloads and stale new requests conflict.

    Args:
        authorization (str | Unset):
        body (RegisterGenerationCommands):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        ErrorModel | GenerationState
    """

    return (
        await asyncio_detailed(
            client=client,
            body=body,
            authorization=authorization,
        )
    ).parsed
