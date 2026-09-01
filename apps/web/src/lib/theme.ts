const THEME_TRANSITION_MS = 180
const THEME_KEY = "lanqin:theme"
let themeTimer: number | undefined

export type ThemeMode = "light" | "dark" | "system"

export function getThemeMode(): ThemeMode {
  const stored = localStorage.getItem(THEME_KEY)
  return stored === "light" || stored === "dark" || stored === "system" ? stored : "system"
}

export function systemPrefersDark() {
  return window.matchMedia("(prefers-color-scheme: dark)").matches
}

export function resolveThemeMode(mode: ThemeMode) {
  return mode === "system" ? systemPrefersDark() : mode === "dark"
}

export function getInitialTheme() {
  return resolveThemeMode(getThemeMode())
}

export function applyTheme(dark: boolean, animated = false, persist = true) {
  const root = document.documentElement
  const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches
  const updateTheme = () => {
    root.classList.toggle("dark", dark)
    root.style.colorScheme = dark ? "dark" : "light"
    if (persist) localStorage.setItem(THEME_KEY, dark ? "dark" : "light")
  }

  if (!animated || reduceMotion) {
    root.classList.remove("theme-transition")
    root.style.removeProperty("--theme-fade-bg")
    updateTheme()
    return
  }

  if (themeTimer) window.clearTimeout(themeTimer)
  root.style.setProperty("--theme-fade-bg", getComputedStyle(document.body).backgroundColor)
  root.classList.add("theme-transition")
  requestAnimationFrame(() => {
    updateTheme()
    themeTimer = window.setTimeout(() => {
      root.classList.remove("theme-transition")
      root.style.removeProperty("--theme-fade-bg")
      themeTimer = undefined
    }, THEME_TRANSITION_MS)
  })
}

export function applyThemeMode(mode: ThemeMode, animated = false) {
  localStorage.setItem(THEME_KEY, mode)
  applyTheme(resolveThemeMode(mode), animated, false)
}

export function initializeTheme() {
  const mode = getThemeMode()
  applyTheme(resolveThemeMode(mode), false, false)
}

export function subscribeToSystemTheme(onChange: (dark: boolean) => void) {
  const media = window.matchMedia("(prefers-color-scheme: dark)")
  const listener = (event: MediaQueryListEvent) => onChange(event.matches)
  media.addEventListener("change", listener)
  return () => media.removeEventListener("change", listener)
}
