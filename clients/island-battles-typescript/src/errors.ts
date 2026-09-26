export type ErrorKind =
  | "authentication"
  | "transport"
  | "protocol"
  | "closed"
  | "http"
  | "validation";

export class GameError extends Error {
  constructor(
    message: string,
    public readonly kind: ErrorKind = "http",
    public readonly status?: number,
    public readonly networkFailure = false,
  ) {
    super(message);
    this.name = "GameError";
  }
}

export function protocol(message: string): GameError {
  return new GameError(message, "protocol");
}
