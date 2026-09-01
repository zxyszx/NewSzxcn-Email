import * as React from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useSearchParams } from "react-router-dom"
import {
  Add20Regular,
  ArrowRight20Regular,
  CheckmarkCircle20Filled,
  Copy20Regular,
  MailArrowForward20Regular,
  Mail20Regular,
  MailSettings20Regular,
  MoreHorizontal20Regular,
  Open20Regular,
  Search20Regular,
} from "@fluentui/react-icons"
import { Tooltip as FluentTooltip } from "@fluentui/react-components"

import { api, type ForwardingSettings, type Mailbox } from "@/lib/api"
import { formatDateTime } from "@/lib/utils"
import { useToast } from "@/hooks/use-toast"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"

const mailboxPageSize = 10

function mailboxErrorMessage(error: unknown) {
  return error instanceof Error && error.message ? error.message : "操作失败，请稍后重试"
}

function forwardingTargets(settings: ForwardingSettings | undefined, mailboxId: string) {
  const accountTargets = settings?.accountTargetEmails?.length
    ? settings.accountTargetEmails
    : settings?.accountTargetEmail ? [settings.accountTargetEmail] : []
  const mailboxRule = settings?.mailboxRules?.find((item) => item.mailboxId === mailboxId)
  const mailboxTargets = mailboxRule?.targetEmails?.length
    ? mailboxRule.targetEmails
    : mailboxRule?.targetEmail ? [mailboxRule.targetEmail] : []
  return Array.from(new Set([...accountTargets, ...mailboxTargets].filter(Boolean)))
}

function forwardingSummary(settings: ForwardingSettings | undefined, mailboxId: string) {
  const targets = forwardingTargets(settings, mailboxId)
  const pending = settings?.pendingBindings?.find(
    (binding) => binding.scope === "mailbox" && binding.mailboxId === mailboxId && binding.status !== "cancelled",
  )

  if (pending?.status === "pending_verification") return { label: "待验证", detail: pending.email, tone: "is-warning" }
  if (pending?.status === "activation_failed") return { label: "转发启用失败", detail: pending.email, tone: "is-error" }
  if (pending?.status === "expired") return { label: "验证已过期", detail: pending.email, tone: "is-error" }
  if (targets.length > 0) {
    return {
      label: `已转发至 ${targets[0]}`,
      detail: targets.length > 1 ? `另有 ${targets.length - 1} 个目标` : "转发中",
      tone: "is-active",
    }
  }
  return { label: "未设置转发", detail: "尚未添加接收地址", tone: "is-muted" }
}

