import { defaultMailTheme, mailThemes } from "@/lib/mail-themes"

const WALLPAPER_KEY = "lanqin:mail-wallpaper"
const ACCENT_KEY = "lanqin:mail-accent"
const MAIL_GLASS_KEY = "newszxcn-mail-glass-strength-v2"
const ADMIN_GLASS_KEY = "newszxcn-admin-glass-strength-v2"

export type AppearanceSettings = {
  wallpaperId: string
  accentColor: string
  mailGlassStrength: number
  adminGlassStrength: number
}

function clampPercentage(value: number, fallback: number) {
  return Number.isFinite(value) ? Math.min(100, Math.max(0, Math.round(value))) : fallback
}

function storedPercentage(key: string, fallback: number) {
  const value = localStorage.getItem(key)
  return value === null ? fallback : clampPercentage(Number(value), fallback)
}

function mix(min: number, max: number, strength: number) {
  return min + (max - min) * (strength / 100)
}

function rgba(red: number, green: number, blue: number, alpha: number) {
  return `rgba(${red}, ${green}, ${blue}, ${alpha.toFixed(3)})`
}

function hexToHsl(value: string) {
  const match = /^#([\da-f]{2})([\da-f]{2})([\da-f]{2})$/i.exec(value)
  if (!match) return { hue: 218, saturation: 88, lightness: 57, foreground: "0 0% 100%" }
  const [red, green, blue] = match.slice(1).map((part) => Number.parseInt(part, 16) / 255)
  const max = Math.max(red, green, blue)
  const min = Math.min(red, green, blue)
  const delta = max - min
  let hue = 0
  if (delta) {
    if (max === red) hue = ((green - blue) / delta) % 6
    else if (max === green) hue = (blue - red) / delta + 2
    else hue = (red - green) / delta + 4
    hue = Math.round(hue * 60)
    if (hue < 0) hue += 360
  }
  const lightness = (max + min) / 2
  const saturation = delta ? delta / (1 - Math.abs(2 * lightness - 1)) : 0
  const luminance = [red, green, blue]
    .map((channel) => channel <= 0.03928 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4)
    .reduce((total, channel, index) => total + channel * [0.2126, 0.7152, 0.0722][index], 0)
  return {
    hue,
    saturation: Math.round(saturation * 100),
    lightness: Math.round(lightness * 100),
    foreground: luminance > 0.43 ? "220 43% 12%" : "0 0% 100%",
  }
}

export function getAppearanceSettings(): AppearanceSettings {
  const wallpaperId = localStorage.getItem(WALLPAPER_KEY) || defaultMailTheme.id
  const accentColor = localStorage.getItem(ACCENT_KEY) || defaultMailTheme.accentColor
  const mailGlassStrength = storedPercentage(MAIL_GLASS_KEY, 100)
  const adminGlassStrength = storedPercentage(ADMIN_GLASS_KEY, 100)
  return { wallpaperId, accentColor, mailGlassStrength, adminGlassStrength }
}

