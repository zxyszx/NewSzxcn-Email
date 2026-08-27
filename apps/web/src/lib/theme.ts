export function getInitialTheme() {
  return localStorage.getItem("lanqin:theme") === "dark" || document.documentElement.classList.contains("dark")
}

export function applyTheme(dark: boolean, _animated = false) {
  const root = document.documentElement
  root.classList.toggle("dark", dark)
  root.style.colorScheme = dark ? "dark" : "light"
  localStorage.setItem("lanqin:theme", dark ? "dark" : "light")
}
