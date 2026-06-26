import type { ITheme } from "@xterm/xterm";

/** Catppuccin Mocha-inspired terminal theme. Shared across all terminal instances. */
export const TERMINAL_THEME: ITheme = {
  background: "#1d1f28",
  foreground: "#e0e0e0",
  cursor: "#c574dd",
  selectionBackground: "#c574dd40",
  black: "#15161e",
  red: "#f37e96",
  green: "#5adecd",
  yellow: "#ffd866",
  blue: "#8796f4",
  magenta: "#c574dd",
  cyan: "#79e6f3",
  white: "#e0e0e0",
  brightBlack: "#414457",
  brightRed: "#ff4870",
  brightGreen: "#17e2c7",
  brightYellow: "#ffd866",
  brightBlue: "#546eff",
  brightMagenta: "#af43d1",
  brightCyan: "#3edced",
  brightWhite: "#fcfcfc",
};

/** Available shells for the shell selector. */
export const ALLOWED_SHELLS = ["/bin/sh", "/bin/bash", "/bin/ash", "/bin/zsh"] as const;
export type ShellOption = (typeof ALLOWED_SHELLS)[number];
