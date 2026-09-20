import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import DOMPurify from "dompurify"
import { ArrowLeft, Download, FileText, Inbox, Mail, Paperclip, RefreshCcw, ShieldCheck } from "lucide-react"
import { sharedInboxApi, type SharedInboxMessage } from "@/lib/api"
import { cn, formatBytes } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"

const tokenStorageKey = "newszxcn:shared-inbox-token"

function consumeShareToken() {
  const fragment = window.location.hash.slice(1).trim()
  if (fragment.startsWith("nis_")) {
    sessionStorage.setItem(tokenStorageKey, fragment)
    window.history.replaceState(null, "", window.location.pathname + window.location.search)
    return fragment
  }
  return sessionStorage.getItem(tokenStorageKey) || ""
}

function shareWindowLabel(minutes: number) {
  if (minutes === 0) return "全部邮件"
  if (minutes < 60) return `最近 ${minutes} 分钟`
  if (minutes < 1440) return `最近 ${minutes / 60} 小时`
  return `最近 ${minutes / 1440} 天`
}

function messageTime(value: string) {
  const date = new Date(value)
  const today = new Date()
  if (date.toDateString() === today.toDateString()) return date.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", hour12: false })
  return date.toLocaleDateString("zh-CN", { year: "numeric", month: "numeric", day: "numeric" })
}

function protectedEmailHTML(html: string) {
  const sanitized = DOMPurify.sanitize(html, {
    ADD_ATTR: ["style", "align", "valign", "bgcolor", "border", "cellpadding", "cellspacing", "width", "height"],
    ADD_TAGS: ["style", "center", "font"],
  })
  const document = new DOMParser().parseFromString(sanitized, "text/html")
  document.querySelectorAll("img").forEach((img) => {
    if (/^data:image\/(?:png|jpeg|gif|webp);base64,/i.test(img.getAttribute("src") || "")) return
    const label = img.getAttribute("alt")?.trim()
    img.replaceWith(document.createTextNode(label || ""))
  })
  const emailStyles = Array.from(document.head.querySelectorAll("style"), (style) => style.outerHTML).join("")
  return `<!doctype html><html><head>
    <meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
    <meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data:; style-src 'unsafe-inline'; font-src data:; base-uri 'none'; form-action 'none'">
    <style>html,body{margin:0;background:#fff;color:#111827}body{box-sizing:border-box;padding:20px;font:14px/1.6 Arial,Helvetica,sans-serif;overflow-wrap:anywhere;-webkit-text-size-adjust:100%}*,*::before,*::after{box-sizing:border-box}img,table{max-width:100%}img{height:auto}pre{white-space:pre-wrap;word-break:break-word}a{color:#2563eb}@media(max-width:600px){body{padding:16px}}</style>${emailStyles}
    </head><body>${document.body.innerHTML}</body></html>`
}

function SharedMessageBody({ html, text }: { html?: string; text?: string }) {
  const frameRef = React.useRef<HTMLIFrameElement>(null)
  const [height, setHeight] = React.useState(320)
  const srcDoc = React.useMemo(() => protectedEmailHTML(html || ""), [html])

  React.useEffect(() => {
    if (!html) return
    const frame = frameRef.current
    if (!frame) return
    let observer: ResizeObserver | undefined
    const resize = () => {
      const doc = frame.contentDocument
      if (!doc) return
      setHeight(Math.max(160, Math.ceil(Math.max(doc.body.scrollHeight, doc.documentElement.scrollHeight))))
    }
    const attach = () => {
      resize()
      if ("ResizeObserver" in window && frame.contentDocument) {
        observer = new ResizeObserver(resize)
        observer.observe(frame.contentDocument.documentElement)
        observer.observe(frame.contentDocument.body)
      }
      frame.contentDocument?.querySelectorAll("img").forEach((img) => img.addEventListener("load", resize, { once: true }))
    }
    frame.addEventListener("load", attach)
    const timer = window.setTimeout(resize, 100)
    return () => { frame.removeEventListener("load", attach); observer?.disconnect(); window.clearTimeout(timer) }
  }, [html, srcDoc])

  return html
    ? <iframe ref={frameRef} title="邮件正文" sandbox="allow-same-origin" referrerPolicy="no-referrer" className="block w-full border-0 bg-white" srcDoc={srcDoc} style={{ height }} />
    : <pre className="whitespace-pre-wrap break-words p-4 font-sans text-sm leading-7 sm:p-5">{text}</pre>
}