export function applyAppearanceSettings(settings = getAppearanceSettings()) {
  const theme = mailThemes.find((item) => item.id === settings.wallpaperId) || defaultMailTheme
  const mailStrength = clampPercentage(settings.mailGlassStrength, 100)
  const adminStrength = clampPercentage(settings.adminGlassStrength, 100)
  const mailBlur = mix(4, 28, mailStrength)
  const adminBlur = mix(4, 28, adminStrength)
  const accent = hexToHsl(settings.accentColor)
  const root = document.documentElement
  root.style.setProperty("--workspace-wallpaper-light", `url("${theme.light}")`)
  root.style.setProperty("--workspace-wallpaper-dark", `url("${theme.dark}")`)
  root.style.setProperty("--workspace-wallpaper-position", theme.backgroundPosition)
  root.style.setProperty("--workspace-glass-blur", `${adminBlur.toFixed(1)}px`)
  root.style.setProperty("--profile-glass-blur", `${adminBlur.toFixed(1)}px`)
  root.style.setProperty("--admin-glass-blur", `${adminBlur.toFixed(1)}px`)
  root.style.setProperty("--mail-glass-blur", `${mailBlur.toFixed(1)}px`)
  root.style.setProperty("--appearance-accent", settings.accentColor)
  root.style.setProperty("--mail-accent-color", settings.accentColor)
  root.style.setProperty("--appearance-primary", `${accent.hue} ${accent.saturation}% ${accent.lightness}%`)
  root.style.setProperty("--appearance-primary-foreground", accent.foreground)
  root.style.setProperty("--primary", `${accent.hue} ${accent.saturation}% ${accent.lightness}%`)
  root.style.setProperty("--primary-foreground", accent.foreground)
  root.style.setProperty("--ring", `${accent.hue} ${accent.saturation}% ${accent.lightness}%`)
  root.style.setProperty("--sidebar-primary", `${accent.hue} ${accent.saturation}% ${accent.lightness}%`)
  root.style.setProperty("--sidebar-ring", `${accent.hue} ${accent.saturation}% ${accent.lightness}%`)
  root.style.setProperty("--compose-send", `${accent.hue} ${accent.saturation}% ${accent.lightness}%`)

  root.style.setProperty("--mail-glass-strong-light", rgba(248, 252, 255, mix(0.78, 0.98, mailStrength)))
  root.style.setProperty("--mail-glass-medium-light", rgba(244, 249, 253, mix(0.72, 0.96, mailStrength)))
  root.style.setProperty("--mail-glass-soft-light", rgba(239, 247, 252, mix(0.65, 0.93, mailStrength)))
  root.style.setProperty("--mail-glass-folder-light", rgba(244, 249, 253, mix(0.66, 0.95, mailStrength)))
  root.style.setProperty("--mail-glass-list-light", rgba(246, 250, 253, mix(0.72, 0.96, mailStrength)))
  root.style.setProperty("--mail-glass-reader-light", rgba(248, 251, 253, mix(0.82, 0.98, mailStrength)))
  root.style.setProperty("--mail-glass-calendar-light", rgba(246, 250, 253, mix(0.76, 0.97, mailStrength)))
  root.style.setProperty("--mail-glass-folder-dark", rgba(15, 19, 27, mix(0.62, 0.94, mailStrength)))
  root.style.setProperty("--mail-glass-list-dark", rgba(24, 23, 31, mix(0.68, 0.95, mailStrength)))
  root.style.setProperty("--mail-glass-reader-dark", rgba(20, 22, 27, mix(0.78, 0.97, mailStrength)))
  root.style.setProperty("--mail-glass-calendar-dark", rgba(24, 27, 33, mix(0.72, 0.95, mailStrength)))
  root.style.setProperty("--mail-glass-strong-dark", rgba(18, 23, 31, mix(0.72, 0.96, mailStrength)))
  root.style.setProperty("--mail-glass-medium-dark", rgba(22, 28, 38, mix(0.68, 0.94, mailStrength)))
  root.style.setProperty("--mail-glass-soft-dark", rgba(27, 34, 45, mix(0.62, 0.91, mailStrength)))

  root.style.setProperty("--profile-glass-folder-light", rgba(244, 249, 253, mix(0.7, 0.95, adminStrength)))
  root.style.setProperty("--profile-glass-list-light", rgba(246, 250, 253, mix(0.74, 0.96, adminStrength)))
  root.style.setProperty("--profile-glass-folder-dark", rgba(15, 19, 27, mix(0.64, 0.94, adminStrength)))
  root.style.setProperty("--profile-glass-list-dark", rgba(24, 23, 31, mix(0.7, 0.95, adminStrength)))
  root.style.setProperty("--admin-glass-sidebar-light", rgba(244, 249, 253, mix(0.7, 0.95, adminStrength)))
  root.style.setProperty("--admin-glass-surface-light", rgba(246, 250, 253, mix(0.74, 0.96, adminStrength)))
  root.style.setProperty("--admin-glass-sidebar-dark", rgba(15, 19, 27, mix(0.64, 0.94, adminStrength)))
  root.style.setProperty("--admin-glass-surface-dark", rgba(24, 23, 31, mix(0.7, 0.95, adminStrength)))
}

export function saveAppearanceSettings(settings: AppearanceSettings) {
  localStorage.setItem(WALLPAPER_KEY, settings.wallpaperId)
  localStorage.setItem(ACCENT_KEY, settings.accentColor)
  localStorage.setItem(MAIL_GLASS_KEY, String(settings.mailGlassStrength))
  localStorage.setItem(ADMIN_GLASS_KEY, String(settings.adminGlassStrength))
  applyAppearanceSettings(settings)
}
