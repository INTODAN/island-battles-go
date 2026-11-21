import { describe, expect, test } from "bun:test";
import { offeredGoogleClientId, wrapGoogleAccounts } from "./googleSignIn";

type Wrapped = Parameters<typeof wrapGoogleAccounts>[0];
type Config = Parameters<Wrapped["initialize"]>[0];
type Moment = Parameters<NonNullable<Parameters<Wrapped["prompt"]>[0]>>[0];

/** A stand-in for Google's `google.accounts.id`, which tests drive by hand. */
function fakeGoogle() {
  const calls: string[] = [];
  let config: Config | undefined;
  let moment: ((moment: Moment) => void) | undefined;
  const id: Wrapped = {
    initialize(next) {
      config = next;
      calls.push("initialize");
    },
    renderButton(_parent, options) {
      calls.push(`renderButton ${options.width}`);
    },
    prompt(listener) {
      moment = listener;
      calls.push("prompt");
    },
    disableAutoSelect() {
      calls.push("disableAutoSelect");
    },
    cancel() {
      calls.push("cancel");
    },
  };
  return {
    id,
    calls,
    config: () => config,
    /** Google's callback with an ID token, from the button or the silent sign-in. */
    credential(credential: string) {
      config?.callback({ credential });
    },
    moment(next: Moment) {
      moment?.(next);
    },
  };
}

const parent = () =>
  ({ clientWidth: 378, replaceChildren() {} }) as unknown as HTMLElement;

describe("offeredGoogleClientId", () => {
  test("the lobby's client ID in the browser; nothing with sign-in off or in the desktop app", () => {
    expect(offeredGoogleClientId({ google_client_id: "abc" }, false)).toBe(
      "abc",
    );
    expect(
      offeredGoogleClientId({ google_client_id: null }, false),
    ).toBeUndefined();
    expect(
      offeredGoogleClientId({ google_client_id: "" }, false),
    ).toBeUndefined();
    expect(
      offeredGoogleClientId({ google_client_id: "abc" }, true),
    ).toBeUndefined();
    expect(offeredGoogleClientId(undefined, false)).toBeUndefined();
  });
});

describe("Google Identity Services", () => {
  test("initializes once with the client ID and the silent sign-in on", () => {
    const google = fakeGoogle();
    wrapGoogleAccounts(google.id, "client-1");
    expect(google.config()).toMatchObject({
      client_id: "client-1",
      auto_select: true,
    });
    expect(google.calls).toEqual(["initialize"]);
  });

  test("the button's credential goes to the name screen", () => {
    const google = fakeGoogle();
    const signIn = wrapGoogleAccounts(google.id, "client-1");
    const seen: string[] = [];
    signIn.renderButton(parent(), (credential) => seen.push(credential));
    google.credential("id-token");
    expect(seen).toEqual(["id-token"]);
    expect(google.calls).toEqual(["initialize", "renderButton 378"]);
  });

  test("the silent sign-in resolves with Google's credential, not the button's handler", async () => {
    const google = fakeGoogle();
    const signIn = wrapGoogleAccounts(google.id, "client-1");
    const seen: string[] = [];
    signIn.renderButton(parent(), (credential) => seen.push(credential));
    const silent = signIn.silent();
    google.moment({
      isDismissedMoment: () => true,
      getDismissedReason: () => "credential_returned",
    });
    google.credential("id-token");
    expect(await silent).toBe("id-token");
    expect(seen).toEqual([]);
  });

  test("a skipped or dismissed prompt resolves with nothing", async () => {
    const google = fakeGoogle();
    const signIn = wrapGoogleAccounts(google.id, "client-1");
    const skipped = signIn.silent();
    google.moment({ isSkippedMoment: () => true });
    expect(await skipped).toBeUndefined();
    const dismissed = signIn.silent();
    google.moment({
      isSkippedMoment: () => false,
      isDismissedMoment: () => true,
      getDismissedReason: () => "cancel_called",
    });
    expect(await dismissed).toBeUndefined();
    // A credential after that goes to the button's handler again.
    const seen: string[] = [];
    signIn.renderButton(parent(), (credential) => seen.push(credential));
    google.credential("late");
    expect(seen).toEqual(["late"]);
  });

  test("sign out turns the silent sign-in off", () => {
    const google = fakeGoogle();
    wrapGoogleAccounts(google.id, "client-1").disableAutoSelect();
    expect(google.calls).toContain("disableAutoSelect");
  });
});