export function MailboxWorkspace({
  mailboxes,
  loading,
  error,
  canCreate,
  onCreate,
  onOpenMailbox,
  onManageForwarding,
  onRetry,
}: {
  mailboxes: Mailbox[]
  loading: boolean
  error: unknown
  canCreate: boolean
  onCreate: () => void
  onOpenMailbox: (mailboxId: string) => void
  onManageForwarding: (mailboxId: string) => void
  onRetry: () => void
}) {
  const { toast } = useToast()
  const [params, setParams] = useSearchParams()
  const query = params.get("q") || ""
  const sort = params.get("sort") === "za" ? "za" : "az"
  const requestedPage = Math.max(1, Number(params.get("page") || "1") || 1)
  const forwarding = useQuery({ queryKey: ["forwarding-settings"], queryFn: api.forwardingSettings, retry: false })
  const normalizedQuery = query.trim().toLocaleLowerCase()
  const filtered = React.useMemo(() => {
    const items = normalizedQuery
      ? mailboxes.filter((mailbox) => mailbox.address.toLocaleLowerCase().includes(normalizedQuery) || mailbox.displayName.toLocaleLowerCase().includes(normalizedQuery))
      : [...mailboxes]
    return items.sort((left, right) => sort === "az" ? left.address.localeCompare(right.address) : right.address.localeCompare(left.address))
  }, [mailboxes, normalizedQuery, sort])
  const totalPages = Math.max(1, Math.ceil(filtered.length / mailboxPageSize))
  const page = Math.min(requestedPage, totalPages)
  const visible = filtered.slice((page - 1) * mailboxPageSize, page * mailboxPageSize)
  const verifiedTargetCount = forwarding.data?.verifiedEmails.filter((item) => item.verified).length || 0
  const forwardedCount = mailboxes.filter((item) => forwardingTargets(forwarding.data, item.id).length > 0).length

  React.useEffect(() => {
    if (requestedPage <= totalPages) return
    const next = new URLSearchParams(params)
    next.set("page", String(totalPages))
    setParams(next, { replace: true })
  }, [params, requestedPage, setParams, totalPages])

  function updateParams(change: Record<string, string | undefined>) {
    const next = new URLSearchParams(params)
    for (const [key, value] of Object.entries(change)) {
      if (value) next.set(key, value)
      else next.delete(key)
    }
    setParams(next, { replace: true })
  }

  async function copyAddress(address: string) {
    await navigator.clipboard.writeText(address)
    toast({ title: "邮箱地址已复制", description: address })
  }

  return (
    <section className="mailbox-workspace" aria-label="我的邮箱">
      <header className="mailbox-workspace-toolbar">
        <div className="mailbox-workspace-heading">
          <MailSettings20Regular aria-hidden="true" />
          <div><h1>我的邮箱</h1><p>创建、查找和管理当前账户拥有的邮箱。</p></div>
        </div>
        <Button type="button" className="mailbox-create-button" onClick={onCreate} disabled={!canCreate}><Add20Regular />创建新邮箱</Button>
      </header>

      <div className="mailbox-stat-strip" aria-label="邮箱概览">
        <div><span>邮箱总数</span><strong>{mailboxes.length}</strong></div>
        <div><span>已验证地址</span><strong>{forwarding.isLoading ? "…" : verifiedTargetCount}</strong></div>
        <div><span>已设置转发</span><strong>{forwarding.isLoading ? "…" : forwardedCount}</strong></div>
      </div>

      <div className="mailbox-list-panel">
        <div className="mailbox-list-toolbar">
          <div><h2>邮箱列表</h2><span>{filtered.length} 个邮箱</span></div>
          <div className="mailbox-list-controls">
            <label className="mailbox-list-search mailbox-search-field">
              <Search20Regular aria-hidden="true" />
              <Input value={query} onChange={(event) => updateParams({ q: event.target.value, page: undefined })} placeholder="搜索邮箱地址" type="search" autoComplete="off" aria-label="搜索邮箱地址" />
            </label>
            <Button type="button" variant="outline" className="mailbox-sort-button" onClick={() => updateParams({ sort: sort === "az" ? "za" : undefined, page: undefined })} aria-label={sort === "az" ? "当前按 A 到 Z 排序" : "当前按 Z 到 A 排序"}>{sort === "az" ? "A-Z" : "Z-A"}</Button>
          </div>
        </div>

        <div className="mailbox-list-body">
          {loading ? (
            <div className="mailbox-list-state">正在加载邮箱…</div>
          ) : error ? (
            <div className="mailbox-list-state"><strong>邮箱加载失败</strong><span>{mailboxErrorMessage(error)}</span><Button type="button" variant="outline" onClick={onRetry}>重新加载</Button></div>
          ) : visible.length === 0 ? (
            <div className="mailbox-list-state"><Mail20Regular /><strong>{query ? "没有匹配的邮箱" : "还没有邮箱"}</strong><span>{query ? "请调整搜索内容。" : "创建后即可在这里收发和管理邮件。"}</span>{!query && canCreate && <Button type="button" onClick={onCreate}><Add20Regular />创建新邮箱</Button>}</div>
          ) : visible.map((mailbox) => {
            const forwardingState = forwardingSummary(forwarding.data, mailbox.id)
            const mailboxState = mailbox.status === "active"
              ? { label: "可用", tone: "is-active" }
              : mailbox.status === "disabled"
                ? { label: "已停用", tone: "is-muted" }
                : { label: "异常", tone: "is-error" }
            return (
              <article className="mailbox-list-row" key={mailbox.id}>
                <span className="mailbox-row-avatar" aria-hidden="true">{mailbox.localPart.slice(0, 1).toLocaleUpperCase()}</span>
                <Button type="button" variant="ghost" className="mailbox-row-main" onClick={() => onOpenMailbox(mailbox.id)}>
                  <span className="mailbox-row-identity"><strong>{mailbox.address}</strong><small>创建于 {formatDateTime(mailbox.createdAt)}</small></span>
                </Button>
                <div className={`mailbox-row-forwarding ${forwardingState.tone}`}>
                  <strong>{forwardingState.label}</strong>
                  <small>{forwardingState.detail}</small>
                </div>
                <div className="mailbox-row-status">
                  <span className={mailboxState.tone}>{mailboxState.label}</span>
                </div>
                <div className="mailbox-row-actions">
                  <Button type="button" variant="outline" className="mailbox-open-button" onClick={() => onOpenMailbox(mailbox.id)}><Open20Regular />打开邮箱</Button>
                  <FluentTooltip content="管理转发" relationship="label" positioning="above" showDelay={450} withArrow>
                    <Button type="button" variant="ghost" size="icon" onClick={() => onManageForwarding(mailbox.id)} aria-label={`管理 ${mailbox.address} 的转发`} title="管理转发"><MailArrowForward20Regular /></Button>
                  </FluentTooltip>
                  <DropdownMenu>
                    <FluentTooltip content="更多" relationship="label" positioning="above" showDelay={450} withArrow>
                      <DropdownMenuTrigger asChild><Button type="button" variant="ghost" size="icon" aria-label={`${mailbox.address} 更多操作`} title="更多"><MoreHorizontal20Regular /></Button></DropdownMenuTrigger>
                    </FluentTooltip>
                    <DropdownMenuContent align="end" className="w-48">
                      <DropdownMenuItem disabled title="当前普通用户接口暂未开放此操作">设为默认邮箱</DropdownMenuItem>
                      <DropdownMenuItem disabled title="当前普通用户接口暂未开放此操作">修改显示名称</DropdownMenuItem>
                      <DropdownMenuItem disabled title="当前普通用户接口暂未开放此操作">修改密码</DropdownMenuItem>
                      <DropdownMenuItem onSelect={() => void copyAddress(mailbox.address)}><Copy20Regular />复制邮箱地址</DropdownMenuItem>
                      <DropdownMenuItem disabled title="当前普通用户接口暂未开放此操作">查看服务器配置</DropdownMenuItem>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem disabled className="mailbox-menu-destructive" title="当前普通用户接口暂未开放删除操作">删除邮箱</DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              </article>
            )
          })}
        </div>

        <footer className="mailbox-list-pagination">
          <span>共 {filtered.length} 个邮箱{totalPages > 1 ? ` · 第 ${page} / ${totalPages} 页` : ""}</span>
          {totalPages > 1 && <div>
            <Button type="button" variant="ghost" size="icon" disabled={page <= 1} onClick={() => updateParams({ page: String(page - 1) })} aria-label="上一页">‹</Button>
            {Array.from({ length: totalPages }, (_, index) => index + 1).slice(Math.max(0, page - 3), Math.max(5, page + 2)).map((value) => <Button type="button" key={value} variant={value === page ? "default" : "ghost"} size="icon" onClick={() => updateParams({ page: value === 1 ? undefined : String(value) })}>{value}</Button>)}
            <Button type="button" variant="ghost" size="icon" disabled={page >= totalPages} onClick={() => updateParams({ page: String(page + 1) })} aria-label="下一页">›</Button>
          </div>}
        </footer>
      </div>
    </section>
  )
}

