import * as React from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useSearchParams } from "react-router-dom"
import {
  Add20Regular,
  ArrowRight20Regular,
  CheckmarkCircle20Filled,
  MailArrowForward20Regular,
  Mail20Regular,
  MailSettings20Regular,
  Search20Regular,
} from "@fluentui/react-icons"

import { api, type ForwardingSettings, type Mailbox } from "@/lib/api"
import { formatDateTime } from "@/lib/utils"
import { useToast } from "@/hooks/use-toast"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet"

const mailboxPageSize = 10

function mailboxErrorMessage(error: unknown) {
  return error instanceof Error && error.message ? error.message : "操作失败，请稍后重试"
}

function looksLikeEmail(value: string) {
  return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value.trim())
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
  const accountTargets = settings?.accountTargetEmails?.length
    ? settings.accountTargetEmails
    : settings?.accountTargetEmail ? [settings.accountTargetEmail] : []
  const mailboxRule = settings?.mailboxRules?.find((item) => item.mailboxId === mailboxId)
  const mailboxTargets = mailboxRule?.targetEmails?.length
    ? mailboxRule.targetEmails
    : mailboxRule?.targetEmail ? [mailboxRule.targetEmail] : []
  const targets = Array.from(new Set([...accountTargets, ...mailboxTargets].filter(Boolean)))
  const pending = settings?.pendingBindings?.find(
    (binding) => binding.scope === "mailbox" && binding.mailboxId === mailboxId && binding.status !== "cancelled",
  )

  if (pending?.status === "pending_verification") return { label: `转发：等待验证 ${pending.email}`, detail: "等待验证", tone: "is-warning", targetCount: targets.length }
  if (pending?.status === "activation_failed") return { label: `转发：启用失败 ${pending.email}`, detail: "启用失败", tone: "is-error", targetCount: targets.length }
  if (pending?.status === "expired") return { label: `转发：验证已过期 ${pending.email}`, detail: "验证失败", tone: "is-error", targetCount: targets.length }
  if (mailboxTargets.length > 0) return {
    label: `转发：${accountTargets.length ? "账号级＋单独" : "单独"} ${mailboxTargets[0]}`,
    detail: "转发中",
    tone: "is-active",
    targetCount: targets.length,
  }
  if (accountTargets.length > 0) return { label: `转发：使用账号级 ${accountTargets[0]}`, detail: "转发中", tone: "is-active", targetCount: targets.length }
  return { label: "转发：未设置", detail: "未设置", tone: "is-muted", targetCount: 0 }
}

