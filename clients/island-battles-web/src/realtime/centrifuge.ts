import { Centrifuge } from "centrifuge";

/** What the module needs from a Centrifugo subscription. The real one fits; tests fake it. */
export interface SubscriptionLike {
  on(
    event: "subscribed" | "unsubscribed" | "error",
    listener: (context: unknown) => void,
  ): unknown;
  on(
    event: "publication",
    listener: (context: { data?: unknown }) => void,
  ): unknown;
  subscribe(): void;
  unsubscribe(): void;
}

/** What the module needs from a Centrifugo client. */
export interface CentrifugeLike {
  on(
    event: "connecting" | "connected" | "disconnected" | "error",
    listener: (context: unknown) => void,
  ): unknown;
  newSubscription(
    channel: string,
    options: { recoverable: boolean; positioned: boolean },
  ): SubscriptionLike;
  removeSubscription(subscription: SubscriptionLike): void;
  connect(): void;
  disconnect(): void;
}

export interface CentrifugeOptions {
  token: string;
  getToken: () => Promise<string>;
}

export type CreateCentrifuge = (
  url: string,
  options: CentrifugeOptions,
) => CentrifugeLike;

export const createCentrifuge: CreateCentrifuge = (url, options) =>
  new Centrifuge(url, options) as unknown as CentrifugeLike;

export { UnauthorizedError } from "centrifuge";
