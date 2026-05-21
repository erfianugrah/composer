import type { KeyboardEvent, MouseEvent } from "react";

/**
 * Browser-native row navigation that respects modifier keys.
 *
 * Returns props to spread on a `<TR>` that should behave like a link to `href`:
 *  - Plain click navigates via `window.location.href` (same tab).
 *  - Cmd/Ctrl-click and middle-click open in a new tab.
 *  - Shift-click opens in a new window.
 *  - Enter / Space when focused activates the row.
 *  - The row is keyboard-focusable with `role="link"` for AT users.
 *
 * Use this for rows whose primary purpose is to navigate. For rows that toggle
 * local state (e.g. expand-inspect, select-for-history), use `clickableRow()`
 * instead — it adds the same keyboard support without the new-tab semantics.
 */
export function navigableRow(href: string) {
  return {
    role: "link" as const,
    tabIndex: 0,
    "aria-label": `Navigate to ${href}`,
    onClick: (e: MouseEvent<HTMLTableRowElement>) => {
      // Modifier-click → new tab/window (browser-native behavior).
      if (e.metaKey || e.ctrlKey) {
        window.open(href, "_blank", "noopener");
        return;
      }
      if (e.shiftKey) {
        window.open(href, "_blank");
        return;
      }
      window.location.href = href;
    },
    onAuxClick: (e: MouseEvent<HTMLTableRowElement>) => {
      // Middle-click → new tab. `onClick` doesn't fire for button 1.
      if (e.button === 1) {
        e.preventDefault();
        window.open(href, "_blank", "noopener");
      }
    },
    onKeyDown: (e: KeyboardEvent<HTMLTableRowElement>) => {
      if (e.key === "Enter") {
        e.preventDefault();
        window.location.href = href;
      } else if (e.key === " ") {
        // Space → preventDefault stops page scroll
        e.preventDefault();
        window.location.href = href;
      }
    },
  };
}

/**
 * Keyboard support for rows that trigger a local action (not navigation).
 *
 * Use for rows that toggle expand state, select for detail panes, etc.
 * `onActivate` runs on click / Enter / Space.
 */
export function clickableRow(onActivate: () => void, ariaLabel?: string) {
  return {
    role: "button" as const,
    tabIndex: 0,
    ...(ariaLabel ? { "aria-label": ariaLabel } : {}),
    onClick: onActivate,
    onKeyDown: (e: KeyboardEvent<HTMLTableRowElement>) => {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        onActivate();
      }
    },
  };
}
