import { useLayoutEffect, useState } from "react";

export type Theme = "system" | "light" | "dark";
const key = "codexm.theme";

export function useTheme() {
  const [theme, setTheme] = useState<Theme>(() => {
    try {
      const saved = localStorage.getItem(key);
      if (saved === "light" || saved === "dark") return saved;
    } catch { /* System preference still applies without browser storage. */ }
    return "system";
  });
  useLayoutEffect(() => {
    const system = matchMedia("(prefers-color-scheme: dark)");
    const apply = () => {
      const resolved = theme === "system" ? (system.matches ? "dark" : "light") : theme;
      document.documentElement.dataset.theme = resolved;
      document.querySelector('meta[name="theme-color"]')?.setAttribute("content", resolved === "dark" ? "#171b19" : "#ffffff");
    };
    apply();
    try { localStorage.setItem(key, theme); } catch { /* The current page can still change appearance. */ }
    system.addEventListener("change", apply);
    return () => system.removeEventListener("change", apply);
  }, [theme]);
  return [theme, setTheme] as const;
}
