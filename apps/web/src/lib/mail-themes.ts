export type MailTheme = {
  id: string
  name: string
  light: string
  dark: string
  lightFallback: string
  darkFallback: string
  thumbnail: string
  accentColor: string
  backgroundPosition: string
  source: string
  distribution: "bundled" | "development-reference"
}

export const mailThemes: MailTheme[] = [
  {
    id: "arcticsolitude",
    name: "极地独处",
    light: "/themes/arcticsolitude/light.webp",
    dark: "/themes/arcticsolitude/dark.webp",
    lightFallback: "/themes/arcticsolitude/light.jpg",
    darkFallback: "/themes/arcticsolitude/dark.jpg",
    thumbnail: "/themes/arcticsolitude/thumbnail.webp",
    accentColor: "#2f75e8",
    backgroundPosition: "center center",
    source: "Microsoft Outlook Assets.car: arctic_solitude light/dark",
    distribution: "development-reference",
  },
  {
    id: "star-lake",
    name: "星湖",
    light: "/themes/star-lake/light.webp",
    dark: "/themes/star-lake/dark.webp",
    lightFallback: "/themes/star-lake/light.jpg",
    darkFallback: "/themes/star-lake/dark.jpg",
    thumbnail: "/themes/star-lake/thumbnail.webp",
    accentColor: "#1596d4",
    backgroundPosition: "center center",
    source: "Microsoft Outlook Assets.car: alpine_glow light/dark",
    distribution: "development-reference",
  },
  {
    id: "violet-lake",
    name: "紫夜湖畔",
    light: "/themes/violet-lake/light.webp",
    dark: "/themes/violet-lake/dark.webp",
    lightFallback: "/themes/violet-lake/light.jpg",
    darkFallback: "/themes/violet-lake/dark.jpg",
    thumbnail: "/themes/violet-lake/thumbnail.webp",
    accentColor: "#b35c8d",
    backgroundPosition: "center center",
    source: "Microsoft Outlook Assets.car: ruby_hills light/dark",
    distribution: "development-reference",
  },
  {
    id: "chroma-stone",
    name: "彩岩",
    light: "/themes/chroma-stone/light.webp",
    dark: "/themes/chroma-stone/dark.webp",
    lightFallback: "/themes/chroma-stone/light.jpg",
    darkFallback: "/themes/chroma-stone/dark.jpg",
    thumbnail: "/themes/chroma-stone/thumbnail.webp",
    accentColor: "#f04478",
    backgroundPosition: "70% center",
    source: "Microsoft Outlook Assets.car: future_plus light/dark",
    distribution: "development-reference",
  },
  {
    id: "neon-crossing",
    name: "霓虹海径",
    light: "/themes/neon-crossing/light.webp",
    dark: "/themes/neon-crossing/dark.webp",
    lightFallback: "/themes/neon-crossing/light.jpg",
    darkFallback: "/themes/neon-crossing/dark.jpg",
    thumbnail: "/themes/neon-crossing/thumbnail.webp",
    accentColor: "#e05c69",
    backgroundPosition: "center center",
    source: "Microsoft Outlook Assets.car: summer_summit light/dark",
    distribution: "development-reference",
  },
]

export const defaultMailTheme = mailThemes[0]
