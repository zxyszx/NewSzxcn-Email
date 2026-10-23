const allowedMailLinkProtocols = new Set(["http:", "https:", "mailto:", "tel:"])

export function normalizeMailLinkHref(value: string, pageProtocol = window.location.protocol) {
  const raw = value.trim()
  if (!raw || raw.startsWith("#")) return ""

  const candidate = raw.startsWith("//") ? `${pageProtocol}${raw}` : raw
  if (!/^[a-z][a-z\d+.-]*:/i.test(candidate)) return ""

  try {
    const url = new URL(candidate)
    return allowedMailLinkProtocols.has(url.protocol.toLowerCase()) ? url.href : ""
  } catch {
    return ""
  }
}

export function navigateToMailLink(href: string) {
  const link = document.createElement("a")
  link.href = href
  link.target = "_self"
  link.rel = "noopener noreferrer"
  link.hidden = true
  document.body.appendChild(link)
  try {
    link.click()
  } finally {
    link.remove()
  }
}

export function bindMailFrameLinks(doc: Document, navigate: (href: string) => void = navigateToMailLink) {
  doc.querySelectorAll<HTMLAnchorElement>("a[href]").forEach((link) => {
    link.target = "_self"
    link.rel = "noopener noreferrer"
    link.removeAttribute("download")
  })

  const handleClick = (event: MouseEvent) => {
    if (event.defaultPrevented || event.button !== 0) return
    const target = event.target as Node | null
    const element = target?.nodeType === 1 ? target as Element : target?.parentElement
    const link = element?.closest<HTMLAnchorElement>("a[href]")
    if (!link) return

    const rawHref = link.getAttribute("href") || ""
    if (rawHref.trim().startsWith("#")) return

    event.preventDefault()
    const href = normalizeMailLinkHref(rawHref)
    if (href) navigate(href)
  }

  doc.addEventListener("click", handleClick)
  return () => doc.removeEventListener("click", handleClick)
}