export function MailboxCreateSheet({
  open,
  onOpenChange,
  canCreate,
  onCreated,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  canCreate: boolean
  onCreated: (mailbox: Mailbox, openMailbox: boolean) => void
}) {
  const qc = useQueryClient()
  const { toast } = useToast()
  const options = useQuery({ queryKey: ["mailbox-apply-options"], queryFn: api.mailboxApplyOptions, enabled: open && canCreate, retry: false })
  const [domainId, setDomainId] = React.useState("")
  const [localPart, setLocalPart] = React.useState("")
  const [displayName, setDisplayName] = React.useState("")
  const [created, setCreated] = React.useState<Mailbox | null>(null)

  React.useEffect(() => {
    if (!open || domainId || !options.data?.domains[0]) return
    setDomainId(options.data.domains[0].id)
  }, [domainId, open, options.data?.domains])

  React.useEffect(() => {
    if (open) return
    setLocalPart("")
    setDisplayName("")
    setCreated(null)
  }, [open])

  const selectedDomain = options.data?.domains.find((item) => item.id === domainId)
  const normalizedLocalPart = localPart.trim().toLocaleLowerCase()
  const preview = normalizedLocalPart && selectedDomain ? `${normalizedLocalPart}@${selectedDomain.name}` : "name@example.com"
  const reserved = options.data?.reservedPrefixes?.some((item) => item.toLocaleLowerCase() === normalizedLocalPart) || false
  const localPartValid = /^[a-z0-9](?:[a-z0-9._-]{0,62}[a-z0-9])?$/.test(normalizedLocalPart)
  const create = useMutation({
    mutationFn: () => api.applyMailbox({ domainId, localPart: normalizedLocalPart, displayName: displayName.trim() }),
    onSuccess: (mailbox) => {
      setCreated(mailbox)
      qc.setQueryData<{ items: Mailbox[] }>(["mailboxes", "mine"], (current) => current ? { ...current, items: [...current.items, mailbox] } : { items: [mailbox] })
      void qc.invalidateQueries({ queryKey: ["mailbox-apply-options"] })
      void qc.invalidateQueries({ queryKey: ["forwarding-settings"] })
      toast({ title: "邮箱创建成功", description: mailbox.address })
    },
    onError: (error) => toast({ title: "创建失败", description: mailboxErrorMessage(error), variant: "destructive" }),
  })

  function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!localPartValid || reserved || !domainId || create.isPending) return
    create.mutate()
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="mailbox-create-sheet w-[min(420px,calc(100vw-16px))] max-w-none p-0 sm:max-w-[420px]" overlayClassName="mailbox-create-overlay" aria-describedby={undefined}>
        <SheetTitle className="sr-only">创建新邮箱</SheetTitle>
        {created ? (
          <div className="mailbox-create-success">
            <span><CheckmarkCircle20Filled /></span>
            <h2>邮箱创建成功</h2>
            <p>{created.address}</p>
            <div className="mailbox-create-success-actions">
              <Button type="button" onClick={() => onCreated(created, true)}>进入邮箱<ArrowRight20Regular /></Button>
              <Button type="button" variant="outline" onClick={() => { setCreated(null); setLocalPart(""); setDisplayName("") }}>继续创建</Button>
              <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>关闭</Button>
            </div>
          </div>
        ) : (
          <form className="mailbox-create-form" onSubmit={submit}>
            <header><span><Add20Regular /></span><div><h2>创建新邮箱</h2><p>新邮箱将归属当前登录账户。</p></div></header>
            {options.isLoading ? <div className="mailbox-create-loading">正在加载可用域名…</div> : options.isError ? <div className="mailbox-create-error"><strong>无法加载创建选项</strong><span>{mailboxErrorMessage(options.error)}</span><Button type="button" variant="outline" onClick={() => void options.refetch()}>重试</Button></div> : (
              <div className="mailbox-create-fields">
                <div className="mailbox-address-preview"><span>邮箱地址预览</span><strong>{preview}</strong></div>
                <div className="space-y-2"><Label htmlFor="mailbox-create-local">邮箱前缀</Label><Input id="mailbox-create-local" value={localPart} onChange={(event) => setLocalPart(event.target.value)} placeholder="例如 netflix01" autoComplete="off" spellCheck={false} data-1p-ignore="true" data-lpignore="true" data-bwignore="true" disabled={create.isPending} />{normalizedLocalPart && !localPartValid && <p className="mailbox-field-error">请使用字母、数字、点、短横线或下划线，且首尾为字母或数字。</p>}{reserved && <p className="mailbox-field-error">该前缀已保留，请换一个名称。</p>}</div>
                <div className="space-y-2"><Label>邮箱域名</Label><Select value={domainId} onValueChange={setDomainId} disabled={create.isPending || !options.data?.enabled}><SelectTrigger><SelectValue placeholder="选择域名" /></SelectTrigger><SelectContent>{options.data?.domains.map((domain) => <SelectItem key={domain.id} value={domain.id}>@{domain.name}</SelectItem>)}</SelectContent></Select></div>
                <div className="space-y-2"><Label htmlFor="mailbox-create-display">显示名称 <span>可选</span></Label><Input id="mailbox-create-display" value={displayName} onChange={(event) => setDisplayName(event.target.value)} maxLength={80} placeholder="收件人看到的名称" autoComplete="off" data-1p-ignore="true" data-lpignore="true" data-bwignore="true" disabled={create.isPending} /></div>
                {!options.data?.enabled && <p className="mailbox-create-unavailable">当前账户未开放自助创建邮箱。</p>}
              </div>
            )}
            <footer><Button type="button" variant="outline" onClick={() => onOpenChange(false)}>取消</Button><Button type="submit" disabled={!options.data?.enabled || !domainId || !localPartValid || reserved || create.isPending}>{create.isPending ? "创建中…" : "创建邮箱"}</Button></footer>
          </form>
        )}
      </SheetContent>
    </Sheet>
  )
}