export function SharedInboxPage() {
  const [token] = React.useState(consumeShareToken)
  const [selectedID, setSelectedID] = React.useState("")
  const [extraItems, setExtraItems] = React.useState<SharedInboxMessage[]>([])
  const [nextCursor, setNextCursor] = React.useState("")
  const [loadingMore, setLoadingMore] = React.useState(false)
  const [mobileDetail, setMobileDetail] = React.useState(false)

  const messages = useQuery({
    queryKey: ["shared-inbox", token],
    queryFn: () => sharedInboxApi.messages(token),
    enabled: !!token,
    retry: false,
  })
  const allItems = React.useMemo(() => [...(messages.data?.items || []), ...extraItems], [extraItems, messages.data?.items])
  const selected = useQuery({
    queryKey: ["shared-inbox-message", token, selectedID],
    queryFn: () => sharedInboxApi.message(token, selectedID),
    enabled: !!token && !!selectedID,
    retry: false,
  })

  React.useEffect(() => {
    setExtraItems([])
    setNextCursor(messages.data?.nextCursor || "")
    if (messages.data?.items.length && !selectedID) setSelectedID(messages.data.items[0].id)
  }, [messages.data])

  async function loadMore() {
    if (!nextCursor || loadingMore) return
    setLoadingMore(true)
    try {
      const page = await sharedInboxApi.messages(token, nextCursor)
      setExtraItems((items) => [...items, ...page.items.filter((item) => !items.some((existing) => existing.id === item.id))])
      setNextCursor(page.nextCursor || "")
    } finally {
      setLoadingMore(false)
    }
  }

  if (!token || messages.isError) {
    return (
      <main className="grid min-h-svh place-items-center bg-background p-6 text-foreground">
        <section className="w-full max-w-md text-center">
          <div className="mx-auto grid size-12 place-items-center rounded-md border bg-card"><ShieldCheck className="h-6 w-6 text-muted-foreground" /></div>
          <h1 className="mt-5 text-xl font-semibold">分享链接无效或已失效</h1>
          <p className="mt-2 text-sm leading-6 text-muted-foreground">链接可能已被关闭、重置、撤销权限，或邮件分享范围已经变更。</p>
        </section>
      </main>
    )
  }

  return (
    <main className="flex h-svh min-h-0 flex-col overflow-hidden bg-background text-foreground">
      <header className="flex min-h-16 shrink-0 items-center justify-between gap-4 border-b bg-card px-4 sm:px-6">
        <div className="flex min-w-0 items-center gap-3">
          <div className="grid size-9 shrink-0 place-items-center rounded-md bg-primary text-primary-foreground"><Mail className="h-5 w-5" /></div>
          <div className="min-w-0"><h1 className="truncate text-sm font-semibold sm:text-base">{messages.data?.mailboxAddress || "分享收件箱"}</h1><p className="truncate text-xs text-muted-foreground">{messages.data ? shareWindowLabel(messages.data.windowMinutes) : "加载中"}</p></div>
        </div>
        <div className="flex items-center gap-2"><span className="hidden items-center gap-1.5 text-xs text-muted-foreground sm:flex"><ShieldCheck className="h-4 w-4" />只读分享</span><Button type="button" variant="outline" size="icon" aria-label="刷新邮件" title="刷新邮件" disabled={messages.isFetching} onClick={() => { setExtraItems([]); setSelectedID(""); void messages.refetch() }}><RefreshCcw className={cn("h-4 w-4", messages.isFetching && "animate-spin")} /></Button></div>
      </header>
      <div className="grid min-h-0 min-w-0 flex-1 grid-cols-[minmax(0,1fr)] md:grid-cols-[minmax(300px,380px)_minmax(0,1fr)]">
        <section className={cn("min-h-0 min-w-0 border-r bg-card", mobileDetail && "hidden md:block")} aria-label="邮件列表">
          <div className="flex h-12 items-center justify-between border-b px-4"><div className="flex items-center gap-2 text-sm font-semibold"><Inbox className="h-4 w-4" />最新邮件</div><span className="text-xs text-muted-foreground">{allItems.length} 封</span></div>
          <div className="h-[calc(100%_-_3rem)] min-w-0 overflow-x-hidden overflow-y-auto">
            {messages.isLoading && <div className="space-y-3 p-4"><Skeleton className="h-20 w-full" /><Skeleton className="h-20 w-full" /><Skeleton className="h-20 w-full" /></div>}
            {allItems.map((item) => (
              <Button key={item.id} type="button" variant="ghost" className={cn("block h-auto min-w-0 max-w-full w-full overflow-hidden whitespace-normal rounded-none border-b px-4 py-3 text-left hover:bg-muted/40", item.id === selectedID && "bg-muted")} onClick={() => { setSelectedID(item.id); setMobileDetail(true) }}>
                <div className="flex min-w-0 items-center justify-between gap-3 text-sm"><span className="min-w-0 truncate font-medium">{item.fromName || item.from}</span><time className="shrink-0 text-xs tabular-nums text-muted-foreground">{messageTime(item.receivedAt)}</time></div>
                <div className="mt-1 flex min-w-0 items-center gap-1.5"><span className="min-w-0 truncate text-sm">{item.subject || "（无主题）"}</span>{item.hasAttachments && <Paperclip className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />}</div>
                <p className="mt-1 truncate text-xs text-muted-foreground">{item.snippet}</p>
              </Button>
            ))}
            {!messages.isLoading && allItems.length === 0 && <div className="px-6 py-16 text-center text-sm text-muted-foreground">当前分享范围内没有邮件</div>}
            {nextCursor && <div className="p-4"><Button type="button" variant="outline" className="w-full" disabled={loadingMore} onClick={loadMore}>{loadingMore ? "加载中" : "加载更多"}</Button></div>}
          </div>
        </section>
        <section className={cn("min-h-0 min-w-0 bg-background", !mobileDetail && "hidden md:block")} aria-label="邮件详情">
          {!selectedID && <div className="grid h-full place-items-center text-sm text-muted-foreground"><div className="text-center"><Mail className="mx-auto mb-3 h-9 w-9" />选择一封邮件查看详情</div></div>}
          {selectedID && <div className="h-full min-w-0 overflow-x-hidden overflow-y-auto">
            <div className="mx-auto min-w-0 max-w-4xl p-4 sm:p-7">
              <Button type="button" variant="ghost" size="sm" className="-ml-2 mb-3 md:hidden" onClick={() => setMobileDetail(false)}><ArrowLeft className="h-4 w-4" />返回列表</Button>
              {selected.isLoading && <div className="space-y-4"><Skeleton className="h-8 w-2/3" /><Skeleton className="h-16 w-full" /><Skeleton className="h-72 w-full" /></div>}
              {selected.isError && <div className="rounded-md border p-6 text-center text-sm text-muted-foreground">该邮件已不在当前分享范围内。</div>}
              {selected.data && <>
                <h2 className="break-words text-xl font-semibold sm:text-2xl">{selected.data.subject || "（无主题）"}</h2>
                <div className="mt-4 border-b pb-4 text-sm leading-6"><div className="font-medium">{selected.data.fromName || selected.data.from}</div><div className="break-all text-muted-foreground">{selected.data.fromName ? selected.data.from : ""}</div><div className="text-muted-foreground">{new Date(selected.data.receivedAt).toLocaleString("zh-CN", { hour12: false })} · {selected.data.folder}</div></div>
                <div className="mt-5 min-w-0 overflow-hidden rounded-md border bg-white text-black">
                  <SharedMessageBody key={selected.data.id} html={selected.data.bodyHtml} text={selected.data.bodyText} />
                </div>
                {!!selected.data.attachments?.length && <div className="mt-5"><h3 className="mb-2 text-sm font-semibold">附件</h3><div className="divide-y rounded-md border">{selected.data.attachments.map((attachment) => <Button key={attachment.id} type="button" variant="ghost" className="flex min-h-12 w-full justify-start rounded-none px-3 py-2 text-left hover:bg-muted/40" onClick={() => void sharedInboxApi.downloadAttachment(token, attachment.id, attachment.filename)}><FileText className="h-4 w-4 shrink-0" /><span className="min-w-0 flex-1 truncate text-sm">{attachment.filename}</span><span className="text-xs text-muted-foreground">{formatBytes(attachment.sizeBytes)}</span><Download className="h-4 w-4" /></Button>)}</div></div>}
                <p className="mt-6 flex items-center gap-2 text-xs text-muted-foreground"><ShieldCheck className="h-4 w-4" />只读分享页面，无法回复、转发、删除或修改邮件。</p>
              </>}
            </div>
          </div>}
        </section>
      </div>
    </main>
  )
}
