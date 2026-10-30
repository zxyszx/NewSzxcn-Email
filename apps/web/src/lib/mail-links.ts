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
  href = normalizeMailLinkHref(href)
  if (!href) return
  const link = document.createElement("a")
  link.href = href
  link.target = "_blank"
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
  // Linkify text nodes only: never replace existing anchor labels or attributes.
  const walker = doc.createTreeWalker(doc.body, 4)
  const nodes: Text[] = []
  while (walker.nextNode()) {
    const node = walker.currentNode as Text
    if (!node.parentElement?.closest("a, script, style, textarea, code")) nodes.push(node)
  }
  for (const node of nodes) {
    const text = node.data
    const pattern = /https?:\/\/[^\s<>"\u3000]+/gi
    const fragment = doc.createDocumentFragment()
    let end = 0
    for (const match of text.matchAll(pattern)) {
      const value = match[0].replace(/[.,;!?，。；！？、）》」』]+$/u, "")
      const href = normalizeMailLinkHref(value)
      if (!href) continue
      fragment.append(doc.createTextNode(text.slice(end, match.index)))
      const link = doc.createElement("a")
      link.href = href
      link.textContent = value
      fragment.append(link)
      end = match.index! + value.length
    }
    if (end) {
      fragment.append(doc.createTextNode(text.slice(end)))
      node.replaceWith(fragment)
    }
  }
  doc.querySelectorAll<HTMLAnchorElement>("a[href]").forEach((link) => {
    link.target = "_blank"
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
