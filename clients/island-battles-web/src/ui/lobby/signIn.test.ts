import { describe, expect, test } from "bun:test";
import { ApiError } from "../../api/errors";
import { googleSignInStep, refusedGoogleSignIn } from "./signIn";

const problem = (status: number, code?: string, detail?: string) =>
  new ApiError({ kind: "problem", status, code, detail });

describe("signing in with Google", () => {
  test("a first sign-in asks for the display name", () => {
    expect(
      googleSignInStep(problem(422, "display_name_required"), false),
    ).toEqual({ kind: "ask-name" });
  });

  test("a taken name stays on the name step with its sentence under the field", () => {
    expect(googleSignInStep(problem(409, "display_name_taken"), true)).toEqual({
      kind: "name-problem",
      message: "Another account has that display name. Choose another.",
    });
    expect(googleSignInStep(problem(422, "invalid_input"), true)).toEqual({
      kind: "name-problem",
      message: "Use 3 to 24 letters, digits, spaces, - and _.",
    });
  });

  test("a refused or expired Google token goes back to the button", () => {
    expect(googleSignInStep(problem(401, "unauthenticated"), false)).toEqual({
      kind: "button-problem",
      message: "Google's sign-in couldn't be checked. Try again.",
    });
    expect(googleSignInStep(problem(401, "unauthenticated"), true)).toEqual({
      kind: "button-problem",
      message: "Google's sign-in expired. Sign in with Google again.",
    });
  });

  test("sign-in turned off meanwhile, and a lost connection", () => {
    expect(googleSignInStep(problem(404, "not_found"), false)).toMatchObject({
      kind: "button-problem",
      message: "Sign-in with Google is off for now. Play as a guest.",
    });
    const offline = new ApiError({ kind: "transport", status: 0 });
    expect(googleSignInStep(offline, true)).toMatchObject({
      kind: "name-problem",
    });
    expect(googleSignInStep(offline, false).kind).toBe("button-problem");
  });

  test("anything else shows the server's sentence where the person is", () => {
    expect(
      googleSignInStep(problem(503, "unavailable", "try later"), false),
    ).toEqual({ kind: "button-problem", message: "Try later." });
    expect(googleSignInStep(new Error("boom"), true)).toEqual({
      kind: "name-problem",
      message: "Could not sign in. Please try again.",
    });
  });
});

describe("the silent sign-in's refusals (#409)", () => {
  test("a refused token, sign-in off or a gone account stops the browser signing in by itself", () => {
    expect(refusedGoogleSignIn(problem(401, "unauthenticated"))).toBe(true);
    expect(refusedGoogleSignIn(problem(404))).toBe(true);
    expect(refusedGoogleSignIn(problem(422, "display_name_required"))).toBe(
      true,
    );
  });

  test("an unreachable or failing lobby keeps it for the next tab", () => {
    expect(
      refusedGoogleSignIn(new ApiError({ kind: "transport", status: 0 })),
    ).toBe(false);
    expect(refusedGoogleSignIn(problem(503, "unavailable"))).toBe(false);
    expect(refusedGoogleSignIn(new Error("boom"))).toBe(false);
  });
});
