import { Centrifuge, UnauthorizedError } from "centrifuge";
import { GameError } from "./errors.js";

export interface RealtimeOptions {
  readonly url: string;
  readonly token: string;
  readonly getToken: () => Promise<string>;
  readonly onPublication: (data: unknown) => void;
  readonly onSubscribed: () => void;
  readonly onReconcile: () => void;
}
/** The game client owns the factory result and closes it even after startup failure. */
export interface Realtime {
  connect(): void | Promise<void>;
  subscribe(channel: string, timeoutMs: number): Promise<void>;
  close(): void | Promise<void>;
}
export type RealtimeFactory = (options: RealtimeOptions) => Realtime;

export const centrifugeFactory: RealtimeFactory = (options) => {
  const sdk = new Centrifuge(options.url, {
    token: options.token,
    getToken: async () => {
      try {
        return await options.getToken();
      } catch (error) {
        if (error instanceof GameError && error.kind === "authentication")
          throw new UnauthorizedError("connection token rejected");
        throw error;
      }
    },
  });
  sdk.on("connected", options.onReconcile);
  sdk.on("disconnected", options.onReconcile);
  let subscription: ReturnType<Centrifuge["newSubscription"]> | undefined;
  return {
    connect() {
      sdk.connect();
    },
    async subscribe(channel, timeoutMs) {
      subscription = sdk.newSubscription(channel, {
        recoverable: true,
        positioned: true,
      });
      subscription.on("publication", (ctx) => options.onPublication(ctx.data));
      subscription.on("subscribed", () => options.onSubscribed());
      subscription.on("unsubscribed", options.onReconcile);
      subscription.subscribe();
      await subscription.ready(timeoutMs);
    },
    close() {
      if (subscription) {
        subscription.unsubscribe();
        sdk.removeSubscription(subscription);
      }
      sdk.disconnect();
    },
  };
};
