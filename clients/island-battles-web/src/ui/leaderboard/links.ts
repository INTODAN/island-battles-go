import { h } from "../dom";
import { type LeaderboardRoute, leaderboardHref } from "./model";
import "./leaderboard.css";

type Child = Node | string;

/**
 * A real link to a leaderboard route, so it can be shared or opened in a new
 * tab. A plain click shows the route in this page with `navigate` instead.
 */
export function routeLink(
  route: LeaderboardRoute,
  navigate: (route: LeaderboardRoute) => void,
  attributes: { class?: string } = {},
  ...children: Child[]
): HTMLAnchorElement {
  const link = h(
    "a",
    { href: leaderboardHref(route), ...attributes },
    ...children,
  );
  link.addEventListener("click", (event) => {
    if (
      event.defaultPrevented ||
      event.button !== 0 ||
      event.metaKey ||
      event.ctrlKey ||
      event.shiftKey ||
      event.altKey
    ) {
      return;
    }
    event.preventDefault();
    navigate(route);
  });
  return link;
}

/** The **Leaderboard** link of the name screen and the menu. */
export function leaderboardLink(text: string, open: () => void): HTMLElement {
  return routeLink({ view: "board" }, open, { class: "link" }, text);
}
