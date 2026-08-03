import * as React from "react"
import DOMPurify from "dompurify"
import { useSearchParams } from "react-router-dom"
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { ArrowRight, BookOpen, CheckCircle2, ChevronDown, Circle, ClipboardList, Copy, ExternalLink, Github, Globe2, Mail, Mailbox, MoreHorizontal, Plus, RefreshCcw, Scale, Search, ShieldCheck, Star, Trash2, Users } from "lucide-react"
import { api, AdminUser, Alias, DNSRecord, Domain, Mailbox as MailboxType, MailMessage, MailTemplate, MaildirSyncHealth, PermissionGroup, PermissionInfo, PermissionLimits, SystemSettings } from "@/lib/api"
import { cn, decodeMimeHeader, formatBytes, formatDate } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Separator } from "@/components/ui/separator"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Textarea } from "@/components/ui/textarea"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { SystemVersionDialog } from "@/components/system-version-dialog"
import { useMe } from "@/hooks/use-me"
import { useToast } from "@/hooks/use-toast"
import { hasAnyPermission, hasPermission } from "@/lib/permissions"
import type { PermissionKey } from "@/lib/api-types"

type Section = "overview" | "users" | "permissionGroups" | "domains" | "mailboxes" | "aliases" | "messages" | "sendAudit" | "settings"
type PendingConfirm = { title: string; description?: string; confirmText: string; onConfirm: () => void }

const sectionMeta: Record<Section, { label: string; frontLabel: string; description: string }> = {
  overview: { label: "数据总览", frontLabel: "数据统计", description: "系统运行、DNS、邮箱和消息状态集中查看。" },
  users: { label: "账号管理", frontLabel: "账号设置", description: "管理登录账号、身份状态、邮箱数量上限和绑定邮箱。" },
  permissionGroups: { label: "权限配额", frontLabel: "账号配额", description: "配置前台菜单权限、发信频率、附件和邮箱创建额度。" },
  domains: { label: "域名管理", frontLabel: "邮箱地址", description: "维护邮件域名、DKIM 和 DNS 检测。" },
  mailboxes: { label: "邮箱管理", frontLabel: "邮箱管理", description: "创建、分配、停用邮箱，保持与前台邮箱列表一致。" },
  aliases: { label: "邮件转发", frontLabel: "邮件转发", description: "管理域名转发规则。" },
  messages: { label: "全部邮件", frontLabel: "全部邮箱", description: "按邮箱、文件夹和关键词查看全站邮件。" },
  sendAudit: { label: "发送队列", frontLabel: "发送队列", description: "查看发信投递、重试和失败记录。" },
  settings: { label: "系统设置", frontLabel: "账号设置", description: "管理站点、发信、存储、注册、安全和邮件模板。" },
}
const sectionLabels = Object.fromEntries(Object.entries(sectionMeta).map(([key, value]) => [key, value.label])) as Record<Section, string>
const sectionKeys = Object.keys(sectionLabels) as Section[]
const sectionPermissions: Record<Section, PermissionKey[]> = {
  overview: ["admin.overview.view"],
  users: ["admin.users.view"],
  permissionGroups: ["admin.permission_groups.view"],
  domains: ["admin.domains.view", "admin.dns.view"],
  mailboxes: ["admin.mailboxes.view"],
  aliases: ["admin.aliases.view"],
  messages: ["admin.messages.view"],
  sendAudit: ["admin.messages.view"],
  settings: ["admin.settings.view", "admin.templates.view"],
}
const projectRepositoryUrl = "https://github.com/zxyszx/NewSzxcn-Email"
const projectTelegramUrl = "https://t.me/+EhII7MSyi3QwNDQ5"
const defaultPermissionLimits: PermissionLimits = { maxAttachmentMb: 25, maxMailboxCount: 9, smtpDailyLimit: 200, smtpMinuteLimit: 20, imapMinuteLimit: 200, pop3MinuteLimit: 150 }
const defaultMailboxLimitOverride = 9
const accountLoginName = (user: Pick<AdminUser, "email" | "loginName">) => user.loginName || user.email

export function AdminPage() {
  const qc = useQueryClient()
  const { toast } = useToast()
  const me = useMe()
  const user = me.data?.user
  const canOverview = hasPermission(user, "admin.overview.view")
  const canUsersView = hasPermission(user, "admin.users.view")
  const canPermissionGroupsView = hasPermission(user, "admin.permission_groups.view")
  const canDomainsView = hasPermission(user, "admin.domains.view")
  const canDNSView = hasPermission(user, "admin.dns.view")
  const canMailboxesView = hasPermission(user, "admin.mailboxes.view")
  const canAliasesView = hasPermission(user, "admin.aliases.view")
  const canMessagesView = hasPermission(user, "admin.messages.view")
  const canSettingsView = hasPermission(user, "admin.settings.view")
  const canTemplatesView = hasPermission(user, "admin.templates.view")
  const overview = useQuery({ queryKey: ["admin", "overview"], queryFn: api.adminOverview, enabled: !!user && canOverview })
  const users = useQuery({ queryKey: ["admin", "users"], queryFn: api.users, enabled: !!user && (canUsersView || canMailboxesView) })
  const permissionGroups = useQuery({ queryKey: ["admin", "permission-groups"], queryFn: api.permissionGroups, enabled: !!user && (canPermissionGroupsView || canUsersView) })
  const domains = useQuery({ queryKey: ["admin", "domains"], queryFn: api.domains, enabled: !!user && (canDomainsView || canDNSView || canMailboxesView || canAliasesView || canSettingsView || canTemplatesView) })
  const mailboxes = useQuery({ queryKey: ["admin", "mailboxes"], queryFn: api.mailboxes, enabled: !!user && (canMailboxesView || canMessagesView) })
  const aliases = useQuery({ queryKey: ["admin", "aliases"], queryFn: api.aliases, enabled: !!user && canAliasesView })
  const settings = useQuery({ queryKey: ["admin", "settings"], queryFn: api.systemSettings, enabled: !!user && canSettingsView })
  const [params, setParams] = useSearchParams()
  const [refreshing, setRefreshing] = React.useState(false)

  const domainItems = domains.data?.items || []
  const mailboxItems = mailboxes.data?.items || []
  const aliasItems = aliases.data?.items || []
  const userItems = users.data?.items || []
  const assignablePermissionGroups = (permissionGroups.data?.items || []).filter((group) => group.id !== "pg_super_admin" && group.id !== "pg_regular_user")
  const visibleSections = sectionKeys.filter((key) => hasAnyPermission(user, sectionPermissions[key]))
  const rawSection = params.get("section") as Section | null
  const section: Section = rawSection && visibleSections.includes(rawSection) ? rawSection : visibleSections[0] || "overview"

  async function refreshAdminPage() {
    if (refreshing) return
    setRefreshing(true)
    try {
      await Promise.all([
        qc.invalidateQueries({ queryKey: ["admin"] }),
        qc.invalidateQueries({ queryKey: ["mailboxes"] }),
        qc.invalidateQueries({ queryKey: ["me"] }),
      ])
      toast({ title: "后台数据已刷新" })
    } catch (error) {
      toast({ title: "刷新失败", description: error instanceof Error ? error.message : "请稍后重试" })
    } finally {
      setRefreshing(false)
    }
  }

  return (
    <ScrollArea className="h-[calc(100svh-3rem)] md:h-svh">
      <main className="mx-auto w-full max-w-[1180px] px-3 pb-10 pt-3 sm:px-4 sm:pt-4">
        <AdminPageHeader section={section} refreshing={refreshing} onRefresh={refreshAdminPage} />

        {section === "overview" && canOverview && (
          <div className="mb-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            <Stat icon={<Users />} label="账号" value={overview.data?.users || 0} />
            <Stat icon={<Globe2 />} label="邮件域名" value={overview.data?.domains || 0} />
            <Stat icon={<Mailbox />} label="邮箱" value={overview.data?.mailboxes || 0} />
            <Stat icon={<ShieldCheck />} label="存储用量" value={formatBytes(overview.data?.storageBytes || 0)} />
          </div>
        )}

        {section === "overview" && <OverviewSection overview={overview.data} domains={domainItems} settings={settings.data} visibleSections={visibleSections} onSectionChange={(next) => setParams(next === "overview" ? {} : { section: next })} />}
        {section === "users" && <UsersSection users={userItems} permissionGroups={assignablePermissionGroups} />}
        {section === "permissionGroups" && <PermissionGroupsSection groups={permissionGroups.data?.items || []} catalog={permissionGroups.data?.catalog || []} />}
        {section === "domains" && <DomainsSection domains={domainItems} />}
        {section === "mailboxes" && <MailboxesSection mailboxes={mailboxItems} users={userItems} domains={domainItems} />}
        {section === "aliases" && <AliasesSection aliases={aliasItems} domains={domainItems} />}
        {section === "messages" && <AdminMessagesSection mailboxes={mailboxItems} systemAdmin={user?.role === "admin"} />}
        {section === "sendAudit" && <AdminSendAuditSection mailboxes={mailboxItems} />}
        {section === "settings" && <SystemSettingsSection settings={settings.data} domains={domainItems} />}
      </main>
    </ScrollArea>
  )
}

function AdminPageHeader({ section, refreshing, onRefresh }: { section: Section; refreshing: boolean; onRefresh: () => void }) {
  const meta = sectionMeta[section]
  return (
    <div className="mb-4 border-b pb-3">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-end sm:justify-between">
        <div className="min-w-0">
          <div className="mb-1 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <span>后台管理</span>
            <span className="h-1 w-1 rounded-full bg-muted-foreground/50" />
            <span>前台：{meta.frontLabel}</span>
          </div>
          <h1 className="text-[20px] font-semibold leading-7 tracking-tight">{meta.label}</h1>
          <p className="mt-1 text-sm leading-5 text-muted-foreground">{meta.description}</p>
        </div>
        <div className="flex items-center gap-2">
          <Button type="button" variant="outline" size="icon" className="h-8 w-8 shadow-none" onClick={onRefresh} disabled={refreshing} aria-label="刷新后台数据" title="刷新后台数据">
            <RefreshCcw className={cn("h-4 w-4", refreshing && "animate-spin")} />
          </Button>
          <Badge variant="outline" className="h-7 rounded-md px-2.5 font-normal">NewSzxcn</Badge>
        </div>
      </div>
    </div>
  )
}

function OverviewSection({ overview, domains, settings, visibleSections, onSectionChange }: { overview?: { activeUsers: number; activeMailboxes: number; aliases: number; messages: number; unreadMessages: number }; domains: Domain[]; settings?: SystemSettings; visibleSections: Section[]; onSectionChange: (section: Section) => void }) {
  const checklist = setupChecklist(overview, domains, settings).filter((item) => visibleSections.includes(item.section))
  return (
    <div className="space-y-6">
      <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_420px]">
        <Card>
          <CardHeader><CardTitle>系统状态</CardTitle></CardHeader>
          <CardContent className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
            <InfoBox label="活跃账号" value={overview?.activeUsers || 0} />
            <InfoBox label="活跃邮箱" value={overview?.activeMailboxes || 0} />
            <InfoBox label="邮件转发" value={overview?.aliases || 0} />
            <InfoBox label="未读邮件" value={overview?.unreadMessages || 0} />
          </CardContent>
        </Card>
        <Card>
          <CardHeader><CardTitle>首次配置</CardTitle></CardHeader>
          <CardContent className="space-y-2">
            {checklist.map((item) => (
              <Button key={item.key} type="button" variant="outline" className="h-auto w-full justify-start gap-3 px-3 py-2 text-left font-normal" onClick={() => onSectionChange(item.section)}>
                {item.done ? <CheckCircle2 className="h-4 w-4 shrink-0 text-green-600" /> : <Circle className="h-4 w-4 shrink-0 text-muted-foreground" />}
                <span className="min-w-0 flex-1">
                  <span className="block font-medium">{item.title}</span>
                  <span className="block truncate text-xs text-muted-foreground">{item.detail}</span>
                </span>
                <ArrowRight className="h-4 w-4 shrink-0 text-muted-foreground" />
              </Button>
            ))}
          </CardContent>
        </Card>
      </div>
      <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_420px]">
        <Card>
          <CardHeader><CardTitle>DNS 状态</CardTitle></CardHeader>
          <CardContent className="space-y-2">
            {domains.map((domain) => <DomainBadgeRow key={domain.id} domain={domain} />)}
            {domains.length === 0 && <Empty text="暂无域名" />}
          </CardContent>
        </Card>
        <Card>
          <CardHeader><CardTitle>运行提示</CardTitle></CardHeader>
          <CardContent className="space-y-3 text-sm text-muted-foreground">
            <InfoLine label="公网地址" value={settings?.publicBaseUrl || "-"} />
            <InfoLine label="SMTP" value={settings?.smtpHost ? `${settings.smtpHost}:${settings.smtpPort}` : "-"} />
            <InfoLine label="注册" value={settings?.openRegistration ? "已开放" : "关闭"} />
            <InfoLine label="自助申请邮箱" value={settings?.userMailboxApplyEnabled ? "已启用" : "关闭"} />
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

function setupChecklist(overview: { activeUsers: number; activeMailboxes: number; aliases: number; messages: number; unreadMessages: number } | undefined, domains: Domain[], settings?: SystemSettings) {
  const hasDomain = domains.length > 0
  const dnsReady = domains.some((domain) => domain.dnsStatus === "ok")
  const hasMailbox = (overview?.activeMailboxes || 0) > 0
  const hasMail = (overview?.messages || 0) > 0
  return [
    { key: "domain", title: "添加邮件域名", detail: hasDomain ? `${domains.length} 个域名已添加` : "先添加 example.com 这样的邮件域名", done: hasDomain, section: "domains" as Section },
    { key: "dns", title: "完成 DNS 检测", detail: dnsReady ? "至少一个域名 DNS 正常" : "配置 MX、SPF、DKIM、DMARC 后执行检测", done: dnsReady, section: "domains" as Section },
    { key: "mailbox", title: "创建邮箱", detail: hasMailbox ? `${overview?.activeMailboxes || 0} 个活跃邮箱` : "给管理员或普通账号创建第一个邮箱", done: hasMailbox, section: "mailboxes" as Section },
    { key: "smtp", title: "确认发信链路", detail: settings?.smtpHost ? `内置 Postfix：${settings.smtpHost}:${settings.smtpPort}` : "默认使用内置 Postfix", done: true, section: "settings" as Section },
    { key: "mail", title: "完成收发测试", detail: hasMail ? `${overview?.messages || 0} 封邮件已入库` : "发送或接收一封测试邮件", done: hasMail, section: "messages" as Section },
  ]
}

function InfoLine({ label, value }: { label: string; value: React.ReactNode }) {
  return <div className="flex items-center justify-between gap-3 rounded-md border px-3 py-2"><span>{label}</span><span className="min-w-0 truncate font-medium text-foreground">{value}</span></div>
}

