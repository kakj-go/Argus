import {
  createContext,
  type ReactNode,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";

export type ThemePreference = "light" | "dark" | "system";
export type ResolvedTheme = "light" | "dark";

const THEME_STORAGE_KEY = "argus.theme";

type ThemeContextValue = {
  preference: ThemePreference;
  resolvedTheme: ResolvedTheme;
  setPreference: (preference: ThemePreference) => void;
  toggleTheme: () => void;
};

const ThemeContext = createContext<ThemeContextValue | null>(null);

function systemTheme(): ResolvedTheme {
  return window.matchMedia?.("(prefers-color-scheme: light)").matches
    ? "light"
    : "dark";
}

function storedPreference(): ThemePreference {
  if (typeof window === "undefined") return "system";
  const value = window.localStorage.getItem(THEME_STORAGE_KEY);
  return value === "light" || value === "dark" || value === "system"
    ? value
    : "system";
}

function resolvePreference(preference: ThemePreference): ResolvedTheme {
  return preference === "system" ? systemTheme() : preference;
}

export function initializeTheme() {
  if (typeof window === "undefined") return;
  const resolved = resolvePreference(storedPreference());
  document.documentElement.dataset.theme = resolved;
  document.documentElement.style.colorScheme = resolved;
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [preference, setPreferenceState] =
    useState<ThemePreference>(storedPreference);
  const [resolvedTheme, setResolvedTheme] = useState<ResolvedTheme>(() =>
    typeof window === "undefined"
      ? "dark"
      : resolvePreference(storedPreference()),
  );

  useEffect(() => {
    const media = window.matchMedia("(prefers-color-scheme: light)");
    const apply = () => {
      const resolved = resolvePreference(preference);
      setResolvedTheme(resolved);
      document.documentElement.dataset.theme = resolved;
      document.documentElement.style.colorScheme = resolved;
    };
    apply();
    media.addEventListener("change", apply);
    return () => media.removeEventListener("change", apply);
  }, [preference]);

  const value = useMemo<ThemeContextValue>(
    () => ({
      preference,
      resolvedTheme,
      setPreference: (next) => {
        window.localStorage.setItem(THEME_STORAGE_KEY, next);
        setPreferenceState(next);
      },
      toggleTheme: () => {
        const next = resolvedTheme === "dark" ? "light" : "dark";
        window.localStorage.setItem(THEME_STORAGE_KEY, next);
        setPreferenceState(next);
      },
    }),
    [preference, resolvedTheme],
  );

  return (
    <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
  );
}

export function useTheme() {
  const value = useContext(ThemeContext);
  if (!value) throw new Error("useTheme must be used inside ThemeProvider");
  return value;
}
