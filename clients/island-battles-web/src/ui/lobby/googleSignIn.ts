// Sign in with Google (#369): the one module that talks to Google Identity Services. The rest of
// the client sees only `GoogleSignIn`, so it is testable without Google's script. The page never
// reads, stores or shows anything of Google's: the ID token goes straight to the lobby, and only
// Google's own button frame (and its "Signing you in" prompt) may show a Google name or picture.

/** Google Identity Services' script. Loaded only when the lobby offers sign-in, in the browser. */
export const googleScriptUrl = "https://accounts.google.com/gsi/client";

export interface GoogleSignIn {
  /** Draws Google's own button into `parent`. A sign-in with it gives `onCredential` an ID token. */
  renderButton(
    parent: HTMLElement,
    onCredential: (credential: string) => void,
  ): void;
  /**
   * Asks Google for an ID token without a click (`auto_select`), for a person whose session ended.
   * Resolves with undefined when Google gives none: the person signed out, has no single Google
   * session to pick, dismissed Google's prompt, or the browser doesn't allow it.
   */
  silent(): Promise<string | undefined>;
  /** Turns the silent sign-in off until the person signs in with the button again. */
  disableAutoSelect(): void;
}

/** What the lobby's `GET /lobby/v1/sign-in` answers. */
export interface SignInSettings {
  google_client_id: string | null;
}

/**
 * The Google client ID to sign in with, or undefined for today's guest-only name screen: when the
 * lobby has sign-in off, and always in the desktop app, whose web view Google's button doesn't
 * work in.
 */
export function offeredGoogleClientId(
  settings: SignInSettings | undefined,
  desktop: boolean,
): string | undefined {
  if (desktop) return undefined;
  const id = settings?.google_client_id;
  return typeof id === "string" && id !== "" ? id : undefined;
}

// The part of Google Identity Services' `google.accounts.id` this module calls.
interface PromptMoment {
  isSkippedMoment?(): boolean;
  isDismissedMoment?(): boolean;
  getDismissedReason?(): string;
}

interface GoogleAccountsId {
  initialize(config: {
    client_id: string;
    callback(response: { credential?: string }): void;
    auto_select?: boolean;
    cancel_on_tap_outside?: boolean;
    context?: string;
    itp_support?: boolean;
    use_fedcm_for_prompt?: boolean;
  }): void;
  renderButton(parent: HTMLElement, options: Record<string, unknown>): void;
  prompt(listener?: (moment: PromptMoment) => void): void;
  disableAutoSelect(): void;
  cancel(): void;
}

declare global {
  interface Window {
    google?: { accounts?: { id?: GoogleAccountsId } };
  }
}

/** How long the silent sign-in may take before the name screen shows. */
const silentTimeoutMs = 12_000;
/** How long Google's script may take to load before the page goes on without sign-in. */
const loadTimeoutMs = 15_000;

/** Wraps a loaded `google.accounts.id` for one client ID. */
export function wrapGoogleAccounts(
  id: GoogleAccountsId,
  clientId: string,
): GoogleSignIn {
  // Google calls one callback for the button and the silent sign-in alike.
  let onButton: ((credential: string) => void) | undefined;
  let onSilent: ((credential: string | undefined) => void) | undefined;
  id.initialize({
    client_id: clientId,
    callback(response) {
      const credential = response.credential;
      if (!credential) return;
      if (onSilent) onSilent(credential);
      else onButton?.(credential);
    },
    auto_select: true,
    cancel_on_tap_outside: false,
    context: "signin",
    itp_support: true,
    use_fedcm_for_prompt: true,
  });
  return {
    renderButton(parent, onCredential) {
      onButton = onCredential;
      parent.replaceChildren();
      const width = Math.round(
        Math.min(400, Math.max(200, parent.clientWidth || 380)),
      );
      id.renderButton(parent, {
        type: "standard",
        theme: "outline",
        size: "large",
        text: "signin_with",
        shape: "rectangular",
        logo_alignment: "center",
        width,
      });
    },
    silent() {
      return new Promise((resolve) => {
        const timer = setTimeout(() => finish(undefined), silentTimeoutMs);
        const finish = (credential: string | undefined) => {
          if (onSilent !== finish) return;
          clearTimeout(timer);
          onSilent = undefined;
          if (credential === undefined) id.cancel();
          resolve(credential);
        };
        onSilent = finish;
        id.prompt((moment) => {
          if (moment.isSkippedMoment?.()) finish(undefined);
          else if (
            moment.isDismissedMoment?.() &&
            moment.getDismissedReason?.() !== "credential_returned"
          ) {
            finish(undefined);
          }
        });
      });
    },
    disableAutoSelect() {
      id.disableAutoSelect();
    },
  };
}

/**
 * Loads Google's script once and initializes it with the lobby's client ID. Resolves with
 * undefined when the script doesn't load (blocked, offline): the name screen then offers only
 * the guest name.
 */
export function loadGoogleSignIn(
  clientId: string,
): Promise<GoogleSignIn | undefined> {
  return new Promise((resolve) => {
    const ready = () => {
      const id = window.google?.accounts?.id;
      resolve(id ? wrapGoogleAccounts(id, clientId) : undefined);
    };
    if (window.google?.accounts?.id) {
      ready();
      return;
    }
    const script = document.createElement("script");
    script.src = googleScriptUrl;
    script.async = true;
    const timer = setTimeout(() => resolve(undefined), loadTimeoutMs);
    script.addEventListener("load", () => {
      clearTimeout(timer);
      ready();
    });
    script.addEventListener("error", () => {
      clearTimeout(timer);
      resolve(undefined);
    });
    document.head.append(script);
  });
}
