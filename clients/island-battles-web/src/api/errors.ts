export type ApiErrorKind =
  /** The server answered with an `application/problem+json` error body. */
  | "problem"
  /** The server answered with an error status and a body that is not a problem document. */
  | "http"
  /** No response arrived: DNS, connection, offline, CORS and similar. */
  | "transport";

export interface ApiErrorInit {
  kind: ApiErrorKind;
  /** HTTP status; 0 for transport failures. */
  status: number;
  /** Lobby error code such as `lobby_full`, from the problem's `code` field. */
  code?: string | undefined;
  /** Game problem type such as `urn:island-battles:problem:game-ended`, from the problem's `type` field. */
  problemType?: string | undefined;
  title?: string | undefined;
  detail?: string | undefined;
  cause?: unknown;
}

/** The one error every failed API call rejects with. */
export class ApiError extends Error {
  readonly kind: ApiErrorKind;
  readonly status: number;
  readonly code: string | undefined;
  readonly problemType: string | undefined;
  readonly title: string | undefined;
  readonly detail: string | undefined;

  constructor(init: ApiErrorInit) {
    const summary = init.detail ?? init.title ?? `HTTP ${init.status}`;
    super(
      init.kind === "transport" ? `Network error: ${summary}` : summary,
      init.cause === undefined ? undefined : { cause: init.cause },
    );
    this.name = "ApiError";
    this.kind = init.kind;
    this.status = init.status;
    this.code = init.code;
    this.problemType = init.problemType;
    this.title = init.title;
    this.detail = init.detail;
  }
}

const text = (value: unknown): string | undefined =>
  typeof value === "string" && value !== "" ? value : undefined;

/**
 * Builds an ApiError from a failed response. Reads the body as JSON and takes
 * whatever problem fields are present; unknown fields (such as `$schema`) are
 * ignored, and a body that is not JSON still yields an error with the status.
 */
export async function errorFromResponse(response: Response): Promise<ApiError> {
  let body: unknown;
  try {
    body = JSON.parse(await response.text());
  } catch {
    body = undefined;
  }
  if (typeof body === "object" && body !== null && !Array.isArray(body)) {
    const problem = body as Record<string, unknown>;
    const type = text(problem.type);
    return new ApiError({
      kind: "problem",
      status: response.status,
      code: text(problem.code),
      problemType: type === "about:blank" ? undefined : type,
      title: text(problem.title) ?? (response.statusText || undefined),
      detail: text(problem.detail),
    });
  }
  return new ApiError({
    kind: "http",
    status: response.status,
    title: response.statusText || undefined,
  });
}