export function MailboxWorkspace({
  mailboxes,
  loading,
  error,
  canCreate,
  onOpenMailbox,
  onRetry,
}: {
  mailboxes: Mailbox[]
  loading: boolean
  error: unknown
  canCreate: boolean
  onOpenMailbox: (mailboxId: string) => void
  onRetry: () => void
}) {
  const { toast } = useToast()
  const qc = useQueryClient()
  const [params, setParams] = useSearchParams()
  const [domainId, setDomainId] = React.useState("")
  const [localPart, setLocalPart] = React.useState("")
  const [forwardingMailboxId, setForwardingMailboxId] = React.useState("")
  const [forwardingDraftTargets, setForwardingDraftTargets] = React.useState<string[]>([])
  const [targetEmailDraft, setTargetEmailDraft] = React.useState("")
  const query = params.get("q") || ""
  const requestedPage = Math.max(1, Number(params.get("page") || "1") || 1)
  const forwarding = useQuery({ queryKey: ["forwarding-settings"], queryFn: api.forwardingSettings, retry: false })
  const createOptions = useQuery({ queryKey: ["mailbox-apply-options"], queryFn: api.mailboxApplyOptions, enabled: canCreate, retry: false })
  const normalizedQuery = query.trim().toLocaleLowerCase()
  const filtered = React.useMemo(() => {
    const items = normalizedQuery
      ? mailboxes.filter((mailbox) => mailbox.address.toLocaleLowerCase().includes(normalizedQuery) || mailbox.displayName.toLocaleLowerCase().includes(normalizedQuery))
      : [...mailboxes]
    return items.sort((left, right) => left.address.localeCompare(right.address))
  }, [mailboxes, normalizedQuery])
  const totalPages = Math.max(1, Math.ceil(filtered.length / mailboxPageSize))
  const page = Math.min(requestedPage, totalPages)
  const visible = filtered.slice((page - 1) * mailboxPageSize, page * mailboxPageSize)
  const verifiedTargetCount = forwarding.data?.verifiedEmails.filter((item) => item.verified).length || 0
  const forwardedCount = mailboxes.filter((item) => forwardingTargets(forwarding.data, item.id).length > 0).length
  const unconfiguredCount = Math.max(0, mailboxes.length - forwardedCount)
  const normalizedLocalPart = localPart.trim().toLocaleLowerCase()
  const localPartValid = /^[a-z0-9](?:[a-z0-9._-]{0,62}[a-z0-9])?$/.test(normalizedLocalPart)
  const reserved = createOptions.data?.reservedPrefixes?.some((item) => item.toLocaleLowerCase() === normalizedLocalPart) || false

  React.useEffect(() => {
    if (domainId || !createOptions.data?.domains[0]) return
    setDomainId(createOptions.data.domains[0].id)
  }, [createOptions.data?.domains, domainId])

  const create = useMutation({
    mutationFn: () => api.applyMailbox({ domainId, localPart: normalizedLocalPart, displayName: "" }),
    onSuccess: (mailbox) => {
      setLocalPart("")
      qc.setQueryData<{ items: Mailbox[] }>(["mailboxes", "mine"], (current) => current ? { ...current, items: [...current.items, mailbox] } : { items: [mailbox] })
      void qc.invalidateQueries({ queryKey: ["mailboxes", "mine"] })
      void qc.invalidateQueries({ queryKey: ["mailbox-apply-options"] })
      void qc.invalidateQueries({ queryKey: ["forwarding-settings"] })
      toast({ title: "邮箱创建成功", description: mailbox.address })
    },
    onError: (mutationError) => toast({ title: "创建失败", description: mailboxErrorMessage(mutationError), variant: "destructive" }),
  })
  const saveForwarding = useMutation({
    mutationFn: () => api.updateMailboxForwarding(forwardingMailboxId, forwardingDraftTargets),
    onSuccess: (next) => {
      qc.setQueryData(["forwarding-settings"], next)
      setForwardingMailboxId("")
      setTargetEmailDraft("")
      toast({ title: "转发设置已保存" })
    },
    onError: (mutationError) => toast({ title: "保存失败", description: mailboxErrorMessage(mutationError), variant: "destructive" }),
  })
  const bindForwardingTarget = useMutation({
    mutationFn: (email: string) => api.createForwardingPendingBinding({ email, scope: "mailbox", mailboxId: forwardingMailboxId }),
    onSuccess: (next, email) => {
      qc.setQueryData(["forwarding-settings"], next)
      const rule = next.mailboxRules.find((item) => item.mailboxId === forwardingMailboxId)
      const targets = rule?.targetEmails?.length ? rule.targetEmails : rule?.targetEmail ? [rule.targetEmail] : []
      const active = targets.some((target) => target.toLocaleLowerCase() === email.toLocaleLowerCase())
      const verification = next.verifiedEmails.find((item) => item.email.toLocaleLowerCase() === email.toLocaleLowerCase())
      setForwardingDraftTargets(Array.from(new Set(targets.filter(Boolean))))
      setTargetEmailDraft("")
      toast({
        title: verification?.deliveryStatus === "failed" ? "验证邮件发送失败" : active ? "绑定成功" : "验证邮件已发送",
        description: verification?.deliveryError || (active ? `${email} 已绑定到当前邮箱` : "车友完成邮箱验证后，系统会自动完成绑定。"),
        variant: verification?.deliveryStatus === "failed" ? "destructive" : "default",
      })
    },
    onError: (mutationError) => toast({ title: "绑定失败", description: mailboxErrorMessage(mutationError), variant: "destructive" }),
  })

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

  function submitCreate(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!createOptions.data?.enabled || !domainId || !localPartValid || reserved || create.isPending) return
    create.mutate()
  }

  function openForwardingDialog(mailboxId: string) {
    const rule = forwarding.data?.mailboxRules.find((item) => item.mailboxId === mailboxId)
    const targets = rule?.targetEmails?.length ? rule.targetEmails : rule?.targetEmail ? [rule.targetEmail] : []
    setForwardingMailboxId(mailboxId)
    setForwardingDraftTargets(Array.from(new Set(targets.filter(Boolean))))
    setTargetEmailDraft("")
  }

  const forwardingMailbox = mailboxes.find((mailbox) => mailbox.id === forwardingMailboxId)
  const accountTargets = forwarding.data?.accountTargetEmails?.length
    ? forwarding.data.accountTargetEmails
    : forwarding.data?.accountTargetEmail ? [forwarding.data.accountTargetEmail] : []
  const normalizedTargetEmail = targetEmailDraft.trim().toLocaleLowerCase()
  const targetVerification = forwarding.data?.verifiedEmails.find(
    (item) => item.email.toLocaleLowerCase() === normalizedTargetEmail,
  )
  const targetAlreadyBound = forwardingDraftTargets.some(
    (target) => target.toLocaleLowerCase() === normalizedTargetEmail,
  ) || accountTargets.some((target) => target.toLocaleLowerCase() === normalizedTargetEmail)
  const pendingTarget = forwarding.data?.pendingBindings?.find(
    (item) => item.scope === "mailbox" && item.mailboxId === forwardingMailboxId && item.email.toLocaleLowerCase() === normalizedTargetEmail && item.status === "pending_verification",
  )
  const targetEmailValid = looksLikeEmail(normalizedTargetEmail)

  return (
    <section className="mailbox-workspace" aria-label="我的邮箱">
      <div className="mailbox-stat-strip" aria-label="邮箱概览与创建邮箱">
        <div className="mailbox-summary" aria-label="邮箱概览">
          <div className="management-stat"><span className="management-stat-icon is-blue"><Mail20Regular /></span><span>邮箱总数<strong>{mailboxes.length}</strong></span></div>
          <i aria-hidden="true" />
          <div className="management-stat"><span className="management-stat-icon is-green"><CheckmarkCircle20Filled /></span><span>已验证地址<strong>{forwarding.isLoading ? "…" : verifiedTargetCount}</strong></span></div>
          <i aria-hidden="true" />
          <div className="management-stat"><span className="management-stat-icon is-purple"><MailArrowForward20Regular /></span><span>启用转发<strong>{forwarding.isLoading ? "…" : forwardedCount}</strong></span></div>
          <i aria-hidden="true" />
          <div className="management-stat"><span className="management-stat-icon is-orange"><MailSettings20Regular /></span><span>未设置转发<strong>{forwarding.isLoading ? "…" : unconfiguredCount}</strong></span></div>
        </div>
        <form className="mailbox-inline-create" onSubmit={submitCreate}>
          <strong>创建新邮箱</strong>
          <div>
            <Input value={localPart} onChange={(event) => setLocalPart(event.target.value)} placeholder="输入邮箱地址前缀" autoComplete="off" spellCheck={false} data-1p-ignore="true" data-lpignore="true" data-bwignore="true" disabled={!canCreate || create.isPending} aria-label="邮箱地址前缀" />
            <Select value={domainId} onValueChange={setDomainId} disabled={!canCreate || create.isPending || createOptions.isLoading || !createOptions.data?.enabled}>
              <SelectTrigger aria-label="邮箱域名"><SelectValue placeholder={createOptions.isLoading ? "加载域名…" : "选择域名"} /></SelectTrigger>
              <SelectContent>{createOptions.data?.domains.map((domain) => <SelectItem key={domain.id} value={domain.id}>@{domain.name}</SelectItem>)}</SelectContent>
            </Select>
            <Button type="submit" className="mailbox-create-button" disabled={!createOptions.data?.enabled || !domainId || !localPartValid || reserved || create.isPending}>{create.isPending ? "创建中…" : "创建邮箱"}</Button>
          </div>
        </form>
      </div>

      <div className="mailbox-list-panel">
        <div className="mailbox-list-toolbar">
          <div><h2>我的邮箱（{filtered.length}）</h2></div>
          <label className="mailbox-list-search">
            <Search20Regular aria-hidden="true" />
            <Input value={query} onChange={(event) => updateParams({ q: event.target.value, page: undefined })} placeholder="搜索邮箱地址" type="search" autoComplete="off" aria-label="搜索邮箱地址" />
          </label>
        </div>

        <div className="mailbox-list-body">
          {loading ? (
            <div className="mailbox-list-state">正在加载邮箱…</div>
          ) : error ? (
            <div className="mailbox-list-state"><strong>邮箱加载失败</strong><span>{mailboxErrorMessage(error)}</span><Button type="button" variant="outline" onClick={onRetry}>重新加载</Button></div>
          ) : visible.length === 0 ? (
            <div className="mailbox-list-state"><Mail20Regular /><strong>{query ? "没有匹配的邮箱" : "还没有邮箱"}</strong><span>{query ? "请调整搜索内容。" : "请在上方创建新邮箱。"}</span></div>
          ) : visible.map((mailbox) => {
            const forwardingState = forwardingSummary(forwarding.data, mailbox.id)
            return (
              <article className="mailbox-list-row" key={mailbox.id}>
                <span className="mailbox-row-avatar" aria-hidden="true"><Mail20Regular /></span>
                <Button type="button" variant="ghost" className="mailbox-row-main" onClick={() => onOpenMailbox(mailbox.id)}>
                  <span className="mailbox-row-identity"><strong>{mailbox.address}</strong></span>
                </Button>
                <span className="mailbox-row-created">创建于 {formatDateTime(mailbox.createdAt)}</span>
                <div className={`mailbox-row-forwarding ${forwardingState.tone}`}>
                  <strong>{forwardingState.label}</strong>
                  {forwardingState.targetCount > 1 && <small>查看全部 {forwardingState.targetCount} 个</small>}
                </div>
                <div className="mailbox-row-status">
                  <span className={`management-status-label ${forwardingState.tone}`}>{forwardingState.detail}</span>
                </div>
                <div className="mailbox-row-actions">
                  <Button type="button" variant="outline" onClick={() => openForwardingDialog(mailbox.id)}><MailArrowForward20Regular />转发</Button>
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

      <Dialog open={Boolean(forwardingMailboxId)} onOpenChange={(open) => { if (!open && !saveForwarding.isPending) setForwardingMailboxId("") }}>
        <DialogContent className="mailbox-forwarding-dialog w-[min(92vw,32rem)] max-w-none" aria-describedby={undefined}>
          <DialogHeader>
            <DialogTitle>邮件转发</DialogTitle>
          </DialogHeader>

          <div className="mailbox-forwarding-dialog-body">
            <strong className="mailbox-forwarding-address">{forwardingMailbox?.address || "当前邮箱"}</strong>
            {accountTargets.length > 0 && (
              <section>
                <div className="mailbox-forwarding-section-title"><strong>账号级转发</strong><span>自动继承</span></div>
                <div className="mailbox-forwarding-targets">{accountTargets.map((email) => <span key={email}>{email}<small>账号级</small></span>)}</div>
              </section>
            )}

            <section>
              <div className="mailbox-forwarding-section-title"><strong>单邮箱转发</strong><span>{forwardingDraftTargets.length} 个目标</span></div>
              {forwardingDraftTargets.length > 0 ? (
                <div className="mailbox-forwarding-targets">{forwardingDraftTargets.map((email) => (
                  <span key={email}>{email}<Button type="button" variant="ghost" size="icon" aria-label={`移除 ${email}`} onClick={() => setForwardingDraftTargets((current) => current.filter((target) => target !== email))}>×</Button></span>
                ))}</div>
              ) : <p className="mailbox-forwarding-empty">尚未设置单邮箱转发目标。</p>}
            </section>

            <section>
              <Label htmlFor="mailbox-forwarding-target">绑定转发邮箱</Label>
              <div className="mailbox-forwarding-add">
                <Input id="mailbox-forwarding-target" value={targetEmailDraft} onChange={(event) => setTargetEmailDraft(event.target.value)} placeholder="输入已验证或未验证邮箱" type="email" autoComplete="off" spellCheck={false} disabled={bindForwardingTarget.isPending || saveForwarding.isPending} onKeyDown={(event) => { if (event.key === "Enter" && targetEmailValid && !targetAlreadyBound && !pendingTarget && !bindForwardingTarget.isPending) { event.preventDefault(); bindForwardingTarget.mutate(normalizedTargetEmail) } }} />
                <Button type="button" disabled={!targetEmailValid || targetAlreadyBound || Boolean(pendingTarget) || bindForwardingTarget.isPending || saveForwarding.isPending} onClick={() => bindForwardingTarget.mutate(normalizedTargetEmail)}>
                  {bindForwardingTarget.isPending ? "处理中…" : targetAlreadyBound ? "已绑定" : pendingTarget ? "等待验证" : targetVerification?.verified ? "立即绑定" : "发送验证并绑定"}
                </Button>
              </div>
              {!normalizedTargetEmail && <small>已验证邮箱会立即绑定；未验证邮箱会发送验证邮件，车友验证成功后自动绑定。</small>}
              {normalizedTargetEmail && !targetEmailValid && <small className="is-error">请输入完整有效的邮箱地址。</small>}
              {targetEmailValid && targetAlreadyBound && <small>该邮箱已经绑定，无需重复操作。</small>}
              {targetEmailValid && pendingTarget && <small>验证邮件已发送，车友验证成功后会自动绑定。</small>}
              {targetEmailValid && !targetAlreadyBound && !pendingTarget && <small>{targetVerification?.verified ? "该邮箱已验证，可以立即绑定。" : "该邮箱尚未验证，提交后将发送验证邮件并等待自动绑定。"}</small>}
            </section>
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" disabled={saveForwarding.isPending} onClick={() => setForwardingMailboxId("")}>取消</Button>
            <Button type="button" disabled={saveForwarding.isPending} onClick={() => saveForwarding.mutate()}>{saveForwarding.isPending ? "保存中…" : "保存"}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
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
