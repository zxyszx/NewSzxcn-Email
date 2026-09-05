export const isDemoMode = import.meta.env.VITE_DEMO_MODE === "true"

export function publicAsset(path: string) {
  return `${import.meta.env.BASE_URL}${path.replace(/^\//, "")}`
}
