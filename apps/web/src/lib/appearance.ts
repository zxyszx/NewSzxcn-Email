import { defaultMailTheme, mailThemes } from "@/lib/mail-themes"

const WALLPAPER_KEY = "lanqin:mail-wallpaper"
const ACCENT_KEY = "lanqin:mail-accent"
const BLUR_KEY = "newszxcn-mail-glass-blur-v1"

export type AppearanceSettings = {
  wallpaperId: string
  accentColor: string
  glassBlur: number
}

export function getAppearanceSettings(): AppearanceSettings {
  const wallpaperId = localStorage.getItem(WALLPAPER_KEY) || defaultMailTheme.id
  const accentColor = localStorage.getItem(ACCENT_KEY) || defaultMailTheme.accentColor
  const storedBlur = Number(localStorage.getItem(BLUR_KEY))
  const glassBlur = Number.isFinite(storedBlur) && storedBlur >= 4 && storedBlur <= 28 ? storedBlur : 14
  return { wallpaperId, accentColor, glassBlur }
}

export function applyAppearanceSettings(settings = getAppearanceSettings()) {
  const theme = mailThemes.find((item) => item.id === settings.wallpaperId) || defaultMailTheme
  const usesDarkerPaneGlass = theme.id === "violet-lake"
  const root = document.documentElement
  root.style.setProperty("--workspace-wallpaper-light", `url("${theme.light}")`)
  root.style.setProperty("--workspace-wallpaper-dark", `url("${theme.dark}")`)
  root.style.setProperty("--workspace-wallpaper-position", theme.backgroundPosition)
  root.style.setProperty("--workspace-glass-blur", `${settings.glassBlur}px`)
  root.style.setProperty("--mail-glass-blur", `${settings.glassBlur}px`)
  root.style.setProperty("--appearance-accent", settings.accentColor)
  root.style.setProperty("--mail-accent-color", settings.accentColor)
  root.style.setProperty("--profile-glass-folder-light", usesDarkerPaneGlass ? "rgba(244, 249, 253, 0.82)" : "rgba(244, 249, 253, 0.78)")
  root.style.setProperty("--profile-glass-list-light", usesDarkerPaneGlass ? "rgba(246, 250, 253, 0.92)" : "rgba(246, 250, 253, 0.9)")
  root.style.setProperty("--profile-glass-folder-dark", usesDarkerPaneGlass ? "rgba(15, 19, 27, 0.74)" : "rgba(15, 19, 27, 0.7)")
  root.style.setProperty("--profile-glass-list-dark", usesDarkerPaneGlass ? "rgba(24, 23, 31, 0.88)" : "rgba(24, 23, 31, 0.86)")
}

export function saveAppearanceSettings(settings: AppearanceSettings) {
  localStorage.setItem(WALLPAPER_KEY, settings.wallpaperId)
  localStorage.setItem(ACCENT_KEY, settings.accentColor)
  localStorage.setItem(BLUR_KEY, String(settings.glassBlur))
  applyAppearanceSettings(settings)
}