function UsersSection({ users, permissionGroups }: { users: AdminUser[]; permissionGroups: PermissionGroup[] }) {
  const me = useMe()
  const user = me.data?.user
  const qc = useQueryClient()
  const { toast } = useToast()
  const [query, setQuery] = React.useState("")
  const [roleFilter, setRoleFilter] = React.useState("all")
  const [statusFilter, setStatusFilter] = React.useState("all")
  const [pendingConfirm, setPendingConfirm] = React.useState<PendingConfirm | null>(null)
  const canCreate = hasPermission(user, "admin.users.create")
  const canDelete = hasPermission(user, "admin.users.delete")
  const filteredUsers = users.filter((user) => {
    const keyword = query.trim().toLowerCase()
    const loginName = accountLoginName(user)
    const matchesKeyword = !keyword || [loginName, user.email, user.displayName, ...(user.mailboxes || [])].some((value) => value.toLowerCase().includes(keyword))
    const matchesRole = roleFilter === "all" || user.role === roleFilter
    const matchesStatus = statusFilter === "all" || (statusFilter === "active" ? !user.disabled : user.disabled)
    return matchesKeyword && matchesRole && matchesStatus
  })
  const remove = useMutation({ mutationFn: api.deleteUser, onSuccess: () => { setPendingConfirm(null); invalidateAdmin(qc); toast({ title: "账号已删除" }) }, onError: (e) => toast({ title: "删除失败", description: e.message }) })
  return (
    <Card>
      <CardHeader>
        <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
          <CardTitle>账号管理</CardTitle>
          {canCreate && <CreateUserDialog permissionGroups={permissionGroups} />}
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex flex-col gap-3 lg:flex-row">
          <div className="relative flex-1">
            <Search className="absolute left-3 top-2.5 h-4 w-4 text-muted-foreground" />
            <Input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索账号、邮箱、显示名称" className="pl-9" />
          </div>
          <Select value={roleFilter} onValueChange={setRoleFilter}>
            <SelectTrigger className="lg:w-36"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部角色</SelectItem>
              <SelectItem value="admin">管理员</SelectItem>
              <SelectItem value="user">普通用户</SelectItem>
            </SelectContent>
          </Select>
          <Select value={statusFilter} onValueChange={setStatusFilter}>
            <SelectTrigger className="lg:w-36"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部状态</SelectItem>
              <SelectItem value="active">正常</SelectItem>
              <SelectItem value="disabled">停用</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-3 md:hidden">
          {filteredUsers.map((user) => (
            <div key={user.id} className="rounded-lg border p-4">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="truncate font-medium">{user.displayName}</div>
                  <div className="truncate text-xs text-muted-foreground">{accountLoginName(user)}</div>
                </div>
                <UserActions user={user} permissionGroups={permissionGroups} onDelete={canDelete ? () => setPendingConfirm({ title: "删除账号？", description: `将删除 ${accountLoginName(user)} 及其关联数据。`, confirmText: "删除账号", onConfirm: () => remove.mutate(user.id) }) : undefined} />
              </div>
              <div className="mt-3 flex flex-wrap gap-2">
                <RoleBadge user={user} />
                <Badge variant={user.disabled ? "secondary" : "default"}>{user.disabled ? "停用" : "正常"}</Badge>
                <Badge variant="outline">{new Date(user.createdAt).toLocaleDateString()}</Badge>
              </div>
              <div className="mt-3"><UserPermissionGroupsCell user={user} /></div>
              <div className="mt-3"><UserMailboxCell user={user} /></div>
            </div>
          ))}
        </div>
        <div className="hidden md:block">
          <Table>
            <TableHeader><TableRow><TableHead>账号</TableHead><TableHead>身份</TableHead><TableHead>权限配额</TableHead><TableHead className="w-[22rem]">邮箱</TableHead><TableHead>状态</TableHead><TableHead>创建</TableHead><TableHead className="w-16"></TableHead></TableRow></TableHeader>
            <TableBody>
              {filteredUsers.map((user) => (
                <TableRow key={user.id}>
                  <TableCell>
                    <div className="font-medium">{user.displayName}</div>
                    <div className="text-xs text-muted-foreground">{accountLoginName(user)}</div>
                  </TableCell>
                  <TableCell><RoleBadge user={user} /></TableCell>
                  <TableCell><UserPermissionGroupsCell user={user} /></TableCell>
                  <TableCell className="w-[22rem] max-w-[22rem]"><UserMailboxCell user={user} /></TableCell>
                  <TableCell><Badge variant={user.disabled ? "secondary" : "default"}>{user.disabled ? "停用" : "正常"}</Badge></TableCell>
                  <TableCell className="text-muted-foreground">{new Date(user.createdAt).toLocaleDateString()}</TableCell>
                  <TableCell><UserActions user={user} permissionGroups={permissionGroups} onDelete={canDelete ? () => setPendingConfirm({ title: "删除账号？", description: `将删除 ${accountLoginName(user)} 及其关联数据。`, confirmText: "删除账号", onConfirm: () => remove.mutate(user.id) }) : undefined} /></TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
        {filteredUsers.length === 0 && <Empty text="没有匹配的账号" />}
      </CardContent>
      <ConfirmDialog open={!!pendingConfirm} title={pendingConfirm?.title || ""} description={pendingConfirm?.description} confirmText={pendingConfirm?.confirmText || "删除"} destructive pending={remove.isPending} onOpenChange={(open) => { if (!open) setPendingConfirm(null) }} onConfirm={() => pendingConfirm?.onConfirm()} />
    </Card>
  )
}

function PermissionGroupsSection({ groups, catalog }: { groups: PermissionGroup[]; catalog: PermissionInfo[] }) {
  const me = useMe()
  const user = me.data?.user
  const qc = useQueryClient()
  const { toast } = useToast()
  const [query, setQuery] = React.useState("")
  const [editing, setEditing] = React.useState<PermissionGroup | null>(null)
  const [pendingConfirm, setPendingConfirm] = React.useState<PendingConfirm | null>(null)
  const canCreate = hasPermission(user, "admin.permission_groups.create")
  const canUpdate = hasPermission(user, "admin.permission_groups.update")
  const canDelete = hasPermission(user, "admin.permission_groups.delete")
  const remove = useMutation({
    mutationFn: api.deletePermissionGroup,
    onSuccess: () => {
      setPendingConfirm(null)
      invalidateAdmin(qc)
      toast({ title: "权限配额已删除" })
    },
    onError: (e) => toast({ title: "删除失败", description: e.message }),
  })
  const filtered = groups.filter((group) => {
    const keyword = query.trim().toLowerCase()
    if (!keyword) return true
    return [group.name, group.description, ...group.permissions].some((value) => value.toLowerCase().includes(keyword))
  })
  const isEditable = (group: PermissionGroup) => group.id !== "pg_super_admin"
  const isDeletable = (group: PermissionGroup) => !group.system && group.userCount === 0
  return (
    <Card>
      <CardHeader>
        <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
          <CardTitle>权限配额</CardTitle>
          {canCreate && <PermissionGroupDialog catalog={catalog} />}
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="relative">
          <Search className="absolute left-3 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索权限配额、说明或权限键" className="pl-9" />
        </div>
        <div className="grid gap-3 lg:grid-cols-2">
          {filtered.map((group) => (
            <div key={group.id} className="rounded-lg border p-4">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <div className="font-medium">{group.name}</div>
                    {group.system && <Badge variant="outline">系统组</Badge>}
                    {!group.system && <Badge variant="secondary">自定义</Badge>}
                    <Badge variant="outline">{group.userCount} 人</Badge>
                  </div>
                  <div className="mt-1 line-clamp-2 text-sm text-muted-foreground">{group.description || "未填写说明"}</div>
                </div>
                {(canUpdate || canDelete) && <DropdownMenu>
                  <DropdownMenuTrigger asChild><Button variant="ghost" size="icon"><MoreHorizontal className="h-4 w-4" /></Button></DropdownMenuTrigger>
                  <DropdownMenuContent align="end">
                    <DropdownMenuItem disabled={!isEditable(group) || !canUpdate} onSelect={() => setEditing(group)}>编辑权限配额</DropdownMenuItem>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem
                      className="text-destructive"
                      disabled={!isDeletable(group) || !canDelete}
                      onSelect={() => setPendingConfirm({ title: "删除权限配额？", description: `${group.name} 删除后不能再分配给账号。`, confirmText: "删除权限配额", onConfirm: () => remove.mutate(group.id) })}
                    >
                      删除权限配额
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>}
              </div>
              <PermissionBadges permissions={group.permissions} catalog={catalog} />
              <PermissionLimitBadges limits={group.limits} />
            </div>
          ))}
        </div>
        {filtered.length === 0 && <Empty text="暂无匹配的权限配额" />}
      </CardContent>
      {editing && <PermissionGroupDialog group={editing} catalog={catalog} open={!!editing} onOpenChange={(open) => { if (!open) setEditing(null) }} />}
      <ConfirmDialog open={!!pendingConfirm} title={pendingConfirm?.title || ""} description={pendingConfirm?.description} confirmText={pendingConfirm?.confirmText || "删除"} destructive pending={remove.isPending} onOpenChange={(open) => { if (!open) setPendingConfirm(null) }} onConfirm={() => pendingConfirm?.onConfirm()} />
    </Card>
  )
}

function PermissionGroupDialog({ group, catalog, open, onOpenChange }: { group?: PermissionGroup; catalog: PermissionInfo[]; open?: boolean; onOpenChange?: (open: boolean) => void }) {
  const qc = useQueryClient()
  const { toast } = useToast()
  const [internalOpen, setInternalOpen] = React.useState(false)
  const dialogOpen = open ?? internalOpen
  const setDialogOpen = onOpenChange ?? setInternalOpen
  const defaultLimitsQuery = useQuery({ queryKey: ["admin", "permission-limits", "defaults"], queryFn: api.defaultPermissionLimits, enabled: dialogOpen })
  const defaultLimits = defaultLimitsQuery.data || defaultPermissionLimits
  const [permissions, setPermissions] = React.useState<PermissionKey[]>(group?.permissions || [])
  const [limits, setLimits] = React.useState<PermissionLimits>(group?.limits || defaultLimits)
  React.useEffect(() => {
    if (dialogOpen) {
      setPermissions(group?.permissions || [])
      setLimits(group?.limits || defaultLimits)
    }
  }, [defaultLimits, dialogOpen, group])
  const mutation = useMutation({
    mutationFn: (form: FormData) => {
      const payload = {
        name: String(form.get("name") || ""),
        description: String(form.get("description") || ""),
        permissions,
        limits,
      }
      return group ? api.updatePermissionGroup(group.id, payload) : api.createPermissionGroup(payload)
    },
    onSuccess: () => {
      invalidateAdmin(qc)
      setDialogOpen(false)
      toast({ title: group ? "权限配额已更新" : "权限配额已创建" })
    },
    onError: (e) => toast({ title: group ? "更新失败" : "创建失败", description: e.message }),
  })
  const trigger = group ? null : (
    <DialogTrigger asChild>
      <Button size="sm"><Plus className="h-4 w-4" />权限配额</Button>
    </DialogTrigger>
  )
  return (
    <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
      {trigger}
      <DialogContent className="max-h-[86vh] overflow-y-auto sm:max-w-3xl">
        <DialogHeader><DialogTitle>{group ? "编辑权限配额" : "创建权限配额"}</DialogTitle></DialogHeader>
        <form className="space-y-4" onSubmit={(event) => { event.preventDefault(); mutation.mutate(new FormData(event.currentTarget)) }}>
          <div className="grid gap-4 md:grid-cols-2">
            <Field name="name" label="名称" defaultValue={group?.name || ""} placeholder="例如：客服主管" />
            <Field name="description" label="说明" defaultValue={group?.description || ""} required={false} />
          </div>
          <PermissionLimitEditor value={limits} onChange={setLimits} />
          <PermissionPicker catalog={catalog} value={permissions} onChange={setPermissions} />
          <DialogFooter><Button disabled={mutation.isPending}>{mutation.isPending ? "保存中..." : "保存"}</Button></DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function PermissionPicker({ catalog, value, onChange }: { catalog: PermissionInfo[]; value: PermissionKey[]; onChange: (value: PermissionKey[]) => void }) {
  const grouped = groupPermissionCatalog(catalog)
  function toggle(permission: PermissionKey, checked: boolean) {
    onChange(checked ? Array.from(new Set([...value, permission])) : value.filter((item) => item !== permission))
  }
  function toggleCategory(items: PermissionInfo[], checked: boolean) {
    const keys = items.map((item) => item.key)
    onChange(checked ? Array.from(new Set([...value, ...keys])) : value.filter((item) => !keys.includes(item)))
  }
  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between gap-3">
        <Label>菜单与操作权限</Label>
        <Badge variant="outline">{value.length} 项</Badge>
      </div>
      <div className="space-y-3">
        {grouped.map(({ category, items }) => {
          const allChecked = items.every((item) => value.includes(item.key))
          return (
            <div key={category} className="rounded-lg border">
              <div className="flex items-center justify-between gap-3 border-b px-3 py-2">
                <label className="flex items-center gap-2 font-medium">
                  <Checkbox checked={allChecked} onCheckedChange={(next) => toggleCategory(items, next === true)} />
                  {category}
                </label>
                <span className="text-xs text-muted-foreground">{items.filter((item) => value.includes(item.key)).length}/{items.length}</span>
              </div>
              <div className="grid gap-2 p-3 md:grid-cols-2">
                {items.map((item) => (
                  <label key={item.key} className="flex min-h-16 items-start gap-3 rounded-md border px-3 py-2">
                    <Checkbox checked={value.includes(item.key)} onCheckedChange={(next) => toggle(item.key, next === true)} />
                    <span className="min-w-0">
                      <span className="block text-sm font-medium">{item.label}</span>
                      <span className="line-clamp-2 text-xs text-muted-foreground">{item.description}</span>
                    </span>
                  </label>
                ))}
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}

function PermissionLimitEditor({ value, onChange }: { value: PermissionLimits; onChange: (value: PermissionLimits) => void }) {
  function update(key: keyof PermissionLimits, raw: string) {
    const next = Number(raw)
    onChange({ ...value, [key]: Number.isFinite(next) && next > 0 ? Math.floor(next) : 0 })
  }
  return (
    <div className="space-y-3 rounded-lg border p-3">
      <div className="flex flex-col gap-1 sm:flex-row sm:items-center sm:justify-between">
        <Label>账号配额</Label>
        <span className="text-xs text-muted-foreground">填 0 表示不限制</span>
      </div>
      <div className="grid gap-3 md:grid-cols-3">
        <div className="space-y-2">
          <Label>邮箱数量上限</Label>
          <Input type="number" min={0} value={value.maxMailboxCount} onChange={(event) => update("maxMailboxCount", event.target.value)} />
        </div>
        <div className="space-y-2">
          <Label>附件上限 MB</Label>
          <Input type="number" min={0} value={value.maxAttachmentMb} onChange={(event) => update("maxAttachmentMb", event.target.value)} />
        </div>
        <div className="space-y-2">
          <Label>SMTP 每日封数</Label>
          <Input type="number" min={0} value={value.smtpDailyLimit} onChange={(event) => update("smtpDailyLimit", event.target.value)} />
        </div>
        <div className="space-y-2">
          <Label>SMTP 每分钟封数</Label>
          <Input type="number" min={0} value={value.smtpMinuteLimit} onChange={(event) => update("smtpMinuteLimit", event.target.value)} />
        </div>
        <div className="space-y-2">
          <Label>IMAP 每分钟命令数</Label>
          <Input type="number" min={0} value={value.imapMinuteLimit} onChange={(event) => update("imapMinuteLimit", event.target.value)} />
        </div>
        <div className="space-y-2">
          <Label>POP3 每分钟命令数</Label>
          <Input type="number" min={0} value={value.pop3MinuteLimit} onChange={(event) => update("pop3MinuteLimit", event.target.value)} />
        </div>
      </div>
    </div>
  )
}

function PermissionBadges({ permissions, catalog }: { permissions: PermissionKey[]; catalog: PermissionInfo[] }) {
  const labelByKey = new Map(catalog.map((item) => [item.key, item.label]))
  if (permissions.length === 0) return <div className="mt-3 text-sm text-muted-foreground">无后台权限</div>
  return (
    <div className="mt-3 flex flex-wrap gap-1.5">
      {permissions.slice(0, 10).map((permission) => (
        <Badge key={permission} variant="outline" className="font-normal">{labelByKey.get(permission) || permission}</Badge>
      ))}
      {permissions.length > 10 && <Badge variant="secondary">+{permissions.length - 10}</Badge>}
    </div>
  )
}

function PermissionLimitBadges({ limits }: { limits?: PermissionLimits }) {
  const defaultLimitsQuery = useQuery({ queryKey: ["admin", "permission-limits", "defaults"], queryFn: api.defaultPermissionLimits })
  const value = limits || defaultLimitsQuery.data || defaultPermissionLimits
  return (
    <div className="mt-3 flex flex-wrap gap-1.5">
      <Badge variant="secondary" className="font-normal">附件 {limitText(value.maxAttachmentMb, "MB")}</Badge>
      <Badge variant="secondary" className="font-normal">邮箱 {limitText(value.maxMailboxCount, "个")}</Badge>
      <Badge variant="secondary" className="font-normal">SMTP 每日 {limitText(value.smtpDailyLimit, "封")}</Badge>
      <Badge variant="secondary" className="font-normal">SMTP 每分钟 {limitText(value.smtpMinuteLimit, "封")}</Badge>
      <Badge variant="secondary" className="font-normal">IMAP 每分钟 {limitText(value.imapMinuteLimit, "次")}</Badge>
      <Badge variant="secondary" className="font-normal">POP3 每分钟 {limitText(value.pop3MinuteLimit, "次")}</Badge>
    </div>
  )
}

function limitText(value: number, unit: string) {
  return value > 0 ? `${value} ${unit}` : "不限"
}

function groupPermissionCatalog(catalog: PermissionInfo[]) {
  const order: string[] = []
  const grouped = new Map<string, PermissionInfo[]>()
  for (const item of catalog) {
    if (!grouped.has(item.category)) {
      grouped.set(item.category, [])
      order.push(item.category)
    }
    grouped.get(item.category)!.push(item)
  }
  return order.map((category) => ({ category, items: grouped.get(category)! }))
}

function DomainsSection({ domains }: { domains: Domain[] }) {
  const me = useMe()
  const user = me.data?.user
  const qc = useQueryClient()
  const { toast } = useToast()
  const [pendingConfirm, setPendingConfirm] = React.useState<PendingConfirm | null>(null)
  const canCreate = hasPermission(user, "admin.domains.create")
  const canUpdate = hasPermission(user, "admin.domains.update")
  const canDelete = hasPermission(user, "admin.domains.delete")
  const canViewDNS = hasPermission(user, "admin.dns.view")
  const update = useMutation({ mutationFn: ({ id, status }: { id: string; status: string }) => api.updateDomain(id, { status }), onSuccess: () => { invalidateAdmin(qc); toast({ title: "域名已更新" }) }, onError: (e) => toast({ title: "更新失败", description: e.message }) })
  const remove = useMutation({ mutationFn: api.deleteDomain, onSuccess: () => { setPendingConfirm(null); invalidateAdmin(qc); toast({ title: "域名已删除" }) }, onError: (e) => toast({ title: "删除失败", description: e.message }) })
  return (
    <Card>
      <CardHeader>
        <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
          <CardTitle>域名管理</CardTitle>
          {canCreate && <CreateDomainDialog />}
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        {domains.map((domain) => (
          <div key={domain.id} className="flex flex-col gap-3 rounded-lg border p-4 md:flex-row md:items-center md:justify-between">
            <div>
              <div className="font-medium">{domain.name}</div>
              <div className="text-xs text-muted-foreground">selector: {domain.dkimSelector}</div>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <Badge variant={domain.status === "active" ? "default" : "secondary"}>{domain.status === "active" ? "启用" : "停用"}</Badge>
              <Badge variant={domain.dnsStatus === "ok" ? "default" : "secondary"}>{domain.dnsStatus === "ok" ? "DNS 正常" : domain.dnsStatus}</Badge>
              {canViewDNS && <DomainDNSDialog domain={domain} />}
              {canUpdate && <Button variant="outline" size="sm" onClick={() => update.mutate({ id: domain.id, status: domain.status === "active" ? "disabled" : "active" })}>{domain.status === "active" ? "停用" : "启用"}</Button>}
              {canDelete && <Button variant="outline" size="sm" onClick={() => setPendingConfirm({ title: "删除域名？", description: `将删除 ${domain.name}，相关邮箱、转发和邮件也可能受影响。`, confirmText: "删除域名", onConfirm: () => remove.mutate(domain.id) })}><Trash2 className="h-4 w-4" />删除</Button>}
            </div>
          </div>
        ))}
        {domains.length === 0 && <Empty text="暂无域名" />}
      </CardContent>
      <ConfirmDialog open={!!pendingConfirm} title={pendingConfirm?.title || ""} description={pendingConfirm?.description} confirmText={pendingConfirm?.confirmText || "删除"} destructive pending={remove.isPending} onOpenChange={(open) => { if (!open) setPendingConfirm(null) }} onConfirm={() => pendingConfirm?.onConfirm()} />
    </Card>
  )
}

function DomainDNSDialog({ domain }: { domain: Domain }) {
  return (
    <Dialog>
      <DialogTrigger asChild>
        <Button variant="outline" size="sm">DNS</Button>
      </DialogTrigger>
      <DialogContent className="max-h-[86vh] overflow-y-auto sm:max-w-3xl">
        <DialogHeader><DialogTitle>{domain.name} DNS</DialogTitle></DialogHeader>
        <DNSPanel domain={domain} embedded />
      </DialogContent>
    </Dialog>
  )
}

function MailboxesSection({ mailboxes, users, domains }: { mailboxes: MailboxType[]; users: AdminUser[]; domains: Domain[] }) {
  const me = useMe()
  const user = me.data?.user
  const qc = useQueryClient()
  const { toast } = useToast()
  const [pendingConfirm, setPendingConfirm] = React.useState<PendingConfirm | null>(null)
  const canCreate = hasPermission(user, "admin.mailboxes.create")
  const canUpdate = hasPermission(user, "admin.mailboxes.update")
  const canDelete = hasPermission(user, "admin.mailboxes.delete")
  const remove = useMutation({ mutationFn: api.deleteMailbox, onSuccess: () => { setPendingConfirm(null); invalidateAdmin(qc); toast({ title: "邮箱已删除" }) }, onError: (e) => toast({ title: "删除失败", description: e.message }) })
  return (
    <Card>
      <CardHeader>
        <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
          <CardTitle>邮箱管理</CardTitle>
          {canCreate && <CreateMailboxDialog domains={domains} users={users} />}
        </div>
      </CardHeader>
      <CardContent>
        <div className="space-y-3 md:hidden">
          {mailboxes.map((mailbox) => (
            <div key={mailbox.id} className="rounded-lg border p-4">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="truncate font-medium">{mailbox.address}</div>
                  <div className="truncate text-xs text-muted-foreground">{mailbox.userEmail || mailbox.userId}</div>
                </div>
                <MailboxActions mailbox={mailbox} users={users} canUpdate={canUpdate} onDelete={canDelete ? () => setPendingConfirm({ title: "删除邮箱？", description: `将删除 ${mailbox.address} 和其中邮件。`, confirmText: "删除邮箱", onConfirm: () => remove.mutate(mailbox.id) }) : undefined} />
              </div>
              <div className="mt-3 flex flex-wrap gap-2">
                <Badge variant={mailbox.status === "active" ? "default" : "secondary"}>{mailbox.status === "active" ? "启用" : "停用"}</Badge>
                <Badge variant="outline">{mailbox.quotaMb} MB</Badge>
                <Badge variant="outline">{mailbox.displayName || "未命名"}</Badge>
              </div>
            </div>
          ))}
        </div>
        <div className="hidden md:block">
          <Table>
            <TableHeader><TableRow><TableHead>地址</TableHead><TableHead>归属账号</TableHead><TableHead>名称</TableHead><TableHead>配额</TableHead><TableHead>状态</TableHead><TableHead className="w-16"></TableHead></TableRow></TableHeader>
            <TableBody>
              {mailboxes.map((mailbox) => (
                <TableRow key={mailbox.id}>
                  <TableCell className="font-medium">{mailbox.address}</TableCell>
                  <TableCell className="text-muted-foreground">{mailbox.userEmail || mailbox.userId}</TableCell>
                  <TableCell>{mailbox.displayName}</TableCell>
                  <TableCell>{mailbox.quotaMb} MB</TableCell>
                  <TableCell><Badge variant={mailbox.status === "active" ? "default" : "secondary"}>{mailbox.status === "active" ? "启用" : "停用"}</Badge></TableCell>
                  <TableCell><MailboxActions mailbox={mailbox} users={users} canUpdate={canUpdate} onDelete={canDelete ? () => setPendingConfirm({ title: "删除邮箱？", description: `将删除 ${mailbox.address} 和其中邮件。`, confirmText: "删除邮箱", onConfirm: () => remove.mutate(mailbox.id) }) : undefined} /></TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
        {mailboxes.length === 0 && <Empty text="暂无邮箱" />}
      </CardContent>
      <ConfirmDialog open={!!pendingConfirm} title={pendingConfirm?.title || ""} description={pendingConfirm?.description} confirmText={pendingConfirm?.confirmText || "删除"} destructive pending={remove.isPending} onOpenChange={(open) => { if (!open) setPendingConfirm(null) }} onConfirm={() => pendingConfirm?.onConfirm()} />
    </Card>
  )
}

function AliasesSection({ aliases, domains }: { aliases: Alias[]; domains: Domain[] }) {
  const me = useMe()
  const user = me.data?.user
  const qc = useQueryClient()
  const { toast } = useToast()
  const [pendingConfirm, setPendingConfirm] = React.useState<PendingConfirm | null>(null)
  const canCreate = hasPermission(user, "admin.aliases.create")
  const canUpdate = hasPermission(user, "admin.aliases.update")
  const canDelete = hasPermission(user, "admin.aliases.delete")
  const update = useMutation({ mutationFn: ({ id, payload }: { id: string; payload: { source: string; destination: string; enabled: boolean } }) => api.updateAlias(id, payload), onSuccess: () => { invalidateAdmin(qc); toast({ title: "转发已更新" }) }, onError: (e) => toast({ title: "更新失败", description: e.message }) })
  const remove = useMutation({ mutationFn: api.deleteAlias, onSuccess: () => { setPendingConfirm(null); invalidateAdmin(qc); toast({ title: "转发已删除" }) }, onError: (e) => toast({ title: "删除失败", description: e.message }) })
  return (
    <Card>
      <CardHeader>
        <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
          <CardTitle>邮件转发</CardTitle>
          {canCreate && <CreateAliasDialog domains={domains} />}
        </div>
      </CardHeader>
      <CardContent>
        <div className="space-y-3 md:hidden">
          {aliases.map((alias) => (
            <div key={alias.id} className="rounded-lg border p-4">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="truncate font-medium">{alias.source}</div>
                  <div className="truncate text-xs text-muted-foreground">{alias.destination}</div>
                </div>
                <AliasActions alias={alias} onToggle={canUpdate ? () => update.mutate({ id: alias.id, payload: { source: alias.source, destination: alias.destination, enabled: !alias.enabled } }) : undefined} onDelete={canDelete ? () => setPendingConfirm({ title: "删除转发？", description: `${alias.source} 将不再转发到 ${alias.destination}。`, confirmText: "删除转发", onConfirm: () => remove.mutate(alias.id) }) : undefined} />
              </div>
              <div className="mt-3 flex flex-wrap gap-2">
                <Badge variant={alias.enabled ? "default" : "secondary"}>{alias.enabled ? "启用" : "停用"}</Badge>
                <Badge variant="outline">{domains.find((d) => d.id === alias.domainId)?.name || alias.domainId}</Badge>
              </div>
            </div>
          ))}
        </div>
        <div className="hidden md:block">
          <Table>
            <TableHeader><TableRow><TableHead>来源</TableHead><TableHead>目标</TableHead><TableHead>域名</TableHead><TableHead>状态</TableHead><TableHead className="w-16"></TableHead></TableRow></TableHeader>
            <TableBody>
              {aliases.map((alias) => (
                <TableRow key={alias.id}>
                  <TableCell className="font-medium">{alias.source}</TableCell>
                  <TableCell>{alias.destination}</TableCell>
                  <TableCell className="text-muted-foreground">{domains.find((d) => d.id === alias.domainId)?.name || alias.domainId}</TableCell>
                  <TableCell><Badge variant={alias.enabled ? "default" : "secondary"}>{alias.enabled ? "启用" : "停用"}</Badge></TableCell>
                  <TableCell><AliasActions alias={alias} onToggle={canUpdate ? () => update.mutate({ id: alias.id, payload: { source: alias.source, destination: alias.destination, enabled: !alias.enabled } }) : undefined} onDelete={canDelete ? () => setPendingConfirm({ title: "删除转发？", description: `${alias.source} 将不再转发到 ${alias.destination}。`, confirmText: "删除转发", onConfirm: () => remove.mutate(alias.id) }) : undefined} /></TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
        {aliases.length === 0 && <Empty text="暂无邮件转发" />}
      </CardContent>
      <ConfirmDialog open={!!pendingConfirm} title={pendingConfirm?.title || ""} description={pendingConfirm?.description} confirmText={pendingConfirm?.confirmText || "删除"} destructive pending={remove.isPending} onOpenChange={(open) => { if (!open) setPendingConfirm(null) }} onConfirm={() => pendingConfirm?.onConfirm()} />
    </Card>
  )
}

function AdminMessagesSection({ mailboxes, systemAdmin }: { mailboxes: MailboxType[]; systemAdmin: boolean }) {
  const [query, setQuery] = React.useState("")
  const [mailboxId, setMailboxId] = React.useState("all")
  const [folder, setFolder] = React.useState("all")
  const [selectedId, setSelectedId] = React.useState<string | null>(null)
  const messages = useInfiniteQuery({
    queryKey: ["admin", "messages", mailboxId, folder, query],
    queryFn: ({ pageParam }) => api.adminMessages({
      mailboxId: mailboxId === "all" ? "" : mailboxId,
      folder: folder === "all" ? "" : folder,
      q: query,
      cursor: typeof pageParam === "string" ? pageParam : "",
    }),
    initialPageParam: "",
    getNextPageParam: (lastPage) => lastPage.nextCursor || undefined,
  })
  const detail = useQuery({ queryKey: ["admin", "message", selectedId], queryFn: () => api.adminMessage(selectedId!), enabled: !!selectedId })
  const items = messages.data?.pages.flatMap((page) => page.items || []) || []
  return (
    <Card>
      <CardHeader>
        <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
          <CardTitle>全部邮件</CardTitle>
          <Button variant="outline" size="sm" onClick={() => messages.refetch()} disabled={messages.isFetching}>
            <RefreshCcw className={cn("h-4 w-4", messages.isFetching && "animate-spin")} />{messages.isFetching ? "刷新中" : "刷新"}
          </Button>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex flex-col gap-3 xl:flex-row">
          <div className="relative flex-1">
            <Search className="absolute left-3 top-2.5 h-4 w-4 text-muted-foreground" />
            <Input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索主题、发件人、收件人、邮箱" className="pl-9" />
          </div>
          <Select value={mailboxId} onValueChange={setMailboxId}>
            <SelectTrigger className="xl:w-72"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部邮箱</SelectItem>
              {systemAdmin && <SelectItem value="unregistered">未知收件</SelectItem>}
              {mailboxes.map((mailbox) => <SelectItem key={mailbox.id} value={mailbox.id}>{mailbox.address}</SelectItem>)}
            </SelectContent>
          </Select>
          <Select value={folder} onValueChange={setFolder}>
            <SelectTrigger className="xl:w-40"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部文件夹</SelectItem>
              <SelectItem value="Inbox">收件箱</SelectItem>
              <SelectItem value="Sent">已发送</SelectItem>
              <SelectItem value="Archive">归档</SelectItem>
              <SelectItem value="Spam">垃圾邮件</SelectItem>
              <SelectItem value="Trash">回收站</SelectItem>
              {systemAdmin && <SelectItem value="Unregistered">未知收件</SelectItem>}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-3 md:hidden">
          {items.map((message) => (
            <div key={message.id} className="rounded-lg border p-4">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="truncate font-medium">{message.subject}</div>
                  <div className="mt-1 line-clamp-2 text-xs text-muted-foreground">{message.snippet}</div>
                </div>
                <Button variant="ghost" size="sm" onClick={() => setSelectedId(message.id)}>查看</Button>
              </div>
              <div className="mt-3 space-y-2 text-sm">
                <div className="truncate text-muted-foreground">邮箱：{message.mailboxAddress || message.recipientAddress || "-"}</div>
                <div className="truncate text-muted-foreground">发件人：{adminSenderDisplayName(message)}</div>
                <div className="truncate text-muted-foreground">收件人：{message.recipientAddress || message.to?.join(", ") || ""}</div>
              </div>
              <div className="mt-3 flex flex-wrap gap-2">
                <Badge variant="secondary">{folderName(message.folder)}</Badge>
                <Badge variant="outline">{formatDate(message.receivedAt)}</Badge>
              </div>
            </div>
          ))}
        </div>
        <div className="hidden md:block">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>邮件</TableHead>
                <TableHead>邮箱</TableHead>
                <TableHead>发件人</TableHead>
                <TableHead>收件人</TableHead>
                <TableHead>文件夹</TableHead>
                <TableHead>时间</TableHead>
                <TableHead className="w-20"></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((message) => (
                <TableRow key={message.id}>
                  <TableCell className="max-w-[360px]">
                    <div className="truncate font-medium">{message.subject}</div>
                    <div className="truncate text-xs text-muted-foreground">{message.snippet}</div>
                  </TableCell>
                  <TableCell>
                    <div className="font-medium">{message.mailboxAddress || message.recipientAddress || "-"}</div>
                    {message.ownerEmail && <div className="text-xs text-muted-foreground">{message.ownerEmail}</div>}
                  </TableCell>
                  <TableCell className="max-w-[220px] truncate" title={adminSenderTitle(message)}>{adminSenderDisplayName(message)}</TableCell>
                  <TableCell className="max-w-[220px] truncate">{message.recipientAddress || message.to?.join(", ") || ""}</TableCell>
                  <TableCell><Badge variant="secondary">{folderName(message.folder)}</Badge></TableCell>
                  <TableCell className="text-muted-foreground">{formatDate(message.receivedAt)}</TableCell>
                  <TableCell><Button variant="ghost" size="sm" onClick={() => setSelectedId(message.id)}>查看</Button></TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
        {messages.isLoading && <Empty text="加载中..." />}
        {!messages.isLoading && items.length === 0 && <Empty text="暂无邮件" />}
        {!messages.isLoading && messages.hasNextPage && (
          <div className="flex justify-center">
            <Button variant="outline" size="sm" disabled={messages.isFetchingNextPage} onClick={() => messages.fetchNextPage()}>
              {messages.isFetchingNextPage ? "加载中..." : "加载更多"}
            </Button>
          </div>
        )}
      </CardContent>
      <AdminMessageDialog message={detail.data} loading={detail.isLoading} open={!!selectedId} onOpenChange={(open) => { if (!open) setSelectedId(null) }} />
    </Card>
  )
}

function AdminSendAuditSection({ mailboxes }: { mailboxes: MailboxType[] }) {
  const [mailboxId, setMailboxId] = React.useState("all")
  const [event, setEvent] = React.useState("all")
  const [messageId, setMessageId] = React.useState("")
  const [from, setFrom] = React.useState("")
  const [to, setTo] = React.useState("")
  const audit = useInfiniteQuery({
    queryKey: ["admin", "send-audit", mailboxId, event, messageId, from, to],
    queryFn: ({ pageParam }) => api.adminSendAudit({
      mailboxId: mailboxId === "all" ? "" : mailboxId,
      event: event === "all" ? "" : event,
      messageId: messageId.trim(),
      from,
      to,
      cursor: typeof pageParam === "string" ? pageParam : "",
    }),
    initialPageParam: "",
    getNextPageParam: (lastPage) => lastPage.nextCursor || undefined,
  })
  const items = audit.data?.pages.flatMap((page) => page.items || []) || []
  return (
    <Card>
      <CardHeader>
        <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
          <CardTitle className="flex items-center gap-2"><ClipboardList className="h-5 w-5" />发送队列</CardTitle>
          <Button variant="outline" size="sm" onClick={() => audit.refetch()} disabled={audit.isFetching}>
            <RefreshCcw className={cn("h-4 w-4", audit.isFetching && "animate-spin")} />{audit.isFetching ? "刷新中" : "刷新"}
          </Button>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid gap-3 lg:grid-cols-[minmax(0,1fr)_180px_180px_160px_160px]">
          <div className="relative">
            <Search className="absolute left-3 top-2.5 h-4 w-4 text-muted-foreground" />
            <Input value={messageId} onChange={(event) => setMessageId(event.target.value)} placeholder="Message-ID 或已发送邮件 ID" className="pl-9" />
          </div>
          <Select value={mailboxId} onValueChange={setMailboxId}>
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部邮箱</SelectItem>
              {mailboxes.map((mailbox) => <SelectItem key={mailbox.id} value={mailbox.id}>{mailbox.address}</SelectItem>)}
            </SelectContent>
          </Select>
          <Select value={event} onValueChange={setEvent}>
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部事件</SelectItem>
              {sendAuditEvents.map((item) => <SelectItem key={item} value={item}>{sendAuditEventLabel(item)}</SelectItem>)}
            </SelectContent>
          </Select>
          <Input type="date" value={from} onChange={(event) => setFrom(event.target.value)} aria-label="开始日期" />
          <Input type="date" value={to} onChange={(event) => setTo(event.target.value)} aria-label="结束日期" />
        </div>
        <div className="space-y-3 md:hidden">
          {items.map((item) => (
            <div key={item.id} className="rounded-lg border p-4">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="font-medium">{sendAuditEventLabel(item.event || "")}</div>
                  <div className="mt-1 truncate text-xs text-muted-foreground">{item.mailboxAddress || item.mailboxId || "-"}</div>
                </div>
                <Badge variant={sendAuditBadgeVariant(item.event)}>{item.status || item.event || "-"}</Badge>
              </div>
              <div className="mt-3 space-y-2 text-sm text-muted-foreground">
                <div className="truncate">收件人：{(item.recipients || []).join(", ") || "-"}</div>
                <div className="truncate">Message-ID：{item.messageId || item.sentMessageId || "-"}</div>
                {item.error && <div className="line-clamp-2 text-destructive">错误：{item.error}</div>}
              </div>
              <div className="mt-3 text-xs text-muted-foreground">{formatDate(item.createdAt)}</div>
            </div>
          ))}
        </div>
        <div className="hidden md:block">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>事件</TableHead>
                <TableHead>邮箱</TableHead>
                <TableHead>收件人</TableHead>
                <TableHead>Message-ID</TableHead>
                <TableHead>错误</TableHead>
                <TableHead>时间</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((item) => (
                <TableRow key={item.id}>
                  <TableCell><Badge variant={sendAuditBadgeVariant(item.event)}>{sendAuditEventLabel(item.event || "")}</Badge></TableCell>
                  <TableCell className="max-w-[220px] truncate">{item.mailboxAddress || item.mailboxId || "-"}</TableCell>
                  <TableCell className="max-w-[260px] truncate" title={(item.recipients || []).join(", ")}>{(item.recipients || []).join(", ") || "-"}</TableCell>
                  <TableCell className="max-w-[240px] truncate" title={item.messageId || item.sentMessageId || ""}>{item.messageId || item.sentMessageId || "-"}</TableCell>
                  <TableCell className="max-w-[260px] truncate text-destructive" title={item.error || ""}>{item.error || "-"}</TableCell>
                  <TableCell className="whitespace-nowrap text-muted-foreground">{formatDate(item.createdAt)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
        {audit.isLoading && <Empty text="加载中..." />}
        {!audit.isLoading && items.length === 0 && <Empty text="暂无发送记录" />}
        {!audit.isLoading && audit.hasNextPage && (
          <div className="flex justify-center">
            <Button variant="outline" size="sm" disabled={audit.isFetchingNextPage} onClick={() => audit.fetchNextPage()}>
              {audit.isFetchingNextPage ? "加载中..." : "加载更多"}
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function SystemSettingsSection({ settings, domains }: { settings?: SystemSettings; domains: Domain[] }) {
  const me = useMe()
  const user = me.data?.user
  const qc = useQueryClient()
  const { toast } = useToast()
  const canSettingsView = hasPermission(user, "admin.settings.view")
  const canUpdateSettings = hasPermission(user, "admin.settings.update")
  const canTestSMTP = hasPermission(user, "admin.settings.test_smtp")
  const canViewTemplates = hasPermission(user, "admin.templates.view")
  const canUpdateTemplates = hasPermission(user, "admin.templates.update")
  const canResetTemplates = hasPermission(user, "admin.templates.reset")
  const templates = useQuery({ queryKey: ["admin", "mail-templates"], queryFn: api.mailTemplates, enabled: canViewTemplates })
  const [settingsTab, setSettingsTab] = React.useState<"base" | "smtp" | "storage" | "mail" | "externalImap" | "templates" | "security" | "about">("base")
  const maildirHealth = useQuery({ queryKey: ["admin", "maildir-sync", "health"], queryFn: api.maildirSyncHealth, enabled: canSettingsView && settingsTab === "storage" })
  const [smtpRequireTls, setSmtpRequireTls] = React.useState(false)
  const [allowInsecureHttp, setAllowInsecureHttp] = React.useState(true)
  const [openRegistration, setOpenRegistration] = React.useState(false)
  const [twoFactorEnabled, setTwoFactorEnabled] = React.useState(false)
  const [turnstileEnabled, setTurnstileEnabled] = React.useState(false)
  const [catchAllEnabled, setCatchAllEnabled] = React.useState(false)
  const [mailAutoRefresh, setMailAutoRefresh] = React.useState(true)
  const [userMailboxApplyEnabled, setUserMailboxApplyEnabled] = React.useState(false)
  const [userMailboxDomainIds, setUserMailboxDomainIds] = React.useState<string[]>([])
  const [externalImapEnabled, setExternalImapEnabled] = React.useState(false)
  const [externalImapAllowPrivateHosts, setExternalImapAllowPrivateHosts] = React.useState(false)
  React.useEffect(() => {
    if (!settings) return
    setSmtpRequireTls(settings.smtpRequireTls)
    setAllowInsecureHttp(settings.allowInsecureHttp)
    setOpenRegistration(settings.openRegistration)
    setTwoFactorEnabled(settings.twoFactorEnabled)
    setTurnstileEnabled(settings.turnstileEnabled)
    setCatchAllEnabled(settings.catchAllEnabled)
    setMailAutoRefresh(settings.mailAutoRefresh)
    setUserMailboxApplyEnabled(settings.userMailboxApplyEnabled)
    setUserMailboxDomainIds(settings.userMailboxDomainIds || [])
    setExternalImapEnabled(settings.externalImapEnabled)
    setExternalImapAllowPrivateHosts(settings.externalImapAllowPrivateHosts)
  }, [settings])
  const save = useMutation({
    mutationFn: (form: FormData) => api.updateSystemSettings({
      publicHostname: fieldValue(form, "publicHostname", settings?.publicHostname || ""),
      publicBaseUrl: fieldValue(form, "publicBaseUrl", settings?.publicBaseUrl || ""),
      smtpHost: fieldValue(form, "smtpHost", settings?.smtpHost || ""),
      smtpPort: fieldValue(form, "smtpPort", settings?.smtpPort || "25"),
      smtpUsername: fieldValue(form, "smtpUsername", settings?.smtpUsername || ""),
      smtpPassword: fieldValue(form, "smtpPassword", ""),
      smtpRequireTls,
      maildirRoot: fieldValue(form, "maildirRoot", settings?.maildirRoot || ""),
      maildirScanSeconds: fieldNumber(form, "maildirScanSeconds", settings?.maildirScanSeconds || 30),
      sessionTtlHours: fieldNumber(form, "sessionTtlHours", settings?.sessionTtlHours || 168),
      allowInsecureHttp,
      openRegistration,
      twoFactorEnabled,
      turnstileEnabled,
      turnstileSiteKey: fieldValue(form, "turnstileSiteKey", settings?.turnstileSiteKey || ""),
      turnstileSecretKey: fieldValue(form, "turnstileSecretKey", ""),
      catchAllEnabled,
      mailAutoRefresh,
      mailRefreshSeconds: fieldNumber(form, "mailRefreshSeconds", settings?.mailRefreshSeconds || 30),
      userMailboxApplyEnabled,
      userMailboxDomainIds,
      reservedMailboxPrefixes: fieldValue(form, "reservedMailboxPrefixes", settings?.reservedMailboxPrefixes || ""),
      externalImapEnabled,
      externalImapSecretKey: fieldValue(form, "externalImapSecretKey", ""),
      externalImapSyncSeconds: fieldNumber(form, "externalImapSyncSeconds", settings?.externalImapSyncSeconds || 300),
      externalImapAllowPrivateHosts,
      externalImapGmailClientId: fieldValue(form, "externalImapGmailClientId", settings?.externalImapGmailClientId || ""),
      externalImapGmailClientSecret: fieldValue(form, "externalImapGmailClientSecret", ""),
      externalImapOutlookClientId: fieldValue(form, "externalImapOutlookClientId", settings?.externalImapOutlookClientId || ""),
      externalImapOutlookClientSecret: fieldValue(form, "externalImapOutlookClientSecret", ""),
    }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["admin", "settings"] })
      qc.invalidateQueries({ queryKey: ["admin", "maildir-sync", "health"] })
      qc.invalidateQueries({ queryKey: ["dns-records"] })
      qc.invalidateQueries({ queryKey: ["public-settings"] })
      toast({ title: "系统设置已保存" })
    },
    onError: (e) => toast({ title: "保存失败", description: e.message }),
  })
  const formKey = settings ? [
    settings.publicHostname,
    settings.publicBaseUrl,
    settings.smtpHost,
    settings.smtpPort,
    settings.smtpUsername,
    settings.smtpPasswordSet,
    settings.smtpRequireTls,
    settings.maildirRoot,
    settings.maildirScanSeconds,
    settings.sessionTtlHours,
    settings.allowInsecureHttp,
    settings.openRegistration,
    settings.twoFactorEnabled,
    settings.turnstileEnabled,
    settings.turnstileSiteKey,
    settings.turnstileSecretSet,
    settings.catchAllEnabled,
    settings.mailAutoRefresh,
    settings.mailRefreshSeconds,
    settings.userMailboxApplyEnabled,
    (settings.userMailboxDomainIds || []).join(","),
    settings.reservedMailboxPrefixes,
    settings.externalImapEnabled,
    settings.externalImapSecretSet,
    settings.externalImapSyncSeconds,
    settings.externalImapAllowPrivateHosts,
    settings.externalImapGmailClientId,
    settings.externalImapGmailClientSecretSet,
    settings.externalImapOutlookClientId,
    settings.externalImapOutlookClientSecretSet,
  ].join("|") : "loading"
  const tabs: { key: typeof settingsTab; label: string }[] = [
    ...(canSettingsView ? [
      { key: "base" as const, label: "基础" },
      { key: "smtp" as const, label: "SMTP" },
      { key: "storage" as const, label: "存储" },
      { key: "mail" as const, label: "邮件" },
      { key: "externalImap" as const, label: "外部 IMAP" },
    ] : []),
    ...(canViewTemplates ? [{ key: "templates" as const, label: "模板" }] : []),
    ...(canSettingsView ? [{ key: "security" as const, label: "安全" }] : []),
    { key: "about", label: "关于" },
  ]
  React.useEffect(() => {
    if (tabs.some((tab) => tab.key === settingsTab)) return
    setSettingsTab(tabs[0]?.key || "about")
  }, [settingsTab, tabs])
  return (
    <form key={formKey} onSubmit={(event) => { event.preventDefault(); if (canUpdateSettings) save.mutate(new FormData(event.currentTarget)) }} className="space-y-6">
      <div className="flex flex-wrap gap-2 rounded-lg border bg-card p-2">
        {tabs.map((tab) => (
          <Button key={tab.key} type="button" variant={settingsTab === tab.key ? "default" : "ghost"} size="sm" onClick={() => setSettingsTab(tab.key)}>
            {tab.label}
          </Button>
        ))}
      </div>

      {settingsTab === "base" && <Card>
        <CardHeader><CardTitle>基础设置</CardTitle></CardHeader>
        <CardContent className="grid gap-4 md:grid-cols-2">
          <Field name="publicHostname" label="公网主机名" defaultValue={settings?.publicHostname || ""} placeholder="mail.example.com" />
          <Field name="publicBaseUrl" label="访问地址" defaultValue={settings?.publicBaseUrl || ""} placeholder="https://mail.example.com" required={false} />
          <Field name="sessionTtlHours" label="登录有效期小时" type="number" defaultValue={String(settings?.sessionTtlHours || 168)} />
          <Field name="maildirScanSeconds" label="Maildir 扫描秒数" type="number" defaultValue={String(settings?.maildirScanSeconds || 30)} />
          <SwitchRow label="允许 HTTP 调试" checked={allowInsecureHttp} onCheckedChange={setAllowInsecureHttp} className="md:col-span-2" />
        </CardContent>
      </Card>}

      {settingsTab === "smtp" && <Card>
        <CardHeader>
          <div className="flex items-center justify-between gap-3">
            <div>
              <CardTitle>发信通道</CardTitle>
            </div>
            {canTestSMTP && <TestSMTPDialog disabled={!settings} />}
          </div>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="rounded-lg border bg-muted/30 p-4 text-sm text-muted-foreground">
            <div className="font-medium text-foreground">当前默认：内置 Postfix</div>
            <div>Host 填 127.0.0.1、端口 25、用户名/密码留空、强制 TLS 关闭。这里的“强制 TLS”仅用于外部 SMTP 中继（587 STARTTLS 或 465 TLS）。</div>
          </div>
          <div className="grid gap-4 md:grid-cols-2">
            <Field name="smtpHost" label="发信主机" defaultValue={settings?.smtpHost || ""} placeholder="127.0.0.1" required={false} />
            <Field name="smtpPort" label="发信端口" defaultValue={settings?.smtpPort || "25"} />
            <Field name="smtpUsername" label="中继用户名（内置 Postfix 留空）" defaultValue={settings?.smtpUsername || ""} required={false} />
            <Field name="smtpPassword" label={settings?.smtpPasswordSet ? "中继密码（留空不变）" : "中继密码（内置 Postfix 留空）"} type="password" required={false} />
            <SwitchRow label="外部中继强制 TLS" checked={smtpRequireTls} onCheckedChange={setSmtpRequireTls} className="md:col-span-2" />
          </div>
        </CardContent>
      </Card>}

      {settingsTab === "storage" && <div className="space-y-6">
        <Card>
          <CardHeader><CardTitle>存储设置</CardTitle></CardHeader>
          <CardContent>
            <Field name="maildirRoot" label="Maildir 根目录" defaultValue={settings?.maildirRoot || ""} required={false} />
          </CardContent>
        </Card>
        <MaildirSyncHealthCard health={maildirHealth.data} loading={maildirHealth.isLoading} error={maildirHealth.error} onRefresh={() => maildirHealth.refetch()} refreshing={maildirHealth.isFetching} fallbackRoot={settings?.maildirRoot || ""} />
      </div>}

      {settingsTab === "mail" && <Card>
        <CardHeader><CardTitle>邮件设置</CardTitle></CardHeader>
        <CardContent className="space-y-5">
          <SwitchRow label="无人收件" checked={catchAllEnabled} onCheckedChange={setCatchAllEnabled} />
          <Separator />
          <SwitchRow label="账号自助申请邮箱" checked={userMailboxApplyEnabled} onCheckedChange={setUserMailboxApplyEnabled} />
          {userMailboxApplyEnabled && (
            <div className="space-y-5 border-t pt-5">
              <div className="space-y-3">
                <Label>开放域名</Label>
                <div className="grid gap-2 md:grid-cols-2">
                  {domains.map((domain) => {
                    const checked = userMailboxDomainIds.includes(domain.id)
                    const disabled = domain.status !== "active"
                    return (
                      <label key={domain.id} className={cn("flex min-h-11 items-center gap-3 rounded-md border px-3 py-2", disabled && "cursor-not-allowed opacity-50")}>
                        <Checkbox
                          checked={checked}
                          disabled={disabled}
                          onCheckedChange={(value) => setUserMailboxDomainIds((items) => value === true ? Array.from(new Set([...items, domain.id])) : items.filter((id) => id !== domain.id))}
                        />
                        <span className="text-sm font-medium">{domain.name}</span>
                      </label>
                    )
                  })}
                </div>
                {domains.length === 0 && <Empty text="暂无域名" />}
              </div>
              <div className="space-y-2">
                <Label>禁止前缀</Label>
                <Textarea name="reservedMailboxPrefixes" defaultValue={settings?.reservedMailboxPrefixes || ""} className="min-h-28 font-mono text-sm" />
              </div>
            </div>
          )}
          <Separator />
          <SwitchRow label="自动刷新" checked={mailAutoRefresh} onCheckedChange={setMailAutoRefresh} />
          {mailAutoRefresh && (
            <div className="border-t pt-5">
              <Field name="mailRefreshSeconds" label="刷新间隔秒数" type="number" min={5} defaultValue={String(settings?.mailRefreshSeconds || 30)} />
            </div>
          )}
        </CardContent>
      </Card>}

      {settingsTab === "externalImap" && <Card>
        <CardHeader>
          <CardTitle>外部 IMAP 接入</CardTitle>
        </CardHeader>
        <CardContent className="space-y-5">
          <div className="rounded-lg border bg-muted/30 p-4 text-sm text-muted-foreground">
            默认关闭。关闭后前台会隐藏外部 IMAP 接入，相关后端接口也会返回禁用。
          </div>
          <SwitchRow label="启用外部 IMAP" checked={externalImapEnabled} onCheckedChange={setExternalImapEnabled} />
          {externalImapEnabled && (
            <div className="space-y-5 border-t pt-5">
              <div className="grid gap-4 md:grid-cols-2">
                <Field name="externalImapSecretKey" label={settings?.externalImapSecretSet ? "密码加密密钥（留空不变）" : "密码加密密钥"} type="password" required={!settings?.externalImapSecretSet} />
                <Field name="externalImapSyncSeconds" label="后台同步间隔秒数" type="number" min={30} defaultValue={String(settings?.externalImapSyncSeconds || 300)} />
              </div>
              <SwitchRow label="允许 localhost / 内网 / link-local IMAP 主机" checked={externalImapAllowPrivateHosts} onCheckedChange={setExternalImapAllowPrivateHosts} />
              <Separator />
              <div className="space-y-3">
                <div>
                  <div className="font-medium">Gmail OAuth2</div>
                  <div className="text-xs text-muted-foreground">回调地址：{(settings?.publicBaseUrl || "${LANQIN_PUBLIC_BASE_URL}").replace(/\/$/, "")}/api/external-imap-oauth/gmail/callback</div>
                </div>
                <div className="grid gap-4 md:grid-cols-2">
                  <Field name="externalImapGmailClientId" label="Gmail Client ID" defaultValue={settings?.externalImapGmailClientId || ""} required={false} />
                  <Field name="externalImapGmailClientSecret" label={settings?.externalImapGmailClientSecretSet ? "Gmail Client Secret（留空不变）" : "Gmail Client Secret"} type="password" required={false} />
                </div>
              </div>
              <Separator />
              <div className="space-y-3">
                <div>
                  <div className="font-medium">Microsoft 365 / Outlook OAuth2</div>
                  <div className="text-xs text-muted-foreground">回调地址：{(settings?.publicBaseUrl || "${LANQIN_PUBLIC_BASE_URL}").replace(/\/$/, "")}/api/external-imap-oauth/outlook/callback</div>
                </div>
                <div className="grid gap-4 md:grid-cols-2">
                  <Field name="externalImapOutlookClientId" label="Outlook Client ID" defaultValue={settings?.externalImapOutlookClientId || ""} required={false} />
                  <Field name="externalImapOutlookClientSecret" label={settings?.externalImapOutlookClientSecretSet ? "Outlook Client Secret（留空不变）" : "Outlook Client Secret"} type="password" required={false} />
                </div>
              </div>
            </div>
          )}
        </CardContent>
      </Card>}

      {settingsTab === "templates" && canViewTemplates && <MailTemplatesPanel templates={templates.data?.items || []} loading={templates.isLoading} canUpdate={canUpdateTemplates} canReset={canResetTemplates} />}

      {settingsTab === "security" && <Card>
        <CardHeader><CardTitle>安全设置</CardTitle></CardHeader>
        <CardContent className="space-y-5">
          <SwitchRow label="开放注册" checked={openRegistration} onCheckedChange={setOpenRegistration} />
          <Separator />
          <SwitchRow label="双因素认证 (2FA)" checked={twoFactorEnabled} onCheckedChange={setTwoFactorEnabled} />
          <Separator />
          <SwitchRow label="Turnstile" checked={turnstileEnabled} onCheckedChange={setTurnstileEnabled} />
          {turnstileEnabled && (
            <div className="grid gap-4 border-t pt-5 md:grid-cols-2">
              <Field name="turnstileSiteKey" label="Site Key" defaultValue={settings?.turnstileSiteKey || ""} required />
              <Field name="turnstileSecretKey" label={settings?.turnstileSecretSet ? "Secret Key（留空不变）" : "Secret Key"} type="password" required={!settings?.turnstileSecretSet} />
            </div>
          )}
        </CardContent>
      </Card>}

      {settingsTab === "about" && <AboutProjectCard />}

      {settingsTab !== "about" && canUpdateSettings && <div className="flex justify-end">
        <Button disabled={save.isPending || !settings}>{save.isPending ? "保存中..." : "保存设置"}</Button>
      </div>}
    </form>
  )
}

function MaildirSyncHealthCard({ health, loading, error, onRefresh, refreshing, fallbackRoot }: { health?: MaildirSyncHealth; loading: boolean; error: Error | null; onRefresh: () => void; refreshing: boolean; fallbackRoot: string }) {
  const root = health?.root || fallbackRoot
  const configured = health?.configured ?? !!root
  const lastRun = health?.lastRun
  const counters = lastRun?.counts || health?.summary
  const recentErrors = health?.recentErrors || []
  const status = health?.running ? "running" : lastRun?.status || (configured ? "idle" : "disabled")
  return (
    <Card>
      <CardHeader>
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="space-y-1">
            <CardTitle>Maildir 同步健康</CardTitle>
            <div className="break-all text-xs text-muted-foreground">{root || "未配置 Maildir 根目录"}</div>
          </div>
          <div className="flex items-center gap-2">
            <Badge variant={configured ? "default" : "secondary"}>{configured ? "已配置" : "未配置"}</Badge>
            <Badge variant={health?.running ? "default" : health?.workerStarted ? "outline" : "secondary"}>{health?.running ? "运行中" : health?.workerStarted ? "worker 已启动" : "worker 未启动"}</Badge>
            <Button type="button" variant="outline" size="sm" onClick={onRefresh} disabled={loading || refreshing}>
              <RefreshCcw className={cn("mr-2 h-4 w-4", refreshing && "animate-spin")} />
              刷新
            </Button>
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        {error && <div className="rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">{queryErrorMessage(error)}</div>}
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
          <InfoLine label="当前状态" value={<MaildirStatusBadge status={status} />} />
          <InfoLine label="最近开始" value={formatOptionalDate(lastRun?.startedAt)} />
          <InfoLine label="最近结束" value={formatOptionalDate(lastRun?.finishedAt)} />
          <InfoLine label="最近耗时" value={formatDuration(lastRun?.durationMs)} />
          <InfoLine label="扫描间隔" value={health?.scanSeconds ? `${health.scanSeconds} 秒` : "-"} />
          <InfoLine label="下次运行" value={formatOptionalDate(health?.nextRunAt)} />
          <InfoLine label="最后错误" value={lastRun?.error || health?.lastError || "-"} />
          <InfoLine label="错误数" value={counterValue(counters, "fileErrors")} />
        </div>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          {maildirCounterRows(counters).map((item) => <InfoBox key={item.key} label={item.label} value={item.value} />)}
        </div>
        <div className="space-y-2">
          <div className="text-sm font-medium">最近错误</div>
          {recentErrors.length === 0 && <Empty text={loading ? "正在读取同步状态..." : "暂无同步错误"} />}
          {recentErrors.length > 0 && (
            <div className="space-y-2">
              {recentErrors.slice(0, 5).map((item, index) => (
                <div key={`${item}-${index}`} className="rounded-lg border px-3 py-2 text-sm">
                  <div className="break-words text-destructive">{item || "未知错误"}</div>
                </div>
              ))}
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  )
}

function MaildirStatusBadge({ status }: { status: string }) {
	const normalized = status.toLowerCase()
	if (normalized === "running") return <Badge>运行中</Badge>
	if (["ok", "success", "succeeded", "idle"].includes(normalized)) return <Badge variant="outline">{normalized === "idle" ? "等待下次扫描" : "正常"}</Badge>
	if (normalized === "partial") return <Badge variant="secondary">部分成功</Badge>
	if (["error", "failed", "failure"].includes(normalized)) return <Badge variant="destructive">失败</Badge>
	if (["disabled", "not_configured"].includes(normalized)) return <Badge variant="secondary">未启用</Badge>
	return <Badge variant="secondary">{status || "-"}</Badge>
}

function maildirCounterRows(counters?: Record<string, number | undefined>) {
  return [
    { key: "filesScanned", label: "扫描文件", value: counterValue(counters, "filesScanned") },
    { key: "imported", label: "导入", value: counterValue(counters, "imported") },
    { key: "backfilled", label: "回填", value: counterValue(counters, "backfilled") },
    { key: "cleaned", label: "清理", value: counterValue(counters, "cleaned") },
    { key: "fileErrors", label: "文件错误", value: counterValue(counters, "fileErrors") },
  ]
}

function counterValue(counters: Record<string, number | undefined> | undefined, key: string) {
  return Number(counters?.[key] || 0)
}

function formatOptionalDate(value?: string) {
  return value ? formatDate(value) || "-" : "-"
}

function formatDuration(value?: number) {
  if (!value) return "-"
  if (value < 1000) return `${value} ms`
  return `${(value / 1000).toFixed(value < 10_000 ? 1 : 0)} 秒`
}

function queryErrorMessage(error: unknown) {
  return error instanceof Error ? error.message : "读取 Maildir 同步健康失败"
}

function AboutProjectCard() {
  return (
    <Card>
      <CardHeader>
        <CardTitle>关于</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4 text-sm">
        <AboutRow label="版本">
          <SystemVersionDialog mode="inline" />
        </AboutRow>
        <AboutRow label="交流">
          <div className="flex flex-wrap gap-3">
            <Button type="button" variant="outline" className="h-11 justify-start px-4 text-base font-normal" asChild>
              <a href={projectRepositoryUrl} target="_blank" rel="noreferrer">
                <Github className="h-5 w-5" />
                GitHub
              </a>
            </Button>
            <Button type="button" variant="outline" className="h-11 justify-start px-4 text-base font-normal" asChild>
              <a href={`${projectRepositoryUrl}/issues`} target="_blank" rel="noreferrer">
                <Circle className="h-5 w-5 text-muted-foreground" />
                Issues
              </a>
            </Button>
            <Button type="button" variant="outline" className="h-11 justify-start px-4 text-base font-normal" asChild>
              <a href={projectTelegramUrl} target="_blank" rel="noreferrer">
                <ExternalLink className="h-5 w-5 text-sky-500" />
                Telegram 群组
              </a>
            </Button>
          </div>
        </AboutRow>
        <AboutRow label="支持">
          <Button type="button" variant="outline" className="h-11 justify-start px-4 text-base font-normal" asChild>
            <a href={projectRepositoryUrl} target="_blank" rel="noreferrer">
              <Star className="h-5 w-5 text-yellow-500" />
              给项目点 Star
            </a>
          </Button>
        </AboutRow>
        <AboutRow label="帮助">
          <div className="flex flex-wrap gap-3">
            <Button type="button" variant="outline" className="h-11 justify-start px-4 text-base font-normal" asChild>
              <a href={`${projectRepositoryUrl}#readme`} target="_blank" rel="noreferrer">
                <BookOpen className="h-5 w-5 text-sky-500" />
                项目文档
              </a>
            </Button>
            <Button type="button" variant="outline" className="h-11 justify-start px-4 text-base font-normal" asChild>
              <a href={`${projectRepositoryUrl}/blob/main/LICENSE`} target="_blank" rel="noreferrer">
                <Scale className="h-5 w-5 text-emerald-500" />
                开源协议
              </a>
            </Button>
          </div>
        </AboutRow>
      </CardContent>
    </Card>
  )
}

function AboutRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid gap-2 sm:grid-cols-[4.5rem_minmax(0,1fr)] sm:items-center">
      <div className="font-medium text-muted-foreground">{label}：</div>
      <div className="min-w-0">{children}</div>
    </div>
  )
}

function TestSMTPDialog({ disabled }: { disabled?: boolean }) {
  const { toast } = useToast()
  const [open, setOpen] = React.useState(false)
  const test = useMutation({
    mutationFn: (form: FormData) => api.testSmtp(String(form.get("to") || "")),
    onSuccess: () => {
      setOpen(false)
      toast({ title: "测试邮件已发送" })
    },
    onError: (e) => toast({ title: "发送失败", description: e.message }),
  })
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button type="button" variant="outline" size="sm" disabled={disabled}>测试发送</Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader><DialogTitle>SMTP 测试发送</DialogTitle></DialogHeader>
        <form className="space-y-4" onSubmit={(event) => { event.preventDefault(); test.mutate(new FormData(event.currentTarget)) }}>
          <Field name="to" label="收件邮箱" type="email" placeholder="test@example.com" />
          <DialogFooter><Button disabled={test.isPending}>{test.isPending ? "发送中..." : "发送"}</Button></DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function MailTemplatesPanel({ templates, loading, canUpdate, canReset }: { templates: MailTemplate[]; loading: boolean; canUpdate: boolean; canReset: boolean }) {
  const qc = useQueryClient()
  const { toast } = useToast()
  const [selectedKey, setSelectedKey] = React.useState("")
  const selected = templates.find((template) => template.key === selectedKey) || templates[0]
  const [subject, setSubject] = React.useState("")
  const [bodyText, setBodyText] = React.useState("")
  const [bodyHtml, setBodyHtml] = React.useState("")
  React.useEffect(() => {
    if (!selectedKey && templates[0]) setSelectedKey(templates[0].key)
  }, [selectedKey, templates])
  React.useEffect(() => {
    if (!selected) return
    setSubject(selected.subject)
    setBodyText(selected.bodyText)
    setBodyHtml(selected.bodyHtml)
  }, [selected])
  const save = useMutation({
    mutationFn: () => api.updateMailTemplate(selected!.key, { subject, bodyText, bodyHtml }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["admin", "mail-templates"] })
      toast({ title: "模板已保存" })
    },
    onError: (e) => toast({ title: "保存失败", description: e.message }),
  })
  const reset = useMutation({
    mutationFn: () => api.resetMailTemplate(selected!.key),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["admin", "mail-templates"] })
      toast({ title: "模板已恢复" })
    },
    onError: (e) => toast({ title: "恢复失败", description: e.message }),
  })
  if (loading) return <Card><CardContent className="p-6"><Empty text="加载中..." /></CardContent></Card>
  if (!selected) return <Card><CardContent className="p-6"><Empty text="暂无模板" /></CardContent></Card>
  return (
    <Card>
      <CardHeader><CardTitle>邮件模板</CardTitle></CardHeader>
      <CardContent className="space-y-4">
        <SelectField label="模板" value={selected.key} onValueChange={setSelectedKey} items={templates.map((template) => [template.key, template.name])} />
        <div className="space-y-2">
          <Label>主题</Label>
          <Input value={subject} onChange={(event) => setSubject(event.target.value)} />
        </div>
        <div className="grid gap-4 lg:grid-cols-2">
          <div className="space-y-2">
            <Label>纯文本</Label>
            <Textarea value={bodyText} onChange={(event) => setBodyText(event.target.value)} className="min-h-64 font-mono text-sm" />
          </div>
          <div className="space-y-2">
            <Label>HTML</Label>
            <Textarea value={bodyHtml} onChange={(event) => setBodyHtml(event.target.value)} className="min-h-64 font-mono text-sm" />
          </div>
        </div>
        {(canUpdate || canReset) && <div className="flex justify-end gap-2">
          {canReset && <Button type="button" variant="outline" disabled={reset.isPending || save.isPending} onClick={() => reset.mutate()}>
            {reset.isPending ? "恢复中..." : "恢复默认"}
          </Button>}
          {canUpdate && <Button type="button" disabled={save.isPending || reset.isPending} onClick={() => save.mutate()}>
            {save.isPending ? "保存中..." : "保存模板"}
          </Button>}
        </div>}
      </CardContent>
    </Card>
  )
}

function AdminMessageDialog({ message, loading, open, onOpenChange }: { message?: MailMessage; loading: boolean; open: boolean; onOpenChange: (open: boolean) => void }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[86vh] overflow-y-auto sm:max-w-4xl">
        <DialogHeader><DialogTitle>{loading ? "加载中..." : message?.subject || "邮件详情"}</DialogTitle></DialogHeader>
        {message && (
          <div className="space-y-5">
            <div className="grid gap-3 rounded-lg border p-4 text-sm md:grid-cols-2">
              <MessageMeta label="所属邮箱" value={message.mailboxAddress || message.recipientAddress || ""} />
              <MessageMeta label="所属账号" value={message.ownerEmail || ""} />
              <MessageMeta label="发件人" value={adminSenderTitle(message)} />
              <MessageMeta label="收件人" value={message.recipientAddress || message.to?.join(", ") || ""} />
              <MessageMeta label="文件夹" value={folderName(message.folder)} />
              <MessageMeta label="时间" value={formatDate(message.receivedAt)} />
            </div>
            <div className="mail-html prose max-w-none rounded-lg border p-5 text-sm leading-7" dangerouslySetInnerHTML={{ __html: DOMPurify.sanitize(message.bodyHtml || `<pre>${escapeHtml(message.bodyText || message.snippet || "")}</pre>`) }} />
            {message.attachments && message.attachments.length > 0 && (
              <div className="rounded-lg border p-4">
                <div className="mb-3 font-medium">附件</div>
                <div className="space-y-2">
                  {message.attachments.map((attachment) => (
                    <a className="flex items-center justify-between rounded-md border p-3 text-sm hover:bg-accent" href={`/api/admin/attachments/${attachment.id}`} key={attachment.id}>
                      <span className="truncate">{attachment.filename}</span>
                      <span className="text-muted-foreground">{formatBytes(attachment.sizeBytes)}</span>
                    </a>
                  ))}
                </div>
              </div>
            )}
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}

function MessageMeta({ label, value }: { label: string; value: string }) {
  return <div className="min-w-0"><div className="text-xs text-muted-foreground">{label}</div><div className="truncate font-medium">{value || "-"}</div></div>
}

function folderName(folder: string) {
  const labels: Record<string, string> = { Inbox: "收件箱", Sent: "已发送", Drafts: "草稿箱", Archive: "归档", Spam: "垃圾邮件", Trash: "回收站", Unregistered: "未注册收件" }
  return labels[folder] || folder
}

function escapeHtml(value: string) {
  return value.replace(/[&<>"']/g, (char) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[char] || char)
}

function adminSenderDisplayName(message: MailMessage) {
  const fromName = decodeMimeHeader(message.fromName?.trim() || "")
  if (fromName) return fromName
  const text = decodeMimeHeader(message.from.trim())
  const namedAddress = text.match(/^"?([^"<]+?)"?\s*<[^>]+>$/)
  const name = namedAddress?.[1]?.trim()
  if (name) return name
  const address = text.match(/<([^>]+)>/)?.[1]?.trim() || text
  return address.split("@")[0]?.trim() || text || "未知发件人"
}

function adminSenderTitle(message: MailMessage) {
  const name = decodeMimeHeader(message.fromName?.trim() || "")
  const from = decodeMimeHeader(message.from)
  return name ? `${name} <${from}>` : from
}

const sendAuditEvents = ["accepted", "queued", "retry", "delivered", "failed", "canceled"]

function sendAuditEventLabel(event: string) {
  switch (event) {
    case "accepted": return "已接受"
    case "queued": return "已入队"
    case "retry": return "重试"
    case "delivered": return "已投递"
    case "failed": return "失败"
    case "canceled": return "已取消"
    default: return event || "-"
  }
}

function sendAuditBadgeVariant(event?: string) {
  if (event === "failed") return "destructive"
  if (event === "delivered" || event === "accepted") return "default"
  return "secondary"
}

function Stat({ icon, label, value }: { icon: React.ReactNode; label: string; value: React.ReactNode }) {
  return <Card><CardContent className="flex items-center gap-3 p-4 sm:gap-4 sm:p-5"><div className="grid h-9 w-9 shrink-0 place-items-center rounded-lg bg-muted text-foreground sm:h-10 sm:w-10">{icon}</div><div className="min-w-0"><div className="truncate text-xl font-semibold tracking-tight sm:text-2xl">{value}</div><div className="text-xs text-muted-foreground">{label}</div></div></CardContent></Card>
}
function InfoBox({ label, value }: { label: string; value: React.ReactNode }) { return <div className="rounded-lg border p-4"><div className="text-xl font-semibold tracking-tight sm:text-2xl">{value}</div><div className="text-xs text-muted-foreground">{label}</div></div> }
function Empty({ text }: { text: string }) { return <div className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">{text}</div> }
function DomainBadgeRow({ domain }: { domain: Domain }) { return <div className="flex items-center justify-between rounded-lg border p-3"><span className="font-medium">{domain.name}</span><Badge variant={domain.dnsStatus === "ok" ? "default" : "secondary"}>{domain.dnsStatus === "ok" ? "正常" : domain.dnsStatus}</Badge></div> }
function invalidateAdmin(qc: ReturnType<typeof useQueryClient>) { qc.invalidateQueries({ queryKey: ["admin"] }); qc.invalidateQueries({ queryKey: ["mailboxes"] }); qc.invalidateQueries({ queryKey: ["me"] }) }

function UserMailboxCell({ user }: { user: AdminUser }) {
  const { toast } = useToast()
  const loginAddress = accountLoginName(user)
  const mailboxes = user.mailboxes || []
  const [mailboxQuery, setMailboxQuery] = React.useState("")
  const normalizedQuery = mailboxQuery.trim().toLowerCase()
  const sortedMailboxes = React.useMemo(() => {
    return Array.from(new Set(mailboxes)).sort((a, b) => a.localeCompare(b, "en", { sensitivity: "base" }))
  }, [mailboxes])
  const [selectedAddress, setSelectedAddress] = React.useState(loginAddress)
  React.useEffect(() => {
    if (selectedAddress === loginAddress || sortedMailboxes.includes(selectedAddress)) return
    setSelectedAddress(loginAddress)
  }, [loginAddress, selectedAddress, sortedMailboxes])
  const filteredMailboxes = React.useMemo(() => {
    if (!normalizedQuery) return sortedMailboxes
    return sortedMailboxes.filter((mailbox) => mailbox.toLowerCase().includes(normalizedQuery))
  }, [normalizedQuery, sortedMailboxes])
  const limit = user.role === "admin" ? "不限" : limitText(user.limits?.maxMailboxCount ?? defaultMailboxLimitOverride, "个")
  const quota = <div className="text-[11px] text-muted-foreground">邮箱 {user.mailboxCount}/{limit}</div>
  async function copyMailbox(address: string) {
    if (!address) return
    await navigator.clipboard.writeText(address)
    toast({ title: "邮箱地址已复制", description: address })
  }
  return (
    <div className="w-full max-w-[21rem] space-y-1">
      <div className="flex min-w-0 items-center gap-1.5">
        <DropdownMenu onOpenChange={(open) => { if (!open) setMailboxQuery("") }}>
          <DropdownMenuTrigger asChild>
            <Button
              type="button"
              variant="outline"
              className="h-8 min-w-0 flex-1 justify-start gap-1.5 overflow-hidden rounded-md border-input bg-background px-2 text-left font-normal shadow-none hover:bg-background"
              title={selectedAddress}
            >
              <Mail className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
              <span className="min-w-0 flex-1 truncate text-[13px] font-medium">{selectedAddress}</span>
              {sortedMailboxes.length > 0 && <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-[11px] font-medium text-muted-foreground">{sortedMailboxes.length} 个</span>}
              <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="w-[21rem] max-w-[calc(100vw-32px)] p-1">
            <div className="px-1 pb-1">
              <div className="relative">
                <Search className="absolute left-2.5 top-2 h-3.5 w-3.5 text-muted-foreground" />
                <Input
                  autoFocus
                  value={mailboxQuery}
                  onChange={(event) => setMailboxQuery(event.target.value)}
                  onKeyDown={(event) => event.stopPropagation()}
                  placeholder="搜索邮箱..."
                  className="h-8 rounded-md bg-background pl-8 pr-2 text-[13px] shadow-none"
                />
              </div>
            </div>
            {filteredMailboxes.map((mailbox) => (
              <DropdownMenuItem
                key={mailbox}
                onSelect={() => setSelectedAddress(mailbox)}
                className={cn("h-8 min-w-0 gap-2 rounded-sm px-2 text-[13px] font-normal", selectedAddress === mailbox && "bg-accent text-accent-foreground")}
              >
                <Mail className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                <span className="min-w-0 flex-1 truncate" title={mailbox}>{mailbox}</span>
              </DropdownMenuItem>
            ))}
            {sortedMailboxes.length === 0 && <DropdownMenuItem disabled className="h-8 px-2 text-[13px] font-normal">暂无创建邮箱</DropdownMenuItem>}
            {sortedMailboxes.length > 0 && filteredMailboxes.length === 0 && <DropdownMenuItem disabled className="h-8 px-2 text-[13px] font-normal">没有匹配邮箱</DropdownMenuItem>}
          </DropdownMenuContent>
        </DropdownMenu>
        <Button type="button" variant="outline" size="icon" className="h-8 w-8 shrink-0 rounded-md bg-background shadow-none hover:bg-background" disabled={!selectedAddress} onClick={() => copyMailbox(selectedAddress)} aria-label="复制邮箱地址" title="复制邮箱地址">
          <Copy className="h-3.5 w-3.5" />
        </Button>
      </div>
      {quota}
    </div>
  )
}

function UserPermissionGroupsCell({ user }: { user: AdminUser }) {
  const groups = user.permissionGroups || []
  if (groups.length === 0) return <span className="text-muted-foreground">普通用户</span>
  return (
    <div className="flex max-w-md flex-wrap gap-1">
      {groups.map((group) => (
        <Badge key={group.id} variant={group.id === "pg_super_admin" ? "default" : "secondary"} className="font-normal">
          {group.name}
        </Badge>
      ))}
    </div>
  )
}

function assignableUserGroupIDs(user: AdminUser) {
  return (user.permissionGroupIds || []).filter((id) => id !== "pg_super_admin" && id !== "pg_regular_user")
}

function PermissionGroupPicker({ groups, value, onChange }: { groups: PermissionGroup[]; value: string[]; onChange: (value: string[]) => void }) {
  function toggle(groupID: string, checked: boolean) {
    onChange(checked ? Array.from(new Set([...value, groupID])) : value.filter((id) => id !== groupID))
  }
  return (
    <div className="space-y-2">
      <Label>权限配额</Label>
      <div className="grid gap-2 md:grid-cols-2">
        {groups.map((group) => {
          const checked = value.includes(group.id)
          return (
            <label key={group.id} className="flex min-h-16 items-start gap-3 rounded-md border px-3 py-2">
              <Checkbox checked={checked} onCheckedChange={(next) => toggle(group.id, next === true)} />
              <span className="min-w-0">
                <span className="block text-sm font-medium">{group.name}</span>
                <span className="line-clamp-2 text-xs text-muted-foreground">{group.description}</span>
              </span>
            </label>
          )
        })}
      </div>
      {groups.length === 0 && <Empty text="暂无可分配权限配额" />}
    </div>
  )
}

function RoleBadge({ user }: { user: AdminUser }) {
  return (
    <div className="flex flex-wrap gap-1">
      <Badge variant={user.role === "admin" ? "default" : "secondary"}>{user.role === "admin" ? "管理员" : "普通用户"}</Badge>
      {user.protected && <Badge variant="outline">默认账号</Badge>}
    </div>
  )
}

function UserActions({ user, permissionGroups, onDelete }: { user: AdminUser; permissionGroups: PermissionGroup[]; onDelete?: () => void }) {
  const me = useMe()
  const currentUser = me.data?.user
  const qc = useQueryClient()
  const { toast } = useToast()
  const [editOpen, setEditOpen] = React.useState(false)
  const [passwordOpen, setPasswordOpen] = React.useState(false)
  const canUpdate = hasPermission(currentUser, "admin.users.update")
  const canResetPassword = hasPermission(currentUser, "admin.users.reset_password")
  const update = useMutation({
    mutationFn: (payload: { displayName: string; role: "admin" | "user"; disabled: boolean; permissionGroupIds?: string[] }) => api.updateUser(user.id, payload),
    onSuccess: () => { invalidateAdmin(qc); toast({ title: "账号已更新" }) },
    onError: (e) => toast({ title: "更新失败", description: e.message }),
  })
  function quickPatch(patch: Partial<{ role: "admin" | "user"; disabled: boolean }>) {
    const role = patch.role || user.role
    update.mutate({
      displayName: user.displayName,
      role,
      disabled: patch.disabled ?? user.disabled,
      permissionGroupIds: role === "user" ? assignableUserGroupIDs(user) : [],
    })
  }
  if (!canUpdate && !canResetPassword && !onDelete) return null
  return <><DropdownMenu><DropdownMenuTrigger asChild><Button variant="ghost" size="icon"><MoreHorizontal className="h-4 w-4" /></Button></DropdownMenuTrigger><DropdownMenuContent align="end">{canUpdate && <DropdownMenuItem onSelect={() => setEditOpen(true)}>编辑账号</DropdownMenuItem>}{canResetPassword && <DropdownMenuItem onSelect={() => setPasswordOpen(true)}>重置密码</DropdownMenuItem>}{!user.protected && canUpdate && <><DropdownMenuSeparator /><DropdownMenuItem onSelect={() => quickPatch({ disabled: !user.disabled })}>{user.disabled ? "启用账号" : "停用账号"}</DropdownMenuItem><DropdownMenuItem onSelect={() => quickPatch({ role: user.role === "admin" ? "user" : "admin" })}>{user.role === "admin" ? "设为普通用户" : "设为管理员"}</DropdownMenuItem></>}{!user.protected && onDelete && <><DropdownMenuSeparator /><DropdownMenuItem className="text-destructive" onSelect={onDelete}>删除账号</DropdownMenuItem></>}</DropdownMenuContent></DropdownMenu>{canUpdate && <EditUserDialog user={user} permissionGroups={permissionGroups} open={editOpen} onOpenChange={setEditOpen} />}{canResetPassword && <ResetPasswordDialog user={user} open={passwordOpen} onOpenChange={setPasswordOpen} />}</>
}

function CreateUserDialog({ permissionGroups }: { permissionGroups: PermissionGroup[] }) {
  const qc = useQueryClient()
  const { toast } = useToast()
  const [open, setOpen] = React.useState(false)
  const [role, setRole] = React.useState<"admin" | "user">("user")
  const [status, setStatus] = React.useState("active")
  const [permissionGroupIds, setPermissionGroupIds] = React.useState<string[]>([])
  const create = useMutation({
    mutationFn: (form: FormData) => api.createUser({
      loginName: String(form.get("loginName") || ""),
      displayName: String(form.get("displayName") || ""),
      password: String(form.get("password") || ""),
      role,
      disabled: status === "disabled",
      mailboxLimitOverride: role === "user" ? mailboxLimitFromForm(form) : undefined,
      permissionGroupIds: role === "user" ? permissionGroupIds : [],
    }),
    onSuccess: () => { invalidateAdmin(qc); setOpen(false); setPermissionGroupIds([]); toast({ title: "账号已创建" }) },
    onError: (e) => toast({ title: "创建失败", description: e.message }),
  })
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild><Button size="sm"><Plus className="h-4 w-4" />账号</Button></DialogTrigger>
      <DialogContent>
        <DialogHeader><DialogTitle>创建账号</DialogTitle></DialogHeader>
        <form className="space-y-4" onSubmit={(event) => { event.preventDefault(); create.mutate(new FormData(event.currentTarget)) }}>
          <Field name="loginName" label="登录名" type="text" autoComplete="off" placeholder="admin" />
          <Field name="displayName" label="显示名称" placeholder="账号名称" />
          <Field name="password" label="初始密码" type="password" minLength={6} />
          <div className="grid grid-cols-2 gap-3">
            <SelectField label="身份" value={role} onValueChange={(value) => setRole(value as "admin" | "user")} items={[["user", "普通用户"], ["admin", "管理员"]]} />
            <SelectField label="状态" value={status} onValueChange={setStatus} items={[["active", "正常"], ["disabled", "停用"]]} />
          </div>
          {role === "user" && <MailboxLimitField defaultValue={defaultMailboxLimitOverride} />}
          {role === "user" && <PermissionGroupPicker groups={permissionGroups} value={permissionGroupIds} onChange={setPermissionGroupIds} />}
          <DialogFooter><Button disabled={create.isPending}>{create.isPending ? "创建中..." : "创建"}</Button></DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function MailboxActions({ mailbox, users, canUpdate, onDelete }: { mailbox: MailboxType; users: AdminUser[]; canUpdate: boolean; onDelete?: () => void }) {
  const [open, setOpen] = React.useState(false)
  if (!canUpdate && !onDelete) return null
  return <><DropdownMenu><DropdownMenuTrigger asChild><Button variant="ghost" size="icon"><MoreHorizontal className="h-4 w-4" /></Button></DropdownMenuTrigger><DropdownMenuContent align="end">{canUpdate && <DropdownMenuItem onSelect={() => setOpen(true)}>编辑邮箱</DropdownMenuItem>}{canUpdate && onDelete && <DropdownMenuSeparator />}{onDelete && <DropdownMenuItem className="text-destructive" onSelect={onDelete}>删除邮箱</DropdownMenuItem>}</DropdownMenuContent></DropdownMenu>{canUpdate && <EditMailboxDialog mailbox={mailbox} users={users} open={open} onOpenChange={setOpen} />}</>
}

function AliasActions({ alias, onToggle, onDelete }: { alias: Alias; onToggle?: () => void; onDelete?: () => void }) {
  if (!onToggle && !onDelete) return null
  return <DropdownMenu><DropdownMenuTrigger asChild><Button variant="ghost" size="icon"><MoreHorizontal className="h-4 w-4" /></Button></DropdownMenuTrigger><DropdownMenuContent align="end">{onToggle && <DropdownMenuItem onSelect={onToggle}>{alias.enabled ? "停用" : "启用"}</DropdownMenuItem>}{onToggle && onDelete && <DropdownMenuSeparator />}{onDelete && <DropdownMenuItem className="text-destructive" onSelect={onDelete}>删除转发</DropdownMenuItem>}</DropdownMenuContent></DropdownMenu>
}

function EditUserDialog({ user, permissionGroups, open, onOpenChange }: { user: AdminUser; permissionGroups: PermissionGroup[]; open: boolean; onOpenChange: (open: boolean) => void }) {
  const qc = useQueryClient()
  const { toast } = useToast()
  const [role, setRole] = React.useState(user.role)
  const [disabled, setDisabled] = React.useState(user.disabled ? "disabled" : "active")
  const [permissionGroupIds, setPermissionGroupIds] = React.useState<string[]>(assignableUserGroupIDs(user))
  React.useEffect(() => {
    setRole(user.role)
    setDisabled(user.disabled ? "disabled" : "active")
    setPermissionGroupIds(assignableUserGroupIDs(user))
  }, [user, open])
  const mut = useMutation({
    mutationFn: (form: FormData) => api.updateUser(user.id, {
      loginName: String(form.get("loginName") || ""),
      displayName: String(form.get("displayName") || ""),
      role,
      disabled: disabled === "disabled",
      mailboxLimitOverride: role === "user" ? mailboxLimitFromForm(form, effectiveMailboxLimit(user)) : undefined,
      permissionGroupIds: role === "user" ? permissionGroupIds : [],
    }),
    onSuccess: () => { invalidateAdmin(qc); onOpenChange(false); toast({ title: "账号已更新" }) },
    onError: (e) => toast({ title: "更新失败", description: e.message }),
  })
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader><DialogTitle>编辑账号</DialogTitle></DialogHeader>
        <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); mut.mutate(new FormData(e.currentTarget)) }}>
          <Field name="loginName" label="登录名" defaultValue={accountLoginName(user)} type="text" autoComplete="off" />
          <Field name="displayName" label="显示名称" defaultValue={user.displayName} />
          <div className="grid grid-cols-2 gap-3">
            <SelectField label="身份" value={role} onValueChange={(value) => setRole(value as "admin" | "user")} items={[["user", "普通用户"], ["admin", "管理员"]]} disabled={user.protected} />
            <SelectField label="状态" value={disabled} onValueChange={setDisabled} items={[["active", "正常"], ["disabled", "停用"]]} disabled={user.protected} />
          </div>
          {role === "user" && !user.protected && <MailboxLimitField defaultValue={effectiveMailboxLimit(user)} />}
          {role === "user" && !user.protected && <PermissionGroupPicker groups={permissionGroups} value={permissionGroupIds} onChange={setPermissionGroupIds} />}
          <DialogFooter><Button disabled={mut.isPending}>{mut.isPending ? "保存中..." : "保存"}</Button></DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function ResetPasswordDialog({ user, open, onOpenChange }: { user: AdminUser; open: boolean; onOpenChange: (open: boolean) => void }) {
  const { toast } = useToast(); const mut = useMutation({ mutationFn: (form: FormData) => api.resetUserPassword(user.id, String(form.get("password") || "")), onSuccess: () => { onOpenChange(false); toast({ title: "密码已重置" }) }, onError: (e) => toast({ title: "重置失败", description: e.message }) })
  return <Dialog open={open} onOpenChange={onOpenChange}><DialogContent><DialogHeader><DialogTitle>重置密码</DialogTitle></DialogHeader><form className="space-y-4" onSubmit={(e) => { e.preventDefault(); mut.mutate(new FormData(e.currentTarget)); e.currentTarget.reset() }}><Field name="loginName" label="登录名" value={accountLoginName(user)} readOnly /><Field name="password" label="新密码" type="password" minLength={6} /><DialogFooter><Button disabled={mut.isPending}>{mut.isPending ? "重置中..." : "重置"}</Button></DialogFooter></form></DialogContent></Dialog>
}

function EditMailboxDialog({ mailbox, users, open, onOpenChange }: { mailbox: MailboxType; users: AdminUser[]; open: boolean; onOpenChange: (open: boolean) => void }) {
  const qc = useQueryClient(); const { toast } = useToast(); const [userId, setUserId] = React.useState(mailbox.userId); const [status, setStatus] = React.useState(mailbox.status)
  React.useEffect(() => { setUserId(mailbox.userId); setStatus(mailbox.status) }, [mailbox, open])
  const mut = useMutation({ mutationFn: (form: FormData) => api.updateMailbox(mailbox.id, { userId, displayName: String(form.get("displayName") || ""), quotaMb: Number(form.get("quotaMb") || 1024), status }), onSuccess: () => { invalidateAdmin(qc); onOpenChange(false); toast({ title: "邮箱已更新" }) }, onError: (e) => toast({ title: "更新失败", description: e.message }) })
  return <Dialog open={open} onOpenChange={onOpenChange}><DialogContent><DialogHeader><DialogTitle>编辑邮箱</DialogTitle></DialogHeader><form className="space-y-4" onSubmit={(e) => { e.preventDefault(); mut.mutate(new FormData(e.currentTarget)) }}><Field name="address" label="邮箱地址" value={mailbox.address} readOnly /><SelectField label="归属账号" value={userId} onValueChange={setUserId} items={users.filter((u) => !u.disabled).map((u) => [u.id, u.email])} /><div className="grid grid-cols-2 gap-3"><Field name="displayName" label="显示名称" defaultValue={mailbox.displayName} /><Field name="quotaMb" label="配额 MB" type="number" defaultValue={String(mailbox.quotaMb)} /></div><SelectField label="状态" value={status} onValueChange={setStatus} items={[['active','启用'],['disabled','停用']]} /><DialogFooter><Button disabled={mut.isPending}>{mut.isPending ? "保存中..." : "保存"}</Button></DialogFooter></form></DialogContent></Dialog>
}

function CreateDomainDialog() {
  const qc = useQueryClient(); const { toast } = useToast(); const [open, setOpen] = React.useState(false)
  const mut = useMutation({ mutationFn: (form: FormData) => api.createDomain(String(form.get("name"))), onSuccess: () => { invalidateAdmin(qc); setOpen(false); toast({ title: "域名已创建" }) }, onError: (e) => toast({ title: "创建失败", description: e.message }) })
  return <Dialog open={open} onOpenChange={setOpen}><DialogTrigger asChild><Button variant="outline"><Plus className="h-4 w-4" />域名</Button></DialogTrigger><DialogContent><DialogHeader><DialogTitle>添加域名</DialogTitle></DialogHeader><form className="space-y-4" onSubmit={(e) => { e.preventDefault(); mut.mutate(new FormData(e.currentTarget)) }}><Field name="name" label="域名" placeholder="example.com" /><DialogFooter><Button disabled={mut.isPending}>创建</Button></DialogFooter></form></DialogContent></Dialog>
}

function CreateMailboxDialog({ domains, users }: { domains: Domain[]; users: AdminUser[] }) {
  const qc = useQueryClient(); const { toast } = useToast(); const [open, setOpen] = React.useState(false); const [domainId, setDomainId] = React.useState(""); const [role, setRole] = React.useState("user"); const [ownerMode, setOwnerMode] = React.useState("new"); const [userId, setUserId] = React.useState("")
  React.useEffect(() => { if (!domainId && domains[0]) setDomainId(domains[0].id); if (!userId && users[0]) setUserId(users[0].id) }, [domains, domainId, users, userId])
  const mut = useMutation({ mutationFn: (form: FormData) => api.createMailbox({ domainId, localPart: String(form.get("localPart")), displayName: String(form.get("displayName")), password: String(form.get("password")), quotaMb: Number(form.get("quotaMb") || 1024), role: role as "admin" | "user", ownerLoginName: String(form.get("ownerLoginName") || ""), userId: ownerMode === "existing" ? userId : "" }), onSuccess: () => { invalidateAdmin(qc); setOpen(false); toast({ title: "邮箱已创建" }) }, onError: (e) => toast({ title: "创建失败", description: e.message }) })
  return <Dialog open={open} onOpenChange={setOpen}><DialogTrigger asChild><Button><Plus className="h-4 w-4" />邮箱</Button></DialogTrigger><DialogContent><DialogHeader><DialogTitle>创建邮箱</DialogTitle></DialogHeader><form className="space-y-4" onSubmit={(e) => { e.preventDefault(); mut.mutate(new FormData(e.currentTarget)) }}><DomainSelect domains={domains} value={domainId} onChange={setDomainId} /><div className="grid grid-cols-2 gap-3"><Field name="localPart" label="邮箱前缀" placeholder="alice" /><Field name="displayName" label="显示名" placeholder="Alice" /></div><SelectField label="归属方式" value={ownerMode} onValueChange={setOwnerMode} items={[['new','新建/按登录名匹配账号'],['existing','追加到已有账号']]} />{ownerMode === "existing" ? <SelectField label="已有账号" value={userId} onValueChange={setUserId} items={users.filter((u) => !u.disabled).map((u) => [u.id, accountLoginName(u)])} /> : <Field name="ownerLoginName" label="归属登录名" placeholder="留空则使用新邮箱地址" required={false} />}<div className="grid grid-cols-2 gap-3"><Field name="password" label="密码" type="password" placeholder="至少 6 位" /><Field name="quotaMb" label="配额 MB" type="number" defaultValue="1024" /></div><SelectField label="身份" value={role} onValueChange={setRole} items={[['user','普通用户'],['admin','管理员']]} /><DialogFooter><Button disabled={mut.isPending || !domainId}>创建</Button></DialogFooter></form></DialogContent></Dialog>
}

function CreateAliasDialog({ domains }: { domains: Domain[] }) {
  const qc = useQueryClient(); const { toast } = useToast(); const [open, setOpen] = React.useState(false); const [domainId, setDomainId] = React.useState("")
  React.useEffect(() => { if (!domainId && domains[0]) setDomainId(domains[0].id) }, [domains, domainId])
  const mut = useMutation({ mutationFn: (form: FormData) => api.createAlias({ domainId, source: String(form.get("source")), destination: String(form.get("destination")), enabled: true }), onSuccess: () => { invalidateAdmin(qc); setOpen(false); toast({ title: "转发已创建" }) }, onError: (e) => toast({ title: "创建失败", description: e.message }) })
  return <Dialog open={open} onOpenChange={setOpen}><DialogTrigger asChild><Button variant="outline"><Plus className="h-4 w-4" />转发</Button></DialogTrigger><DialogContent><DialogHeader><DialogTitle>创建邮件转发</DialogTitle></DialogHeader><form className="space-y-4" onSubmit={(e) => { e.preventDefault(); mut.mutate(new FormData(e.currentTarget)) }}><DomainSelect domains={domains} value={domainId} onChange={setDomainId} /><Field name="source" label="来源" placeholder="sales 或 sales@example.com" /><Field name="destination" label="目标邮箱" placeholder="alice@example.com" /><DialogFooter><Button disabled={mut.isPending || !domainId}>创建</Button></DialogFooter></form></DialogContent></Dialog>
}

function DNSPanel({ domain, embedded = false }: { domain?: Domain; embedded?: boolean }) {
  const me = useMe()
  const user = me.data?.user
  const canCheckDNS = hasPermission(user, "admin.dns.check")
  const { toast } = useToast(); const qc = useQueryClient(); const records = useQuery({ queryKey: ["dns-records", domain?.id], queryFn: () => api.dnsRecords(domain!.id), enabled: !!domain })
  const check = useMutation({ mutationFn: () => api.checkDns(domain!.id), onSuccess: (res) => { qc.invalidateQueries({ queryKey: ["admin", "domains"] }); toast({ title: res.status === "ok" ? "DNS 检测通过" : "DNS 检测未通过", description: Object.values(res.checks).map((c) => c.message).join("；") }) } })
  if (!domain) return <Card><CardContent className="p-6 text-muted-foreground">请选择域名</CardContent></Card>
  const content = <>
    <p className="mb-3 text-sm text-muted-foreground">以下为需要在域名 DNS 管理中添加的记录：</p>
    <div className="space-y-3">{records.data?.items.map((r) => <DNSRecordRow key={`${r.type}-${r.name}`} record={r} />)}</div>
    {check.data && <>
      <Separator className="my-4" />
      <div className="flex items-center gap-2 text-sm font-medium text-muted-foreground"><CheckCircle2 className="h-4 w-4" />检测结果</div>
      <div className="mt-2 space-y-2">{Object.entries(check.data.checks).map(([k, v]) => <div key={k} className="flex items-center gap-2 text-sm"><CheckCircle2 className={`h-4 w-4 shrink-0 ${v.ok ? "text-green-600" : "text-destructive"}`} /><span className="font-medium">{k.toUpperCase()}:</span> {v.message}</div>)}</div>
    </>}</>
  const checkButton = canCheckDNS ? <Button variant="outline" size="sm" onClick={() => check.mutate()} disabled={check.isPending}><RefreshCcw className="h-4 w-4" />检测</Button> : null
  const header = <div className="flex items-center justify-between"><CardTitle>DNS 记录</CardTitle>{checkButton}</div>
  if (embedded) return <div className="space-y-4"><div className="flex items-center justify-between"><div className="font-medium">DNS 记录</div>{checkButton}</div>{content}</div>
  return <Card><CardHeader>{header}</CardHeader><CardContent>{content}</CardContent></Card>
}

function dnsDescription(record: DNSRecord): string {
  if (record.type === "TXT" && record.name.startsWith("_dmarc")) return "声明域名的 DMARC 策略（如何处理未通过 SPF/DKIM 验证的邮件）。"
  if (record.type === "TXT" && record.value.includes("DKIM1")) return "DKIM 公钥。收件服务器用此密钥验证邮件是否由你发出。"
  if (record.type === "TXT" && record.value.includes("spf1")) return "声明哪些服务器有权使用你的域名发件，防止伪造。"
  if (record.type === "MX") return `确保 ${record.name} 的 A 记录已指向你的服务器 IP，邮件才能到达。`
  return ""
}

function DNSRecordRow({ record }: { record: DNSRecord }) {
  const { toast } = useToast(); const text = `${record.type} ${record.name} ${record.value}`
  const desc = dnsDescription(record)
  return <div className="rounded-lg border bg-card p-3">
    <div className="mb-2 flex items-center justify-between">
      <Badge variant="outline" className="font-mono">{record.type}</Badge>
      <Button size="sm" variant="ghost" className="h-7 gap-1 text-xs" onClick={() => { navigator.clipboard.writeText(text); toast({ title: "已复制" }) }}><Copy className="h-3.5 w-3.5" />复制</Button>
    </div>
    {desc && <p className="mb-2 text-xs text-muted-foreground">{desc}</p>}
    <div className="break-all font-mono text-xs text-muted-foreground">
      <div><span className="text-foreground">Name:</span> {record.name}</div>
      <div><span className="text-foreground">Value:</span> {record.value}</div>
      <div><span className="text-foreground">TTL:</span> {record.ttl}s</div>
    </div>
  </div>
}

function fieldValue(form: FormData, name: string, fallback: string) {
  const value = form.get(name)
  return value === null ? fallback : String(value)
}
function fieldNumber(form: FormData, name: string, fallback: number) {
  const value = form.get(name)
  if (value === null) return fallback
  const n = Number(value)
  return Number.isFinite(n) && n > 0 ? n : fallback
}
function SwitchRow({ label, checked, onCheckedChange, className = "" }: { label: string; checked: boolean; onCheckedChange: (checked: boolean) => void; className?: string }) {
  return (
    <div className={`flex min-h-14 items-center justify-between gap-4 ${className}`}>
      <Label className="text-base font-medium">{label}</Label>
      <Switch checked={checked} onCheckedChange={onCheckedChange} />
    </div>
  )
}
function Field({ label, required = true, ...props }: React.InputHTMLAttributes<HTMLInputElement> & { label: string }) { return <div className="space-y-2"><Label>{label}</Label><Input required={required} {...props} /></div> }
function MailboxLimitField({ defaultValue }: { defaultValue: number }) {
  return (
    <div className="space-y-2">
      <Label>邮箱数量上限</Label>
      <Input name="mailboxLimitOverride" type="number" min={0} step={1} defaultValue={String(defaultValue)} />
      <div className="text-xs text-muted-foreground">普通用户默认 9 个，填 0 表示不限制。</div>
    </div>
  )
}
function mailboxLimitFromForm(form: FormData, fallback = defaultMailboxLimitOverride) {
  const value = Number(form.get("mailboxLimitOverride") || fallback)
  return Number.isFinite(value) && value >= 0 ? Math.floor(value) : fallback
}
function effectiveMailboxLimit(user: AdminUser) {
  return user.mailboxLimitOverride ?? user.limits?.maxMailboxCount ?? defaultMailboxLimitOverride
}
function SelectField({ label, value, onValueChange, items, disabled = false }: { label: string; value: string; onValueChange: (value: string) => void; items: string[][]; disabled?: boolean }) { return <div className="space-y-2"><Label>{label}</Label><Select value={value} onValueChange={onValueChange} disabled={disabled}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent>{items.map(([value, label]) => <SelectItem key={value} value={value}>{label}</SelectItem>)}</SelectContent></Select></div> }
function DomainSelect({ domains, value, onChange }: { domains: Domain[]; value: string; onChange: (value: string) => void }) { return <div className="space-y-2"><Label>域名</Label><Select value={value} onValueChange={onChange}><SelectTrigger><SelectValue placeholder="选择域名" /></SelectTrigger><SelectContent>{domains.map((d) => <SelectItem key={d.id} value={d.id}>{d.name}</SelectItem>)}</SelectContent></Select></div> }
