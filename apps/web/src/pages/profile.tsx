import * as React from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useNavigate, useSearchParams } from "react-router-dom"
import { ArrowLeft, BarChart3, Ban, Bell, BellOff, BookOpen, CheckCircle2, ChevronDown, ChevronUp, Clock3, Code2, Contact, Copy, ExternalLink, HardDrive, Image, Info, KeyRound, LogOut, Mail, MailCheck, MailX, Moon, PanelLeftOpen, PencilLine, PlayCircle, Plus, RefreshCcw, Search, SendHorizontal, Settings, ShieldCheck, SlidersHorizontal, Sun, Trash2, Users, X } from "lucide-react"
import { QRCodeSVG } from "qrcode.react"
import { api, APIToken, ExternalImapAccount, ExternalImapAccountPayload, ExternalImapFolder, ExternalImapOAuthProvider, ExternalImapStorageMode, ExternalImapSyncRun, ExternalImapTlsMode, ForwardingSettings, ForwardingVerifiedEmail, MailFolder, MailLabel, MailRule, MailRuleAction, MailRuleCondition, Mailbox, MailboxApplyOptions, MailSignature, MailStats, PermissionLimits } from "@/lib/api"
import { cn, formatBytes } from "@/lib/utils"
import { applyTheme, getInitialTheme } from "@/lib/theme"
import { DisplayMode, useDisplayMode } from "@/lib/display-mode"
import { useMe } from "@/hooks/use-me"
import { useLogout } from "@/hooks/use-logout"
import { useIsMobile } from "@/hooks/use-mobile"
import { validatePasswordConfirm } from "@/lib/validation"
import { hasPermission } from "@/lib/permissions"
import { Button } from "@/components/ui/button"
import { PasswordInput } from "@/components/ui/password-input"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { Badge } from "@/components/ui/badge"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Sheet, SheetContent, SheetFooter, SheetHeader, SheetTitle, SheetTrigger } from "@/components/ui/sheet"
import { Select, SelectContent, SelectGroup, SelectItem, SelectLabel, SelectSeparator, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { useToast } from "@/hooks/use-toast"

type Tab = "profile" | "mailboxes" | "contacts" | "cleanup" | "cleanupQueue" | "rules" | "blocked" | "stats" | "apiTokens"
type AccountSettingsTab = "account" | "mail" | "clients" | "security"
type MailboxView = "mine" | "forwarding" | "verified"
type PendingConfirm = { title: string; description?: string; confirmText: string; destructive?: boolean; onConfirm: () => void }
type RetryableQuery = { isLoading: boolean; isError: boolean; error: Error | null; refetch: () => Promise<unknown> }
const tabs: Record<Tab, { label: string; icon: React.ReactNode }> = {
  profile: { label: "账号设置", icon: <Settings className="h-4 w-4" /> },
  mailboxes: { label: "邮箱管理", icon: <Mail className="h-4 w-4" /> },
  contacts: { label: "联系人管理", icon: <Contact className="h-4 w-4" /> },
  cleanup: { label: "邮件清理", icon: <Trash2 className="h-4 w-4" /> },
  cleanupQueue: { label: "待清理邮件", icon: <Clock3 className="h-4 w-4" /> },
  rules: { label: "收信规则", icon: <SlidersHorizontal className="h-4 w-4" /> },
  blocked: { label: "被拦截邮件", icon: <Ban className="h-4 w-4" /> },
  stats: { label: "数据统计", icon: <BarChart3 className="h-4 w-4" /> },
  apiTokens: { label: "开发者", icon: <Code2 className="h-4 w-4" /> },
}
const tabKeys = Object.keys(tabs) as Tab[]
const accountSettingTabs: { key: AccountSettingsTab; label: string }[] = [
  { key: "account", label: "账号" },
  { key: "mail", label: "邮件" },
  { key: "clients", label: "通知与客户端" },
  { key: "security", label: "安全" },
]
const forwardingTargetCollator = new Intl.Collator("en", { sensitivity: "base", numeric: true })
const forwardingBindingPendingStatus = "pending_verification"

export function ProfilePage() {
  const me = useMe()
  const qc = useQueryClient()
  const navigate = useNavigate()
  const [params, setParams] = useSearchParams()
  const { toast } = useToast()
  const passwordFormRef = React.useRef<HTMLFormElement>(null)
  const twoFactorFormRef = React.useRef<HTMLFormElement>(null)
  const [mailboxId, setMailboxId] = React.useState(() => localStorage.getItem("lanqin:selected-mailbox") || "")
  const [statsMailboxId, setStatsMailboxId] = React.useState("all")
  const [statsRangeDays, setStatsRangeDays] = React.useState(30)
  const [darkMode, setDarkMode] = React.useState(getInitialTheme)
  const [displayMode, setDisplayMode] = useDisplayMode()
  const [blockedMailboxId, setBlockedMailboxId] = React.useState("all")
  const [ruleDialogOpen, setRuleDialogOpen] = React.useState(false)
  const [mobileSidebarOpen, setMobileSidebarOpen] = React.useState(false)
  const [externalRunAccountId, setExternalRunAccountId] = React.useState("")
  const [twoFactorRecoveryCodes, setTwoFactorRecoveryCodes] = React.useState<string[]>([])
  const isMobile = useIsMobile()
  const themeMountedRef = React.useRef(false)

  const rawTab = params.get("tab") as Tab | null
  const rawAccountTab = params.get("accountTab") as AccountSettingsTab | null
  const user = me.data?.user
  const canAccessMail = hasPermission(user, "mail.access")
  const canReadMail = hasPermission(user, "mail.messages.read")
  const canOrganizeMail = hasPermission(user, "mail.messages.organize")
  const canManageLabels = hasPermission(user, "mail.labels.manage")
  const canManageContacts = hasPermission(user, "mail.contacts.manage")
  const canManageSignatures = hasPermission(user, "mail.signatures.manage")
  const canManageRules = hasPermission(user, "mail.rules.manage")
  const canManageBlocked = hasPermission(user, "mail.blocked_senders.manage")
  const canViewStats = hasPermission(user, "mail.stats.view")
  const canApplyMailbox = hasPermission(user, "mail.mailboxes.apply")
  const canConfigureMailboxApply = hasPermission(user, "admin.settings.update")
  const visibleTabKeys = tabKeys.filter((key) => {
    if (key === "profile") return true
    if (key === "mailboxes") return canAccessMail || canApplyMailbox
    if (key === "contacts") return canManageContacts
    if (key === "cleanup") return canOrganizeMail
    if (key === "cleanupQueue") return canOrganizeMail
    if (key === "rules") return canManageRules
    if (key === "blocked") return canManageBlocked
    if (key === "stats") return canViewStats
    if (key === "apiTokens") return true
    return false
  })
  const tab: Tab = rawTab && visibleTabKeys.includes(rawTab) ? rawTab : "profile"
  const accountTab: AccountSettingsTab = rawAccountTab && accountSettingTabs.some((item) => item.key === rawAccountTab) ? rawAccountTab : "account"
  const mailboxes = useQuery({ queryKey: ["mailboxes", "mine"], queryFn: api.myMailboxes, enabled: canAccessMail })
  const mailboxApplyOptions = useQuery({ queryKey: ["mailbox-apply-options"], queryFn: api.mailboxApplyOptions, enabled: canApplyMailbox && tab === "mailboxes" })
  const publicSettings = useQuery({ queryKey: ["public-settings"], queryFn: api.publicSettings, enabled: tab === "mailboxes" || (tab === "profile" && accountTab === "clients") })
  const apiTokens = useQuery({ queryKey: ["api-tokens"], queryFn: api.apiTokens, enabled: tab === "apiTokens" })
  const contacts = useQuery({ queryKey: ["contacts"], queryFn: api.contacts, enabled: canManageContacts && tab === "contacts" })
  const signatures = useQuery({ queryKey: ["signatures"], queryFn: api.signatures, enabled: canManageSignatures && tab === "profile" && accountTab === "mail" })
  const rules = useQuery({ queryKey: ["rules"], queryFn: api.rules, enabled: canManageRules && tab === "rules" })
  const ruleForwarding = useQuery({ queryKey: ["forwarding-settings"], queryFn: api.forwardingSettings, enabled: canManageRules && canAccessMail && tab === "rules" })
  const ruleVerifiedEmails = React.useMemo(() => ruleForwarding.data?.verifiedEmails.filter((item) => item.verified).map((item) => item.email) || [], [ruleForwarding.data?.verifiedEmails])
  const blocked = useQuery({ queryKey: ["blocked-senders"], queryFn: api.blockedSenders, enabled: canManageBlocked && tab === "blocked" })
  const selectedMailbox = React.useMemo(() => mailboxes.data?.items.find((m) => m.id === mailboxId), [mailboxes.data?.items, mailboxId])
  const activeMailboxId = selectedMailbox?.id || ""
  const externalImapEnabled = publicSettings.data?.externalImapEnabled ?? false
  const externalImapAccounts = useQuery({ queryKey: ["external-imap-accounts", activeMailboxId], queryFn: () => api.externalImapAccounts(activeMailboxId), enabled: tab === "mailboxes" && !!activeMailboxId && canAccessMail && externalImapEnabled })
  React.useEffect(() => {
    if (!externalRunAccountId) return
    if (externalImapAccounts.data?.items.some((item) => item.id === externalRunAccountId)) return
    setExternalRunAccountId("")
  }, [externalImapAccounts.data?.items, externalRunAccountId])
  const selectedExternalRunAccount = externalImapAccounts.data?.items.find((item) => item.id === externalRunAccountId)
  const externalRunFolders = useQuery({ queryKey: ["external-imap-run-folders", externalRunAccountId], queryFn: () => api.externalFolders(externalRunAccountId), enabled: tab === "mailboxes" && !!externalRunAccountId && !!selectedExternalRunAccount && canAccessMail && externalImapEnabled })
  const externalSyncRuns = useQuery({ queryKey: ["external-imap-sync-runs", externalRunAccountId], queryFn: () => api.externalImapSyncRuns(externalRunAccountId), enabled: tab === "mailboxes" && !!externalRunAccountId && !!selectedExternalRunAccount && canAccessMail && externalImapEnabled })
  const labels = useQuery({ queryKey: ["labels", activeMailboxId], queryFn: () => api.labels(activeMailboxId), enabled: !!activeMailboxId && ((tab === "profile" && accountTab === "mail" && (canReadMail || canManageLabels)) || (tab === "rules" && canManageRules)) })
  const accountStats = useQuery({ queryKey: ["mail-stats", "all", 30], queryFn: () => api.mailStats("all", 30), enabled: canViewStats && tab === "profile" && accountTab === "account" })
  const mailboxStats = useQuery({ queryKey: ["mail-stats", activeMailboxId, 30], queryFn: () => api.mailStats(activeMailboxId, 30), enabled: !!activeMailboxId && canViewStats && (tab === "cleanup" || tab === "cleanupQueue") })
  const blockedStats = useQuery({ queryKey: ["mail-stats", blockedMailboxId, 30], queryFn: () => api.mailStats(blockedMailboxId, 30), enabled: canViewStats && tab === "blocked" })
  const dashboardStats = useQuery({ queryKey: ["mail-stats", statsMailboxId, statsRangeDays], queryFn: () => api.mailStats(statsMailboxId, statsRangeDays), enabled: canViewStats && tab === "stats" })

  const profile = useMutation({
    mutationFn: (form: FormData) => api.updateProfile({ displayName: String(form.get("displayName") || "") }),
    onSuccess: (data) => { qc.setQueryData(["me"], data); toast({ title: "个人资料已保存" }) },
    onError: (error) => toast({ title: "保存失败", description: error.message }),
  })
  const password = useMutation({
    mutationFn: (form: FormData) => {
      const newPassword = String(form.get("newPassword") || "")
      validatePasswordConfirm(newPassword, String(form.get("confirmPassword") || ""), "两次输入的新密码不一致")
      return api.changePassword({ currentPassword: String(form.get("currentPassword") || ""), newPassword })
    },
    onSuccess: () => { passwordFormRef.current?.reset(); toast({ title: "密码已更新" }) },
    onError: (error) => toast({ title: "修改失败", description: error.message }),
  })
  const setupTwoFactor = useMutation({
    mutationFn: api.setupTwoFactor,
    onSuccess: () => { setTwoFactorRecoveryCodes([]); toast({ title: "双因素密钥已生成" }) },
    onError: (error) => toast({ title: "生成失败", description: error.message }),
  })
  const enableTwoFactor = useMutation({
    mutationFn: (form: FormData) => api.enableTwoFactor(String(form.get("code") || "")),
    onSuccess: (data) => { qc.setQueryData(["me"], { user: data.user }); setTwoFactorRecoveryCodes(data.recoveryCodes || []); setupTwoFactor.reset(); twoFactorFormRef.current?.reset(); toast({ title: "双因素认证已启用" }) },
    onError: (error) => toast({ title: "启用失败", description: error.message }),
  })
  const disableTwoFactor = useMutation({
    mutationFn: (form: FormData) => api.disableTwoFactor(String(form.get("code") || "")),
    onSuccess: (data) => { qc.setQueryData(["me"], data); setTwoFactorRecoveryCodes([]); twoFactorFormRef.current?.reset(); toast({ title: "双因素认证已关闭" }) },
    onError: (error) => toast({ title: "关闭失败", description: error.message }),
  })
  const createApiToken = useMutation({
    mutationFn: (payload: { name: string; expiresAt?: string; scopes: string[] }) => api.createApiToken(payload),
    onSuccess: (res) => { qc.invalidateQueries({ queryKey: ["api-tokens"] }); toast({ title: "API 密钥已创建" }); return res },
    onError: (error) => toast({ title: "创建失败", description: error.message }),
  })
  const updateApiToken = useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: { name?: string; expiresAt?: string; disabled?: boolean; scopes?: string[] } }) => api.updateApiToken(id, payload),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["api-tokens"] }); toast({ title: "API 密钥已更新" }) },
    onError: (error) => toast({ title: "更新失败", description: error.message }),
  })
  const deleteApiToken = useMutation({
    mutationFn: api.deleteApiToken,
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["api-tokens"] }); toast({ title: "API 密钥已撤销" }) },
    onError: (error) => toast({ title: "撤销失败", description: error.message }),
  })
  const createContact = useMutation({
    mutationFn: (form: FormData) => api.createContact({ name: String(form.get("name") || ""), email: String(form.get("email") || ""), note: String(form.get("note") || "") }),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["contacts"] }); toast({ title: "联系人已保存" }) },
    onError: (error) => toast({ title: "保存失败", description: error.message }),
  })
  const deleteContact = useMutation({ mutationFn: api.deleteContact, onSuccess: () => { qc.invalidateQueries({ queryKey: ["contacts"] }); toast({ title: "联系人已删除" }) }, onError: (error) => toast({ title: "删除失败", description: error.message }) })
  const createSignature = useMutation({
    mutationFn: (form: FormData) => api.createSignature({ mailboxId: String(form.get("mailboxId") || ""), name: String(form.get("name") || ""), content: String(form.get("content") || ""), isDefault: form.get("isDefault") === "on" }),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["signatures"] }); qc.invalidateQueries({ queryKey: ["signature"] }); toast({ title: "签名已保存" }) },
    onError: (error) => toast({ title: "保存失败", description: error.message }),
  })
  const updateSignature = useMutation({
    mutationFn: ({ id, form }: { id: string; form: FormData }) => api.updateSignature(id, { mailboxId: String(form.get("mailboxId") || ""), name: String(form.get("name") || ""), content: String(form.get("content") || ""), isDefault: form.get("isDefault") === "on" }),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["signatures"] }); qc.invalidateQueries({ queryKey: ["signature"] }); toast({ title: "签名已更新" }) },
    onError: (error) => toast({ title: "保存失败", description: error.message }),
  })
  const setDefaultSignature = useMutation({
    mutationFn: api.setDefaultSignature,
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["signatures"] }); qc.invalidateQueries({ queryKey: ["signature"] }); toast({ title: "默认签名已更新" }) },
    onError: (error) => toast({ title: "设置失败", description: error.message }),
  })
  const deleteSignature = useMutation({ mutationFn: api.deleteSignature, onSuccess: () => { qc.invalidateQueries({ queryKey: ["signatures"] }); qc.invalidateQueries({ queryKey: ["signature"] }); toast({ title: "签名已删除" }) }, onError: (error) => toast({ title: "删除失败", description: error.message }) })
  const createRule = useMutation({
    mutationFn: (payload: {
      mailboxId: string
      name: string
      matchMode: "all" | "any"
      conditions: MailRuleCondition[]
      actions: MailRuleAction[]
      applyToExisting: boolean
      stopProcessing: boolean
      enabled: boolean
    }) => api.createRule(payload),
    onSuccess: (rule) => {
      qc.invalidateQueries({ queryKey: ["rules"] })
      qc.invalidateQueries({ queryKey: ["messages"] })
      qc.invalidateQueries({ queryKey: ["mail-stats"] })
      qc.invalidateQueries({ queryKey: ["labels"] })
      setRuleDialogOpen(false)
      toast({ title: rule.appliedExistingCount ? `收件规则已保存，已应用 ${rule.appliedExistingCount} 封邮件` : "收件规则已保存" })
    },
    onError: (error) => toast({ title: "保存失败", description: error.message }),
  })
  const deleteRule = useMutation({ mutationFn: api.deleteRule, onSuccess: () => { qc.invalidateQueries({ queryKey: ["rules"] }); toast({ title: "规则已删除" }) }, onError: (error) => toast({ title: "删除失败", description: error.message }) })
  const updateRule = useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: Partial<RuleCreatePayload> }) => api.updateRule(id, payload),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["rules"] }); setRuleDialogOpen(false); toast({ title: "收件规则已更新" }) },
    onError: (error) => toast({ title: "更新失败", description: error.message }),
  })
  const moveRule = useMutation({
    mutationFn: ({ id, direction }: { id: string; direction: "up" | "down" }) => api.moveRule(id, direction),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["rules"] }),
    onError: (error) => toast({ title: "排序失败", description: error.message }),
  })
  const applyRule = useMutation({
    mutationFn: api.applyRule,
    onSuccess: (res) => {
      qc.invalidateQueries({ queryKey: ["messages"] })
      qc.invalidateQueries({ queryKey: ["folders"] })
      qc.invalidateQueries({ queryKey: ["mail-stats"] })
      qc.invalidateQueries({ queryKey: ["labels"] })
      toast({ title: `规则已应用到 ${res.affected} 封现有邮件` })
    },
    onError: (error) => toast({ title: "应用失败", description: error.message }),
  })
  const createBlocked = useMutation({
    mutationFn: (form: FormData) => api.createBlockedSender({ mailboxId: blockedMailboxId === "all" ? "" : blockedMailboxId, email: String(form.get("email") || ""), reason: String(form.get("reason") || "") }),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["blocked-senders"] }); toast({ title: "拦截规则已保存" }) },
    onError: (error) => toast({ title: "保存失败", description: error.message }),
  })
  const deleteBlocked = useMutation({ mutationFn: api.deleteBlockedSender, onSuccess: () => { qc.invalidateQueries({ queryKey: ["blocked-senders"] }); toast({ title: "拦截规则已删除" }) }, onError: (error) => toast({ title: "删除失败", description: error.message }) })
  const createLabel = useMutation({
    mutationFn: (form: FormData) => api.createLabel({ mailboxId: activeMailboxId, name: String(form.get("name") || ""), color: String(form.get("color") || "") }),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["labels"] }); toast({ title: "标签已创建" }) },
    onError: (error) => toast({ title: "创建失败", description: error.message }),
  })
  const deleteLabel = useMutation({
    mutationFn: (id: string) => api.deleteLabel(id, activeMailboxId),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["labels"] }); toast({ title: "标签已删除" }) },
    onError: (error) => toast({ title: "删除失败", description: error.message }),
  })
  const cleanup = useMutation({
    mutationFn: (target: "empty-trash" | "empty-spam" | "archive-read-inbox") => api.cleanupMail({ mailboxId, target }),
    onSuccess: (res) => { qc.invalidateQueries({ queryKey: ["mail-stats"] }); qc.invalidateQueries({ queryKey: ["folders"] }); qc.invalidateQueries({ queryKey: ["messages"] }); toast({ title: `已处理 ${res.affected} 封邮件` }) },
    onError: (error) => toast({ title: "清理失败", description: error.message }),
  })
  const applyMailbox = useMutation({
    mutationFn: api.applyMailbox,
    onSuccess: (mailbox) => {
      qc.invalidateQueries({ queryKey: ["mailboxes", "mine"] })
      qc.invalidateQueries({ queryKey: ["mailbox-apply-options"] })
      setMailboxId(mailbox.id)
      toast({ title: "邮箱已申请" })
    },
    onError: (error) => toast({ title: "申请失败", description: error.message }),
  })
  const createExternalImap = useMutation({
    mutationFn: api.createExternalImapAccount,
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["external-imap-accounts"] }); qc.invalidateQueries({ queryKey: ["mail-external-accounts"] }); toast({ title: "外部 IMAP 已保存" }) },
    onError: (error) => toast({ title: "保存失败", description: error.message }),
  })
  const updateExternalImap = useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: ExternalImapAccountPayload }) => api.updateExternalImapAccount(id, payload),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["external-imap-accounts"] }); qc.invalidateQueries({ queryKey: ["mail-external-accounts"] }); toast({ title: "外部 IMAP 已更新" }) },
    onError: (error) => toast({ title: "更新失败", description: error.message }),
  })
  const deleteExternalImap = useMutation({
    mutationFn: api.deleteExternalImapAccount,
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["external-imap-accounts"] }); qc.invalidateQueries({ queryKey: ["mail-external-accounts"] }); toast({ title: "外部 IMAP 已删除" }) },
    onError: (error) => toast({ title: "删除失败", description: error.message }),
  })
  const testExternalImap = useMutation({
    mutationFn: api.testExternalImapAccount,
    onSuccess: (res) => toast({ title: `连接成功，发现 ${res.folders} 个文件夹` }),
    onError: (error) => toast({ title: "连接失败", description: error.message }),
  })
  const syncExternalImap = useMutation({
    mutationFn: api.syncExternalImapAccount,
    onSuccess: (run) => { qc.invalidateQueries({ queryKey: ["external-imap-accounts"] }); qc.invalidateQueries({ queryKey: ["external-imap-sync-runs"] }); qc.invalidateQueries({ queryKey: ["folders"] }); qc.invalidateQueries({ queryKey: ["messages"] }); toast({ title: `同步完成：导入 ${run.imported}，跳过 ${run.skipped}` }) },
    onError: (error) => toast({ title: "同步失败", description: error.message }),
  })
  const syncExternalImapFolder = useMutation({
    mutationFn: ({ id, folder }: { id: string; folder: string }) => api.syncExternalImapFolder(id, folder),
    onSuccess: (run) => { qc.invalidateQueries({ queryKey: ["external-imap-accounts"] }); qc.invalidateQueries({ queryKey: ["external-imap-sync-runs"] }); qc.invalidateQueries({ queryKey: ["folders"] }); qc.invalidateQueries({ queryKey: ["messages"] }); toast({ title: `${run.folder || "文件夹"} 同步完成：导入 ${run.imported}，跳过 ${run.skipped}` }) },
    onError: (error) => toast({ title: "同步失败", description: error.message }),
  })
  const startExternalOAuth = useMutation({
    mutationFn: ({ provider, mailboxId, email, storageMode }: { provider: ExternalImapOAuthProvider; mailboxId: string; email: string; storageMode: ExternalImapStorageMode }) => api.startExternalImapOAuth(provider, { mailboxId, email, storageMode, syncReadState: true, enabled: true }),
    onSuccess: (res) => { window.location.href = res.url },
    onError: (error) => toast({ title: "授权失败", description: error.message }),
  })

  React.useEffect(() => {
    if (!mailboxes.isSuccess) return
    const items = mailboxes.data?.items || []
    if (items.length === 0) {
      if (mailboxId) setMailboxId("")
      localStorage.removeItem("lanqin:selected-mailbox")
      return
    }
    if (!mailboxId || !items.some((m) => m.id === mailboxId)) setMailboxId(items[0].id)
  }, [mailboxId, mailboxes.isSuccess, mailboxes.data?.items])
  React.useEffect(() => { if (mailboxId) localStorage.setItem("lanqin:selected-mailbox", mailboxId); else localStorage.removeItem("lanqin:selected-mailbox") }, [mailboxId])
  React.useEffect(() => { applyTheme(darkMode, themeMountedRef.current); themeMountedRef.current = true }, [darkMode])
  React.useEffect(() => {
    document.documentElement.classList.add("workspace-ui")
    return () => document.documentElement.classList.remove("workspace-ui")
  }, [])

  const logout = useLogout()
  async function copy(text: string) { await navigator.clipboard.writeText(text); toast({ title: "已复制" }) }
  function setTab(next: Tab) {
    const visibleNext = visibleTabKeys.includes(next) ? next : "profile"
    const nextParams = new URLSearchParams(params)
    if (visibleNext === "profile") nextParams.delete("tab")
    else {
      nextParams.set("tab", visibleNext)
      nextParams.delete("accountTab")
    }
    setParams(nextParams)
    setMobileSidebarOpen(false)
  }
  function setAccountTab(next: AccountSettingsTab) {
    const nextParams = new URLSearchParams(params)
    nextParams.delete("tab")
    if (next === "account") nextParams.delete("accountTab")
    else nextParams.set("accountTab", next)
    setParams(nextParams)
  }
  if (me.isLoading) return <div className="grid h-svh place-items-center text-muted-foreground">加载中...</div>
  if (me.isError || !user) return <div className="grid h-svh place-items-center text-muted-foreground">登录状态已失效</div>

  const sidebarContent = (
    <div className="flex h-full w-[var(--app-sidebar-width)] shrink-0 flex-col border-r border-border bg-card">
      <div className="h-[64px] border-b">
        <AccountHeader name={user.displayName || selectedMailbox?.address || "NewSzxcn"} email={user.email || selectedMailbox?.address} darkMode={darkMode} onToggleTheme={() => setDarkMode((v) => !v)} onBack={() => navigate("/")} />
      </div>
      <nav className="min-h-0 flex-1 overflow-y-auto p-2">
        <div className="px-2 pb-2 pt-2 text-xs font-semibold text-muted-foreground">管理</div>
        <div className="space-y-1">
          {visibleTabKeys.map((key) => (
            <button
              key={key}
              type="button"
              className={cn(
                "flex h-9 w-full items-center gap-2 rounded-md px-3 text-left text-sm transition-colors",
                tab === key ? "bg-[hsl(var(--sidebar-active))] font-semibold text-[hsl(var(--sidebar-active-foreground))]" : "text-muted-foreground hover:bg-muted/70 hover:text-foreground",
              )}
              onClick={() => setTab(key)}
            >
              <span className={cn("text-muted-foreground [&>svg]:h-4 [&>svg]:w-4 [&>svg]:stroke-[1.8]", tab === key && "text-[hsl(var(--sidebar-active-foreground))]")}>{tabs[key].icon}</span>
              <span className="truncate">{tabs[key].label}</span>
            </button>
          ))}
          {user.role === "admin" && (
            <button
              type="button"
              className="flex h-9 w-full items-center gap-2 rounded-md px-3 text-left text-sm text-muted-foreground transition-colors hover:bg-muted/70 hover:text-foreground"
              onClick={() => navigate("/admin")}
            >
              <ShieldCheck className="h-4 w-4 stroke-[1.8]" />
              <span className="truncate">后台管理</span>
            </button>
          )}
        </div>
      </nav>
      <div className="border-t p-2">
        <Button type="button" variant="outline" size="sm" className="h-9 w-full justify-start gap-2 border-destructive/35 px-3 text-destructive shadow-none hover:border-destructive/55 hover:bg-destructive/10 hover:text-destructive dark:border-destructive/45 dark:hover:bg-destructive/15" onClick={logout}>
          <LogOut className="h-4 w-4" />
          <span>退出登录</span>
        </Button>
      </div>
    </div>
  )

  const pageTitle = tabs[tab].label
  const pageSubtitle = tab === "stats" ? "查看邮件收发趋势、分布情况和常用联系人。" : undefined
  const pageAction = tab === "stats"
    ? <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
        <div className="w-full sm:w-[220px]"><MailboxSelect value={statsMailboxId} mailboxes={mailboxes.data?.items || []} onChange={setStatsMailboxId} /></div>
        <StatsRangeTabs rangeDays={statsRangeDays} onRangeChange={setStatsRangeDays} />
      </div>
    : tab === "apiTokens"
      ? <Button asChild variant="outline" size="sm" className="h-8 px-3 text-xs"><a href="https://github.com/zxyszx/NewSzxcn-Email/blob/main/docs/API.md" target="_blank" rel="noreferrer"><BookOpen className="h-4 w-4" />API 文档</a></Button>
      : undefined

  return (
    <div className="profile-workspace-theme h-svh overflow-hidden bg-background">
      {isMobile ? (
        <div className="flex h-full min-h-0 flex-col">
          <header className="flex h-14 shrink-0 items-center gap-2 border-b px-3">
            <Sheet open={mobileSidebarOpen} onOpenChange={setMobileSidebarOpen}>
              <SheetTrigger asChild>
                <Button size="icon" variant="ghost" aria-label="打开导航"><PanelLeftOpen className="h-4 w-4" /></Button>
              </SheetTrigger>
              <SheetContent side="left" className="w-[85vw] max-w-80 p-0 [&>button]:hidden" aria-describedby={undefined}>
                <SheetTitle className="sr-only">管理导航</SheetTitle>
                <div className="h-svh">{sidebarContent}</div>
              </SheetContent>
            </Sheet>
            <div className="min-w-0 flex-1 text-sm font-semibold">{pageTitle}</div>
            <Button type="button" variant="ghost" size="icon" onClick={() => navigate("/")} aria-label="返回邮箱"><ArrowLeft className="h-4 w-4" /></Button>
          </header>
          <ScrollArea className="min-h-0 flex-1">
            <main className="w-full pb-10">
              {tab !== "mailboxes" && <SettingsPageHeader title={pageTitle} subtitle={pageSubtitle} action={pageAction} activeTab={tab === "profile" ? accountTab : undefined} onAccountTabChange={setAccountTab} />}
              <div className={contentFrameClass(tab)}>{renderTab()}</div>
            </main>
          </ScrollArea>
        </div>
      ) : (
        <div className="flex h-full min-h-0 w-full">
          {sidebarContent}
          <section className="min-w-0 flex-1 overflow-y-auto">
            <main className="pb-12">
              <SettingsPageHeader title={pageTitle} subtitle={pageSubtitle} action={pageAction} activeTab={tab === "profile" ? accountTab : undefined} onAccountTabChange={setAccountTab} />
              <div className={contentFrameClass(tab)}>{renderTab()}</div>
            </main>
          </section>
        </div>
      )}
    </div>
  )
  function renderTab() {
    const visibleQueries = visibleTabQueries()
    const failedQueries = visibleQueries.filter((query) => query.isError && query.error)
    if (failedQueries.length > 0) {
      return (
        <ProfileQueryFailure
          errors={failedQueries.map((query) => query.error!)}
          onRetry={() => { void Promise.all(failedQueries.map((query) => query.refetch())) }}
        />
      )
    }
    if (visibleQueries.some((query) => query.isLoading)) return <ProfileSectionLoading />
    if (tab === "profile") return (
      <AccountSettingsSection
        activeTab={accountTab}
        user={user!}
        profile={profile}
        password={password}
        passwordFormRef={passwordFormRef}
        stats={canViewStats ? accountStats.data : undefined}
        showStats={canViewStats}
        displayMode={displayMode}
        onDisplayModeChange={setDisplayMode}
        twoFactorFormRef={twoFactorFormRef}
        setupTwoFactor={setupTwoFactor}
        enableTwoFactor={enableTwoFactor}
        disableTwoFactor={disableTwoFactor}
        twoFactorRecoveryCodes={twoFactorRecoveryCodes}
        mailboxes={mailboxes.data?.items || []}
        selectedMailboxId={mailboxId}
        selectedMailbox={selectedMailbox}
        labels={labels.data?.items || []}
        labelsLoading={labels.isLoading}
        labelsPending={createLabel.isPending || deleteLabel.isPending}
        onCreateLabel={(form) => createLabel.mutate(form)}
        onDeleteLabel={(id) => deleteLabel.mutate(id)}
        signatures={signatures.data?.items || []}
        signaturesLoading={signatures.isLoading}
        signaturesPending={createSignature.isPending || updateSignature.isPending || setDefaultSignature.isPending || deleteSignature.isPending}
        onCreateSignature={(form) => createSignature.mutate(form)}
        onUpdateSignature={(id, form) => updateSignature.mutate({ id, form })}
        onSetDefaultSignature={(id) => setDefaultSignature.mutate(id)}
        onDeleteSignature={(id) => deleteSignature.mutate(id)}
        clientHostname={publicSettings.data?.publicHostname}
        onCopy={copy}
        onSelectMailbox={setMailboxId}
        onOpenCleanup={() => setTab("cleanup")}
      />
    )
    if (tab === "mailboxes") return (
      <MailboxManagement
        mailboxes={canAccessMail ? mailboxes.data?.items || [] : []}
        applyOptions={mailboxApplyOptions.data}
        applyPending={applyMailbox.isPending}
        canConfigureApply={canConfigureMailboxApply}
        selectedMailboxId={mailboxId}
        externalImapEnabled={externalImapEnabled}
        externalAccounts={externalImapAccounts.data?.items || []}
        externalPending={createExternalImap.isPending || updateExternalImap.isPending || deleteExternalImap.isPending || testExternalImap.isPending || syncExternalImap.isPending || syncExternalImapFolder.isPending || startExternalOAuth.isPending}
        selectedExternalRunAccountId={externalRunAccountId}
        externalRunFolders={externalRunFolders.data?.items || []}
        externalSyncRuns={externalSyncRuns.data?.items || []}
        onSelectExternalRunAccount={setExternalRunAccountId}
        onSelect={setMailboxId}
        onOpen={(id) => { if (!canAccessMail) return; setMailboxId(id); navigate("/") }}
        onApply={(payload) => applyMailbox.mutateAsync(payload).then(() => undefined)}
        onConfigureApply={() => navigate("/admin?section=settings&settingsTab=mail")}
        onCreateExternal={(payload) => createExternalImap.mutate(payload)}
        onStartExternalOAuth={(provider, payload) => startExternalOAuth.mutate({ provider, ...payload })}
        onUpdateExternal={(id, payload) => updateExternalImap.mutate({ id, payload })}
        onDeleteExternal={(id) => deleteExternalImap.mutate(id)}
        onTestExternal={(id) => testExternalImap.mutate(id)}
        onSyncExternal={(id) => syncExternalImap.mutate(id)}
        onSyncExternalFolder={(id, folder) => syncExternalImapFolder.mutate({ id, folder })}
      />
    )
    if (tab === "contacts") return <ContactsSection items={contacts.data?.items || []} loading={contacts.isLoading} pending={createContact.isPending} onCreate={(form) => createContact.mutate(form)} onDelete={(id) => deleteContact.mutate(id)} onCopy={copy} />
    if (tab === "cleanup") return <CleanupSection mailbox={selectedMailbox} stats={canViewStats ? mailboxStats.data : undefined} showStats={canViewStats} pending={cleanup.isPending} onCleanup={(target) => cleanup.mutate(target)} />
    if (tab === "cleanupQueue") return <CleanupQueueSection mailbox={selectedMailbox} stats={canViewStats ? mailboxStats.data : undefined} />
    if (tab === "rules") return <RulesSection items={rules.data?.items || []} mailboxes={mailboxes.data?.items || []} labels={labels.data?.items || []} verifiedEmails={ruleVerifiedEmails} open={ruleDialogOpen} onOpenChange={setRuleDialogOpen} onCreate={(payload) => createRule.mutate(payload)} onUpdate={(id, payload) => updateRule.mutate({ id, payload })} onToggle={(item) => updateRule.mutate({ id: item.id, payload: { enabled: !item.enabled } })} onMove={(id, direction) => moveRule.mutate({ id, direction })} onApply={(id) => applyRule.mutate(id)} onDelete={(id) => deleteRule.mutate(id)} pending={createRule.isPending || updateRule.isPending || moveRule.isPending || applyRule.isPending} />
    if (tab === "blocked") return <BlockedSection items={blocked.data?.items || []} mailboxes={mailboxes.data?.items || []} mailboxId={blockedMailboxId} spamCount={canViewStats ? blockedStats.data?.byFolder.find((f) => f.role === "spam")?.count || 0 : 0} onMailboxChange={setBlockedMailboxId} onCreate={(form) => createBlocked.mutate(form)} onDelete={(id) => deleteBlocked.mutate(id)} pending={createBlocked.isPending} />
    if (tab === "stats") return <StatsSection stats={dashboardStats.data} />
    if (tab === "apiTokens") return <ApiTokensSection items={apiTokens.data?.items || []} loading={apiTokens.isLoading} pending={createApiToken.isPending || updateApiToken.isPending || deleteApiToken.isPending} onCreate={(payload) => createApiToken.mutateAsync(payload)} onUpdate={(id, payload) => updateApiToken.mutate({ id, payload })} onDelete={(id) => deleteApiToken.mutate(id)} onCopy={copy} />
    return null
  }
  function visibleTabQueries(): RetryableQuery[] {
    if (tab === "profile") {
      if (accountTab === "security") return []
      if (accountTab === "account") return [mailboxes, accountStats]
      if (accountTab === "mail") return [mailboxes, labels, signatures]
      return [mailboxes, publicSettings]
    }
    if (tab === "mailboxes") return [mailboxes, mailboxApplyOptions, publicSettings, externalImapAccounts, externalRunFolders, externalSyncRuns]
    if (tab === "contacts") return [contacts]
    if (tab === "cleanup" || tab === "cleanupQueue") return [mailboxes, mailboxStats]
    if (tab === "rules") return [mailboxes, rules, ruleForwarding, labels]
    if (tab === "blocked") return [mailboxes, blocked, blockedStats]
    if (tab === "stats") return [mailboxes, dashboardStats]
    if (tab === "apiTokens") return [apiTokens]
    return []
  }
}

function ProfileSectionLoading() {
  return (
    <div className="space-y-4" aria-label="正在加载页面数据" aria-busy="true">
      <Skeleton className="h-40 w-full" />
      <Skeleton className="h-56 w-full" />
      <span className="sr-only">加载中...</span>
    </div>
  )
}

function contentFrameClass(tab: Tab) {
  return cn(
    "w-full",
    tab === "mailboxes" ? "pt-4 sm:pt-6" : "pt-6",
    tab === "profile" ? "mx-auto max-w-[896px]" :
      tab === "mailboxes" ? "mx-auto max-w-5xl px-4 sm:px-6" :
      tab === "stats" ? "px-4 sm:px-6" :
      tab === "rules" || tab === "apiTokens" ? "mx-auto max-w-[896px] px-4 sm:px-0" :
      "mx-auto max-w-[1024px] px-4 sm:px-0",
  )
}

function SettingsPageHeader({ title, subtitle, action, activeTab, onAccountTabChange }: { title: string; subtitle?: string; action?: React.ReactNode; activeTab?: AccountSettingsTab; onAccountTabChange: (tab: AccountSettingsTab) => void }) {
  return (
    <div className="border-b px-4 py-4 sm:px-6">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="min-w-0">
          <h1 className="text-[20px] font-semibold leading-7">{title}</h1>
          {subtitle && <p className="text-sm leading-5 text-muted-foreground">{subtitle}</p>}
        </div>
        {action && <div className="shrink-0">{action}</div>}
      </div>
      {activeTab && (
        <div className="profile-settings-tabs mt-3 flex overflow-x-auto">
          {accountSettingTabs.map((item) => (
            <button
              key={item.key}
              type="button"
              className={cn(
                "profile-settings-tab h-[38px] shrink-0 px-4 text-sm font-medium transition-colors",
                activeTab === item.key ? "border-[hsl(var(--sidebar-active-foreground))] bg-[hsl(var(--sidebar-active))] text-[hsl(var(--sidebar-active-foreground))]" : "border-transparent text-muted-foreground hover:text-foreground",
              )}
              data-active={activeTab === item.key}
              onClick={() => onAccountTabChange(item.key)}
            >
              {item.label}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

function StatsRangeTabs({ rangeDays, onRangeChange }: { rangeDays: number; onRangeChange: (days: number) => void }) {
  return (
    <div className="grid grid-cols-4 gap-1 sm:flex">
      {[
        [7, "7天"],
        [30, "30天"],
        [90, "90天"],
        [365, "365天"],
      ].map(([value, label]) => (
        <button
          key={value}
          type="button"
          className={cn("h-[30px] rounded-md border px-3 text-xs font-normal transition-colors", rangeDays === value ? "border-primary bg-primary text-primary-foreground" : "bg-background text-foreground hover:bg-muted")}
          onClick={() => onRangeChange(Number(value))}
        >
          {label}
        </button>
      ))}
    </div>
  )
}

type AccountSettingsSectionProps = {
  activeTab: AccountSettingsTab
  user: { id: string; loginName?: string; email: string; displayName: string; role: string; disabled: boolean; twoFactorEnabled: boolean; createdAt: string; limits?: PermissionLimits }
  profile: { mutate: (form: FormData) => void; isPending: boolean }
  password: { mutate: (form: FormData) => void; isPending: boolean }
  passwordFormRef: React.RefObject<HTMLFormElement>
  stats?: MailStats
  showStats: boolean
  displayMode: DisplayMode
  onDisplayModeChange: (mode: DisplayMode) => void
  twoFactorFormRef: React.RefObject<HTMLFormElement>
  setupTwoFactor: { data?: { secret: string; otpauthUrl: string }; mutate: () => void; reset: () => void; isPending: boolean }
  enableTwoFactor: { mutate: (form: FormData) => void; isPending: boolean }
  disableTwoFactor: { mutate: (form: FormData) => void; isPending: boolean }
  twoFactorRecoveryCodes: string[]
  onCopy: (text: string) => void
  mailboxes: Mailbox[]
  selectedMailboxId: string
  selectedMailbox?: Mailbox
  labels: MailLabel[]
  labelsLoading: boolean
  labelsPending: boolean
  onCreateLabel: (form: FormData) => void
  onDeleteLabel: (id: string) => void
  signatures: MailSignature[]
  signaturesLoading: boolean
  signaturesPending: boolean
  onCreateSignature: (form: FormData) => void
  onUpdateSignature: (id: string, form: FormData) => void
  onSetDefaultSignature: (id: string) => void
  onDeleteSignature: (id: string) => void
  clientHostname?: string
  onSelectMailbox: (id: string) => void
  onOpenCleanup: () => void
}

function AccountSettingsSection(props: AccountSettingsSectionProps) {
  let content: React.ReactNode
  if (props.activeTab === "mail") {
    content = (
      <MailPreferencesSection
        labels={props.labels}
        labelsLoading={props.labelsLoading}
        labelsPending={props.labelsPending}
        onCreateLabel={props.onCreateLabel}
        onDeleteLabel={props.onDeleteLabel}
        selectedMailbox={props.selectedMailbox}
        signatures={props.signatures}
        signaturesLoading={props.signaturesLoading}
        signaturesPending={props.signaturesPending}
        mailboxes={props.mailboxes}
        onCreateSignature={props.onCreateSignature}
        onUpdateSignature={props.onUpdateSignature}
        onSetDefaultSignature={props.onSetDefaultSignature}
        onDeleteSignature={props.onDeleteSignature}
      />
    )
  } else if (props.activeTab === "clients") {
    content = <ClientSettingsSection mailboxes={props.mailboxes} selectedMailboxId={props.selectedMailboxId} hostname={props.clientHostname} onSelectMailbox={props.onSelectMailbox} onCopy={props.onCopy} />
  } else if (props.activeTab === "security") {
    content = (
      <SecuritySettingsSection
        user={props.user}
        password={props.password}
        passwordFormRef={props.passwordFormRef}
        twoFactorFormRef={props.twoFactorFormRef}
        setupTwoFactor={props.setupTwoFactor}
        enableTwoFactor={props.enableTwoFactor}
        disableTwoFactor={props.disableTwoFactor}
        recoveryCodes={props.twoFactorRecoveryCodes}
        onCopy={props.onCopy}
      />
    )
  } else {
    content = (
      <AccountTabSection
        user={props.user}
        profile={props.profile}
        stats={props.stats}
        showStats={props.showStats}
        displayMode={props.displayMode}
        onDisplayModeChange={props.onDisplayModeChange}
        selectedMailbox={props.selectedMailbox}
        mailboxes={props.mailboxes}
        onOpenCleanup={props.onOpenCleanup}
      />
    )
  }
  return <div className="profile-account-workspace">{content}</div>
}

function SettingsCard({ title, subtitle, action, children, className, contentClassName, surface = "card" }: { title: string; subtitle?: string; action?: React.ReactNode; children: React.ReactNode; className?: string; contentClassName?: string; surface?: "card" | "section" }) {
  return (
    <section
      className={cn(
        surface === "section"
          ? "profile-settings-section"
          : "rounded-lg border bg-card shadow-[0_1px_2px_rgba(15,23,42,0.04)]",
        className,
      )}
    >
      <div className="flex flex-col gap-3 px-6 py-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <h2 className="break-words text-[15px] font-semibold leading-6 text-foreground [overflow-wrap:anywhere]">{title}</h2>
          {subtitle && <p className="mt-0.5 break-words text-xs leading-5 text-muted-foreground [overflow-wrap:anywhere]">{subtitle}</p>}
        </div>
        {action && <div className="w-full shrink-0 sm:w-auto sm:justify-end [&>a]:w-full [&>button]:w-full sm:[&>a]:w-auto sm:[&>button]:w-auto">{action}</div>}
      </div>
      <div className={cn("px-6 pb-5", contentClassName)}>{children}</div>
    </section>
  )
}

function ProfileQueryFailure({ errors, onRetry }: { errors: Error[]; onRetry: () => void }) {
  const messages = Array.from(new Set(errors.map((error) => error.message).filter(Boolean)))
  return (
    <div className="rounded-lg border border-destructive/40 bg-destructive/5 px-6 py-8 text-center" role="alert">
      <div className="text-base font-semibold text-destructive">页面数据读取失败</div>
      <div className="mt-2 break-words text-sm text-muted-foreground">{messages.join("；") || "请检查网络连接后重试"}</div>
      <Button type="button" variant="outline" className="mt-5" onClick={onRetry}>
        <RefreshCcw className="h-4 w-4" />重新读取
      </Button>
    </div>
  )
}

function AccountTabSection({ user, profile, stats, showStats, displayMode, onDisplayModeChange, selectedMailbox, mailboxes, onOpenCleanup }: { user: AccountSettingsSectionProps["user"]; profile: AccountSettingsSectionProps["profile"]; stats?: MailStats; showStats: boolean; displayMode: DisplayMode; onDisplayModeChange: (mode: DisplayMode) => void; selectedMailbox?: Mailbox; mailboxes: Mailbox[]; onOpenCleanup: () => void }) {
  const accountName = user.email
  const quotaBytes = stats?.quotaBytes || (selectedMailbox?.quotaMb ? selectedMailbox.quotaMb * 1024 * 1024 : 0)
  const storageBytes = stats?.storageBytes || 0
  const quotaPct = quotaBytes > 0 ? Math.min(100, Math.round((storageBytes / quotaBytes) * 100)) : 0
  return (
    <div className="profile-continuous-panel">
      <SettingsCard title="账号信息" surface="section">
        <form className="space-y-5" onSubmit={(event) => { event.preventDefault(); profile.mutate(new FormData(event.currentTarget)) }}>
          <InfoLine label="主登录邮箱" value={accountName} />
          <div className="grid gap-2 sm:grid-cols-[10rem_minmax(0,1fr)] sm:items-center">
            <Label htmlFor="profile-display-name" className="text-base font-normal text-muted-foreground">显示名称</Label>
            <div className="flex flex-col gap-2 sm:ml-auto sm:w-[320px] sm:flex-row">
              <Input id="profile-display-name" name="displayName" defaultValue={user.displayName} maxLength={80} required className="h-9" />
              <Button type="submit" size="sm" disabled={profile.isPending}>{profile.isPending ? "保存中..." : "保存"}</Button>
            </div>
          </div>
        </form>
      </SettingsCard>

      <SettingsCard title="邮件列表显示" surface="section">
        <div className="grid grid-cols-2 gap-1 rounded-lg bg-muted p-1" role="group" aria-label="邮件列表显示模式">
          {(["detailed", "compact"] as DisplayMode[]).map((mode) => (
            <button
              key={mode}
              type="button"
              className={cn("h-9 rounded-md px-3 text-sm font-medium transition-colors", displayMode === mode ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground")}
              aria-pressed={displayMode === mode}
              onClick={() => onDisplayModeChange(mode)}
            >
              {mode === "detailed" ? "标准布局" : "紧凑布局"}
            </button>
          ))}
        </div>
      </SettingsCard>

      {showStats && <SettingsCard title="存储容量" surface="section" action={<Button type="button" variant="outline" size="sm" onClick={onOpenCleanup}>邮件清理</Button>}>
        <div className="mb-2 flex items-center justify-between text-sm text-muted-foreground">
          <span>{quotaBytes > 0 ? `${formatBytes(storageBytes)} / ${formatBytes(quotaBytes)}` : formatBytes(storageBytes)}</span>
          <span>{quotaBytes > 0 ? `${quotaPct}%` : "不限"}</span>
        </div>
        <div className="h-2 overflow-hidden rounded-full bg-muted">
          <div className="h-full rounded-full bg-primary transition-all" style={{ width: `${quotaBytes > 0 ? quotaPct : 12}%` }} />
        </div>
      </SettingsCard>}

      <SettingsCard title="账号配额" surface="section" action={<span className="pt-1 text-sm text-muted-foreground">实时按当前账号配置计算</span>}>
        <div className="grid gap-3 md:grid-cols-2">
          <QuotaBox title="邮箱创建" lines={[`当前拥有 ${mailboxes.length} 个邮箱`, user.limits?.maxMailboxCount ? `最多可添加 ${user.limits.maxMailboxCount} 个邮箱` : "管理员不限制邮箱数量", user.limits?.maxMailboxCount ? "达到上限后不可继续自助申请" : "可继续添加邮箱"]} highlight={user.limits?.maxMailboxCount ? "普通额度" : "管理员无限"} />
          <QuotaBox title="验证邮箱" lines={["已绑定主账号邮箱", "可继续添加验证邮箱"]} />
          <QuotaBox title="发信频率" lines={[`每 24 小时 最多 ${user.limits?.smtpDailyLimit || "不限"} 封邮件`, `每分钟最多 ${user.limits?.smtpMinuteLimit || "不限"} 封`]} />
          <QuotaBox title="协议访问频率" lines={[`IMAP：每 1 分钟 最多 ${user.limits?.imapMinuteLimit || "不限"} 次命令`, `POP3：每 1 分钟 最多 ${user.limits?.pop3MinuteLimit || "不限"} 次命令`]} />
          <QuotaBox className="md:col-span-2" title="附件与应用密码" lines={[`单封附件上限 ${user.limits?.maxAttachmentMb || "不限"} MB`, "客户端访问使用邮箱登录密码或系统分配密码"]} />
        </div>
      </SettingsCard>

    </div>
  )
}

function InfoLine({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="grid gap-2 sm:grid-cols-[10rem_minmax(0,1fr)] sm:items-center">
      <div className="text-base text-muted-foreground">{label}</div>
      <div className="min-w-0 truncate text-base font-semibold sm:text-right">{value}</div>
    </div>
  )
}

function QuotaBox({ title, lines, highlight, className }: { title: string; lines: string[]; highlight?: string; className?: string }) {
  return (
    <div className={cn("rounded-lg border p-4", className)}>
      <div className="mb-3 flex items-center gap-2">
        <div className="text-base font-semibold">{title}</div>
        {highlight && <Badge variant="secondary" className="text-[10px]">{highlight}</Badge>}
      </div>
      <div className="space-y-1 text-sm text-muted-foreground">
        {lines.map((line) => <div key={line}>{line}</div>)}
      </div>
    </div>
  )
}

function MailPreferencesSection({
  labels,
  labelsLoading,
  labelsPending,
  onCreateLabel,
  onDeleteLabel,
  selectedMailbox,
  signatures,
  signaturesLoading,
  signaturesPending,
  mailboxes,
  onCreateSignature,
  onUpdateSignature,
  onSetDefaultSignature,
  onDeleteSignature,
}: {
  labels: MailLabel[]
  labelsLoading: boolean
  labelsPending: boolean
  onCreateLabel: (form: FormData) => void
  onDeleteLabel: (id: string) => void
  selectedMailbox?: Mailbox
  signatures: MailSignature[]
  signaturesLoading: boolean
  signaturesPending: boolean
  mailboxes: Mailbox[]
  onCreateSignature: (form: FormData) => void
  onUpdateSignature: (id: string, form: FormData) => void
  onSetDefaultSignature: (id: string) => void
  onDeleteSignature: (id: string) => void
}) {
  const [labelColor, setLabelColor] = React.useState("#3b82f6")
  const [signatureMailboxId, setSignatureMailboxId] = React.useState("all")
  const [signatureDefault, setSignatureDefault] = React.useState(false)
  const [editingSignature, setEditingSignature] = React.useState<MailSignature | null>(null)
  const [pendingConfirm, setPendingConfirm] = React.useState<PendingConfirm | null>(null)

  function submitLabel(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!selectedMailbox) return
    const form = new FormData(event.currentTarget)
    form.set("color", labelColor)
    onCreateLabel(form)
    event.currentTarget.reset()
    setLabelColor("#3b82f6")
  }

  function submitSignature(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    form.set("mailboxId", signatureMailboxId === "all" ? "" : signatureMailboxId)
    form.set("isDefault", signatureDefault ? "on" : "")
    onCreateSignature(form)
    event.currentTarget.reset()
    setSignatureMailboxId("all")
    setSignatureDefault(false)
  }

  return (
    <div className="space-y-6">
      <SettingsCard title="标签管理">
        <form className="flex gap-2" onSubmit={submitLabel}>
          <Input name="name" className="h-[42px] flex-1 text-base" placeholder={selectedMailbox ? "标签名称" : "请先选择邮箱"} disabled={!selectedMailbox || labelsPending} required />
          <Input type="color" value={labelColor} onChange={(event) => setLabelColor(event.target.value)} className="h-10 w-12 cursor-pointer bg-background p-1" aria-label="标签颜色" />
          <Button className="h-10 px-4" disabled={!selectedMailbox || labelsPending}>{labelsPending ? "创建中" : "创建"}</Button>
        </form>
        <div className="mt-4 flex flex-wrap gap-2">
          {labels.map((label) => (
            <span key={label.id} className="inline-flex h-8 items-center gap-2 rounded-full border px-3 text-sm">
              <span className="size-3 rounded-full" style={{ backgroundColor: label.color || "#64748b" }} />
              {label.name}
              <Button type="button" variant="ghost" size="icon" className="h-5 w-5 p-0 text-muted-foreground shadow-none hover:text-destructive" disabled={labelsPending} onClick={() => onDeleteLabel(label.id)} aria-label={`删除标签 ${label.name}`}>
                <X className="h-3.5 w-3.5" />
              </Button>
            </span>
          ))}
          {!labelsLoading && labels.length === 0 && <span className="text-sm text-muted-foreground">暂无标签</span>}
        </div>
      </SettingsCard>

      <SettingsCard title="签名管理" subtitle="支持全局签名和按发件邮箱绑定的默认签名。" action={<span className="pt-1 text-sm text-muted-foreground">共 {signatures.length} 个签名</span>}>
        <form className="rounded-lg border p-4" onSubmit={submitSignature}>
          <div className="grid gap-3 md:grid-cols-2">
            <Field label="签名名称"><Input name="name" className="h-10" required placeholder="例如：默认签名" /></Field>
            <Field label="绑定邮箱"><MailboxSelect value={signatureMailboxId} mailboxes={mailboxes} onChange={setSignatureMailboxId} /></Field>
          </div>
          <Field label="签名内容">
            <Textarea name="content" required className="min-h-[138px] text-base" placeholder="支持多行文本，写信时会自动转换为 HTML" />
          </Field>
          <label className="mb-4 mt-4 flex items-center gap-2 text-sm">
            <Checkbox checked={signatureDefault} onCheckedChange={(checked) => setSignatureDefault(checked === true)} />
            设为默认签名
          </label>
          <Button disabled={signaturesPending}>{signaturesPending ? "保存中..." : "创建签名"}</Button>
        </form>
        <div className="mt-4 space-y-3">
          {signatures.map((item) => {
            const mailbox = item.mailboxId ? mailboxes.find((m) => m.id === item.mailboxId)?.address || "未知邮箱" : "全局签名"
            return (
              <div key={item.id} className="rounded-lg border p-4">
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2 text-sm font-medium">
                      <span>{item.name}</span>
                      {item.isDefault && <Badge>默认</Badge>}
                      <Badge variant="outline">{mailbox}</Badge>
                    </div>
                    <p className="mt-2 whitespace-pre-wrap text-sm text-muted-foreground">{item.content}</p>
                  </div>
                  <div className="flex shrink-0 gap-1">
                    {!item.isDefault && <Button type="button" variant="outline" size="sm" disabled={signaturesPending} onClick={() => onSetDefaultSignature(item.id)}>设为默认</Button>}
                    <Button type="button" variant="ghost" size="icon" className="size-8" aria-label={`编辑签名 ${item.name}`} title="编辑签名" onClick={() => setEditingSignature(item)}><PencilLine className="h-4 w-4" /></Button>
                    <Button type="button" variant="ghost" size="icon" className="size-8 text-destructive" aria-label={`删除签名 ${item.name}`} title="删除签名" onClick={() => setPendingConfirm({ title: "删除签名？", description: `签名“${item.name}”将被删除。`, confirmText: "删除签名", destructive: true, onConfirm: () => { onDeleteSignature(item.id); setPendingConfirm(null) } })}><Trash2 className="h-4 w-4" /></Button>
                  </div>
                </div>
              </div>
            )
          })}
          {!signaturesLoading && signatures.length === 0 && <div className="py-5 text-center text-sm text-muted-foreground">暂无签名</div>}
        </div>
      </SettingsCard>

      <Dialog open={!!editingSignature} onOpenChange={(open) => { if (!open) setEditingSignature(null) }}>
        <DialogContent className="max-h-[92dvh] overflow-y-auto sm:max-w-2xl">
          <DialogHeader><DialogTitle>编辑签名</DialogTitle></DialogHeader>
          {editingSignature && (
            <EditSignatureForm
              key={editingSignature.id}
              item={editingSignature}
              mailboxes={mailboxes}
              pending={signaturesPending}
              onCancel={() => setEditingSignature(null)}
              onSubmit={(id, form) => { onUpdateSignature(id, form); setEditingSignature(null) }}
            />
          )}
        </DialogContent>
      </Dialog>
      <ConfirmDialog open={!!pendingConfirm} title={pendingConfirm?.title || ""} description={pendingConfirm?.description} confirmText={pendingConfirm?.confirmText || "删除"} destructive={!!pendingConfirm?.destructive} pending={signaturesPending} onOpenChange={(open) => { if (!open) setPendingConfirm(null) }} onConfirm={() => pendingConfirm?.onConfirm()} />
    </div>
  )
}

function EditSignatureForm({ item, mailboxes, pending, onCancel, onSubmit }: { item: MailSignature; mailboxes: Mailbox[]; pending: boolean; onCancel: () => void; onSubmit: (id: string, form: FormData) => void }) {
  const [mailboxId, setMailboxId] = React.useState(item.mailboxId || "all")
  const [isDefault, setIsDefault] = React.useState(item.isDefault)
  return (
    <form className="space-y-4" onSubmit={(event) => { event.preventDefault(); const form = new FormData(event.currentTarget); form.set("mailboxId", mailboxId === "all" ? "" : mailboxId); form.set("isDefault", isDefault ? "on" : ""); onSubmit(item.id, form) }}>
      <div className="grid gap-4 md:grid-cols-2">
        <Field label="签名名称"><Input name="name" defaultValue={item.name} required /></Field>
        <Field label="绑定邮箱"><MailboxSelect value={mailboxId} mailboxes={mailboxes} onChange={setMailboxId} /></Field>
      </div>
      <Field label="签名内容"><Textarea name="content" required className="min-h-44" defaultValue={item.content} /></Field>
      <label className="flex items-center gap-3 text-sm font-medium">
        <Checkbox checked={isDefault} onCheckedChange={(value) => setIsDefault(value === true)} />
        <span>设为当前范围默认签名</span>
      </label>
      <DialogFooter className="gap-2 [&>button]:w-full sm:[&>button]:w-auto">
        <Button type="button" variant="outline" onClick={onCancel}>取消</Button>
        <Button disabled={pending}>{pending ? "保存中..." : "保存修改"}</Button>
      </DialogFooter>
    </form>
  )
}

function SecuritySettingsSection({ user, password, passwordFormRef, twoFactorFormRef, setupTwoFactor, enableTwoFactor, disableTwoFactor, recoveryCodes, onCopy }: { user: AccountSettingsSectionProps["user"]; password: AccountSettingsSectionProps["password"]; passwordFormRef: React.RefObject<HTMLFormElement>; twoFactorFormRef: React.RefObject<HTMLFormElement>; setupTwoFactor: AccountSettingsSectionProps["setupTwoFactor"]; enableTwoFactor: AccountSettingsSectionProps["enableTwoFactor"]; disableTwoFactor: AccountSettingsSectionProps["disableTwoFactor"]; recoveryCodes: string[]; onCopy: (text: string) => void }) {
  return (
    <div className="space-y-6">
      <SettingsCard title="当前登录" contentClassName="border-t py-5">
        <div className="flex items-center gap-4">
          <div className="flex size-10 items-center justify-center rounded-full bg-emerald-100 text-emerald-700"><ShieldCheck className="h-5 w-5" /></div>
          <div>
            <div className="font-semibold">{user.email}</div>
            <div className="text-sm text-muted-foreground">当前已登录账号</div>
          </div>
        </div>
      </SettingsCard>

      <SettingsCard title="密码管理">
        <form ref={passwordFormRef} className="space-y-4" onSubmit={(e) => { e.preventDefault(); password.mutate(new FormData(e.currentTarget)) }}>
          <Field label="当前密码"><PasswordInput name="currentPassword" required /></Field>
          <Field label="新密码"><PasswordInput name="newPassword" minLength={6} required placeholder="输入新密码" /></Field>
          <Field label="确认新密码"><PasswordInput name="confirmPassword" minLength={6} required placeholder="再次输入密码" /></Field>
          <Button disabled={password.isPending}>{password.isPending ? "设置中..." : "设置密码"}</Button>
        </form>
      </SettingsCard>

      <SettingsCard title="两步验证">
        <div className="mb-4 flex items-center justify-between rounded-lg border p-3">
          <div className="flex items-center gap-2 text-sm"><KeyRound className="h-4 w-4" />认证状态</div>
          <Badge variant={user.twoFactorEnabled ? "default" : "secondary"}>{user.twoFactorEnabled ? "已启用" : "未启用"}</Badge>
        </div>
        {!user.twoFactorEnabled && !setupTwoFactor.data && (
          <Button onClick={() => setupTwoFactor.mutate()} disabled={setupTwoFactor.isPending}>{setupTwoFactor.isPending ? "生成中..." : "启用两步验证"}</Button>
        )}
        {!user.twoFactorEnabled && setupTwoFactor.data && (
          <form ref={twoFactorFormRef} className="space-y-4" onSubmit={(e) => { e.preventDefault(); enableTwoFactor.mutate(new FormData(e.currentTarget)) }}>
            <div className="grid gap-4 lg:grid-cols-[220px_minmax(0,1fr)]">
              <div className="flex justify-center rounded-lg border bg-white p-4">
                <QRCodeSVG value={setupTwoFactor.data.otpauthUrl} size={184} level="M" />
              </div>
              <div className="space-y-4">
                <Field label="密钥">
                  <div className="flex gap-2">
                    <Input value={setupTwoFactor.data.secret} readOnly />
                    <Button type="button" variant="outline" onClick={() => onCopy(setupTwoFactor.data!.secret)}><Copy className="h-4 w-4" />复制</Button>
                  </div>
                </Field>
                <Field label="绑定地址">
                  <div className="flex gap-2">
                    <Input value={setupTwoFactor.data.otpauthUrl} readOnly />
                    <Button type="button" variant="outline" onClick={() => onCopy(setupTwoFactor.data!.otpauthUrl)}><Copy className="h-4 w-4" />复制</Button>
                  </div>
                </Field>
              </div>
            </div>
            <Field label="验证码"><Input name="code" inputMode="numeric" autoComplete="one-time-code" minLength={6} maxLength={6} required /></Field>
            <div className="flex justify-end gap-2">
              <Button type="button" variant="outline" onClick={() => setupTwoFactor.reset()}>取消</Button>
              <Button disabled={enableTwoFactor.isPending}>{enableTwoFactor.isPending ? "启用中..." : "确认启用"}</Button>
            </div>
          </form>
        )}
        {user.twoFactorEnabled && (
          <div className="space-y-4">
            {recoveryCodes.length > 0 && (
              <div className="rounded-lg border bg-muted/30 p-4">
                <div className="mb-3 flex items-center justify-between gap-3">
                  <div>
                    <div className="text-sm font-semibold">恢复码</div>
                    <div className="text-xs text-muted-foreground">每个恢复码只能使用一次。</div>
                  </div>
                  <Button type="button" variant="outline" size="sm" onClick={() => onCopy(recoveryCodes.join("\n"))}><Copy className="h-4 w-4" />复制</Button>
                </div>
                <div className="grid gap-2 sm:grid-cols-2">
                  {recoveryCodes.map((code) => <code key={code} className="rounded-md bg-background px-3 py-2 text-sm font-semibold">{code}</code>)}
                </div>
              </div>
            )}
            <form ref={twoFactorFormRef} className="space-y-4" onSubmit={(e) => { e.preventDefault(); disableTwoFactor.mutate(new FormData(e.currentTarget)) }}>
              <Field label="当前验证码或恢复码"><Input name="code" autoComplete="one-time-code" minLength={6} required /></Field>
              <div className="flex justify-end">
                <Button variant="destructive" disabled={disableTwoFactor.isPending}>{disableTwoFactor.isPending ? "关闭中..." : "关闭两步验证"}</Button>
              </div>
            </form>
          </div>
        )}
      </SettingsCard>

    </div>
  )
}

function CleanupQueueSection({ mailbox, stats }: { mailbox?: Mailbox; stats?: MailStats }) {
  const rows = (stats?.byFolder || []).filter((item) => item.role === "trash" || item.role === "spam").map((item) => ({ name: folderLabel(item.folder), count: item.count, bytes: item.bytes }))
  return (
    <SettingsCard title="待清理邮件" subtitle={mailbox ? `当前邮箱：${mailbox.address}` : "请先选择邮箱"}>
      <div className="space-y-2">
        {rows.map((row) => (
          <div key={row.name} className="grid grid-cols-[1fr_auto_auto] items-center gap-3 rounded-lg border p-3 text-sm">
            <span className="font-medium">{row.name}</span>
            <Badge variant="secondary">{row.count} 封</Badge>
            <span className="text-muted-foreground">{formatBytes(row.bytes)}</span>
          </div>
        ))}
        {rows.length === 0 && <EmptyState text="暂无待清理邮件" />}
      </div>
    </SettingsCard>
  )
}

function MailboxManagement({
  mailboxes,
  applyOptions,
  applyPending,
  canConfigureApply,
  selectedMailboxId,
  externalImapEnabled,
  externalAccounts,
  externalPending,
  selectedExternalRunAccountId,
  externalRunFolders,
  externalSyncRuns,
  onSelectExternalRunAccount,
  onSelect,
  onOpen,
  onApply,
  onConfigureApply,
  onCreateExternal,
  onStartExternalOAuth,
  onUpdateExternal,
  onDeleteExternal,
  onTestExternal,
  onSyncExternal,
  onSyncExternalFolder,
}: {
  mailboxes: Mailbox[]
  applyOptions?: MailboxApplyOptions
  applyPending: boolean
  canConfigureApply: boolean
  selectedMailboxId: string
  externalImapEnabled: boolean
  externalAccounts: ExternalImapAccount[]
  externalPending: boolean
  selectedExternalRunAccountId: string
  externalRunFolders: ExternalImapFolder[]
  externalSyncRuns: ExternalImapSyncRun[]
  onSelectExternalRunAccount: (id: string) => void
  onSelect: (id: string) => void
  onOpen: (id: string) => void
  onApply: (payload: { domainId: string; localPart: string; displayName: string }) => Promise<void>
  onConfigureApply: () => void
  onCreateExternal: (payload: ExternalImapAccountPayload) => void
  onStartExternalOAuth: (provider: ExternalImapOAuthProvider, payload: { mailboxId: string; email: string; storageMode: ExternalImapStorageMode }) => void
  onUpdateExternal: (id: string, payload: ExternalImapAccountPayload) => void
  onDeleteExternal: (id: string) => void
  onTestExternal: (id: string) => void
  onSyncExternal: (id: string) => void
  onSyncExternalFolder: (id: string, folder: string) => void
}) {
  const qc = useQueryClient()
  const { toast } = useToast()
  const isMobile = useIsMobile()
  const [mailboxViewParams, setMailboxViewParams] = useSearchParams()
  const rawMailboxView = mailboxViewParams.get("mailboxView")
  const mailboxView: MailboxView = rawMailboxView === "forwarding" || rawMailboxView === "verified" ? rawMailboxView : "mine"
  const canApply = !!applyOptions?.enabled && (applyOptions.domains || []).length > 0
  const [domainId, setDomainId] = React.useState(() => applyOptions?.domains?.[0]?.id || "")
  const [localPart, setLocalPart] = React.useState("")
  const [mailboxSearch, setMailboxSearch] = React.useState("")
  const [mailboxPage, setMailboxPage] = React.useState(1)
  const [mailboxPageSize, setMailboxPageSize] = React.useState(10)
  const [singleForwardSearch, setSingleForwardSearch] = React.useState("")
  const [singleForwardPage, setSingleForwardPage] = React.useState(1)
  const [verifiedSearch, setVerifiedSearch] = React.useState("")
  const [verifiedPage, setVerifiedPage] = React.useState(1)
  const [verifiedPageSize, setVerifiedPageSize] = React.useState(10)
  const [forwardingMailbox, setForwardingMailbox] = React.useState<Mailbox | null>(null)
  const [forwardDraft, setForwardDraft] = React.useState<string[]>([])
  const [forwardingTargetQuery, setForwardingTargetQuery] = React.useState("")
  const [forwardingVerificationCandidate, setForwardingVerificationCandidate] = React.useState("")
  const [forwardingTargetPreview, setForwardingTargetPreview] = React.useState<{ title: string; subtitle: string; source: string; targets: string[] } | null>(null)
  const [accountForwardTargets, setAccountForwardTargets] = React.useState<string[]>([])
  const [accountTargetQuery, setAccountTargetQuery] = React.useState("")
  const [addVerifiedOpen, setAddVerifiedOpen] = React.useState(false)
  const [mobileCreateOpen, setMobileCreateOpen] = React.useState(false)
  const [verifiedEmailDraft, setVerifiedEmailDraft] = React.useState("")
  const [pendingVerifiedDelete, setPendingVerifiedDelete] = React.useState<ForwardingVerifiedEmail | null>(null)
  const [pendingExternalDelete, setPendingExternalDelete] = React.useState<ExternalImapAccount | null>(null)
  const forwarding = useQuery({ queryKey: ["forwarding-settings"], queryFn: api.forwardingSettings, enabled: mailboxes.length > 0 })
  const verifiedEmailItems = React.useMemo(() => [...(forwarding.data?.verifiedEmails || [])], [forwarding.data?.verifiedEmails])
  const verifiedEmails = React.useMemo(() => sortForwardingTargets(verifiedEmailItems.filter((item) => item.verified).map((item) => item.email)), [verifiedEmailItems])
  const pendingVerifiedEmailItems = React.useMemo(() => verifiedEmailItems
    .filter((item) => !item.verified)
    .sort((a, b) => forwardingItemTime(b, false) - forwardingItemTime(a, false)), [verifiedEmailItems])
  const completedVerifiedEmailItems = React.useMemo(() => verifiedEmailItems
    .filter((item) => item.verified)
    .sort((a, b) => forwardingItemTime(b, true) - forwardingItemTime(a, true)), [verifiedEmailItems])
  const normalizedVerifiedEmailDraft = verifiedEmailDraft.trim().toLowerCase()
  const normalizedVerifiedSearch = verifiedSearch.trim().toLowerCase()
  const matchingPendingVerifiedEmailItems = React.useMemo(() => normalizedVerifiedSearch
    ? pendingVerifiedEmailItems.filter((item) => item.email.toLowerCase().includes(normalizedVerifiedSearch))
    : pendingVerifiedEmailItems, [normalizedVerifiedSearch, pendingVerifiedEmailItems])
  const matchingCompletedVerifiedEmailItems = React.useMemo(() => normalizedVerifiedSearch
    ? completedVerifiedEmailItems.filter((item) => item.email.toLowerCase().includes(normalizedVerifiedSearch))
    : completedVerifiedEmailItems, [completedVerifiedEmailItems, normalizedVerifiedSearch])
  const verifiedEmailDraftExists = verifiedEmailItems.some((item) => item.email.toLowerCase() === normalizedVerifiedEmailDraft)
  const hasPendingVerifiedEmails = verifiedEmailItems.some((item) => !item.verified)
  const pendingBindings = React.useMemo(() => forwarding.data?.pendingBindings || [], [forwarding.data?.pendingBindings])
  const visiblePendingBindings = React.useMemo(() => pendingBindings.filter((item) => item.status !== "cancelled"), [pendingBindings])
  const mailboxForwards = React.useMemo<Record<string, string[]>>(() => {
    const next: Record<string, string[]> = {}
    for (const rule of forwarding.data?.mailboxRules || []) {
      const targets = forwardingTargetsFromRule(rule)
      if (targets.length > 0) next[rule.mailboxId] = targets
    }
    return next
  }, [forwarding.data?.mailboxRules])
  const normalizedMailboxSearch = mailboxSearch.trim().toLowerCase()
  const domainOptions = applyOptions?.domains || []
  const selectedDomain = domainOptions.find((domain) => domain.id === domainId) || domainOptions[0]
  const selectedMailbox = mailboxes.find((mailbox) => mailbox.id === selectedMailboxId) || mailboxes[0]
  const sortedMailboxes = React.useMemo(() => [...mailboxes].sort((a, b) => forwardingTargetCollator.compare(a.address, b.address)), [mailboxes])
  const filteredMailboxes = normalizedMailboxSearch
    ? sortedMailboxes.filter((mailbox) => mailbox.address.toLowerCase().includes(normalizedMailboxSearch))
    : sortedMailboxes
  const mailboxTotalPages = Math.max(1, Math.ceil(filteredMailboxes.length / mailboxPageSize))
  const pagedMailboxes = filteredMailboxes.slice((mailboxPage - 1) * mailboxPageSize, mailboxPage * mailboxPageSize)
  const forwardedMailboxCount = sortedMailboxes.filter((mailbox) => mergeForwardingTargets(accountForwardTargets, mailboxForwards[mailbox.id] || []).length > 0).length
  const singleForwardMailboxes = React.useMemo(() => {
    const query = singleForwardSearch.trim().toLowerCase()
    return sortedMailboxes.filter((mailbox) => (mailboxForwards[mailbox.id] || []).length > 0 && (!query || mailbox.address.toLowerCase().includes(query)))
  }, [mailboxForwards, singleForwardSearch, sortedMailboxes])
  const singleForwardTotalPages = Math.max(1, Math.ceil(singleForwardMailboxes.length / 10))
  const pagedSingleForwardMailboxes = singleForwardMailboxes.slice((singleForwardPage - 1) * 10, singleForwardPage * 10)
  const verifiedTotalPages = Math.max(1, Math.ceil(matchingCompletedVerifiedEmailItems.length / verifiedPageSize))
  const pagedVerifiedEmailItems = matchingCompletedVerifiedEmailItems.slice((verifiedPage - 1) * verifiedPageSize, verifiedPage * verifiedPageSize)
  const latestVerifiedAt = completedVerifiedEmailItems[0]?.verifiedAt || completedVerifiedEmailItems[0]?.createdAt
  const savedAccountForwardTargets = forwardingTargetsFromSettings(forwarding.data)
  const accountForwardingChanged = accountForwardTargets.join("\n") !== savedAccountForwardTargets.join("\n")
  const setForwardingCache = React.useCallback((settings: ForwardingSettings) => {
    qc.setQueryData(["forwarding-settings"], settings)
  }, [qc])
  const refreshForwardingSettings = React.useCallback(() => {
    void qc.invalidateQueries({ queryKey: ["forwarding-settings"] })
  }, [qc])
  const addVerifiedEmail = useMutation({
    mutationFn: api.addForwardingVerifiedEmail,
    onSuccess: (settings, email) => {
      setForwardingCache(settings)
      refreshForwardingSettings()
      window.setTimeout(refreshForwardingSettings, 2500)
      window.setTimeout(refreshForwardingSettings, 7000)
      setVerifiedEmailDraft("")
      setVerifiedSearch("")
      setAddVerifiedOpen(false)
      const item = settings.verifiedEmails.find((entry) => entry.email.toLowerCase() === email.trim().toLowerCase())
      toast({
        title: item?.deliveryStatus === "failed" ? "验证邮箱已添加，邮件发送失败" : "验证邮件已加入发送队列",
        description: item?.deliveryStatus === "failed" ? item.deliveryError || "请稍后重发验证邮件" : "发送后请检查目标邮箱的收件箱和垃圾邮件",
      })
    },
    onError: (error) => toast({ title: "添加失败", description: error.message }),
  })
  const resendVerifiedEmail = useMutation({
    mutationFn: ({ id }: { id: string; email: string }) => api.resendForwardingVerifiedEmail(id),
    onSuccess: (settings, item) => {
      setForwardingCache(settings)
      refreshForwardingSettings()
      window.setTimeout(refreshForwardingSettings, 2500)
      window.setTimeout(refreshForwardingSettings, 7000)
      const next = settings.verifiedEmails.find((entry) => entry.id === item.id)
      toast({
        title: next?.deliveryStatus === "failed" ? "重发失败" : "验证邮件已重新加入发送队列",
        description: next?.deliveryStatus === "failed" ? next.deliveryError || "请稍后再试" : "发送后请检查目标邮箱的收件箱和垃圾邮件",
      })
    },
    onError: (error) => toast({ title: "重发失败", description: error.message }),
  })
  const deleteVerifiedEmail = useMutation({
    mutationFn: ({ id }: { id: string; email: string }) => api.deleteForwardingVerifiedEmail(id),
    onSuccess: (settings) => {
      setForwardingCache(settings)
      setPendingVerifiedDelete(null)
      toast({ title: "验证邮箱已移除" })
    },
    onError: (error) => toast({ title: "移除失败", description: error.message }),
  })
  const saveAccountForwarding = useMutation({
    mutationFn: api.updateAccountForwarding,
    onSuccess: (settings) => {
      setForwardingCache(settings)
      toast({ title: "账号级转发已保存" })
    },
    onError: (error) => toast({ title: "保存失败", description: error.message }),
  })
  const saveMailboxForwarding = useMutation({
    mutationFn: ({ mailboxId, targetEmails }: { mailboxId: string; targetEmails: string[] }) => api.updateMailboxForwarding(mailboxId, targetEmails),
    onSuccess: (settings) => {
      setForwardingCache(settings)
      setForwardingMailbox(null)
      setForwardDraft([])
      toast({ title: "邮箱转发已保存" })
    },
    onError: (error) => toast({ title: "保存失败", description: error.message }),
  })
  const createPendingBinding = useMutation({
    mutationFn: (payload: { email: string; scope: "account" | "mailbox"; mailboxId?: string }) => api.createForwardingPendingBinding(payload),
    onSuccess: (settings, variables) => {
      setForwardingCache(settings)
      setForwardingVerificationCandidate("")
      setForwardingTargetQuery("")
      setAccountTargetQuery("")
      toast({ title: "验证邮件已发送", description: variables.scope === "account" ? "确认后将自动设为账号级转发目标。" : `确认后将自动添加到 ${forwardingMailbox?.address || variables.mailboxId}，无需再次设置。` })
    },
    onError: (error) => toast({ title: "发送失败", description: error.message }),
  })
  const cancelPendingBinding = useMutation({
    mutationFn: (id: string) => api.deleteForwardingPendingBinding(id),
    onSuccess: (settings) => { setForwardingCache(settings); toast({ title: "待确认转发已取消" }) },
    onError: (error) => toast({ title: "取消失败", description: error.message }),
  })
  const retryPendingBinding = useMutation({
    mutationFn: (id: string) => api.retryForwardingPendingBinding(id),
    onSuccess: (settings) => { setForwardingCache(settings); toast({ title: "转发已经启用" }) },
    onError: (error) => toast({ title: "重试失败", description: error.message }),
  })
  const forwardingBusy = forwarding.isLoading || addVerifiedEmail.isPending || resendVerifiedEmail.isPending || deleteVerifiedEmail.isPending || saveAccountForwarding.isPending || saveMailboxForwarding.isPending || createPendingBinding.isPending || cancelPendingBinding.isPending || retryPendingBinding.isPending

  React.useEffect(() => {
    if (!domainOptions.length) return
    setDomainId((current) => domainOptions.some((domain) => domain.id === current) ? current : domainOptions[0].id)
  }, [domainOptions])

  React.useEffect(() => {
    setAccountForwardTargets(forwardingTargetsFromSettings(forwarding.data))
  }, [forwarding.data?.accountTargetEmail, forwarding.data?.accountTargetEmails])
  const priorBindingStatuses = React.useRef(new Map<string, string>())
  React.useEffect(() => {
    const previous = priorBindingStatuses.current
    for (const binding of visiblePendingBindings) {
      if (previous.get(binding.id) === forwardingBindingPendingStatus && binding.status === "active") {
        toast({ title: `${binding.email} 已完成确认`, description: "转发已经自动启用。" })
      }
    }
    priorBindingStatuses.current = new Map(visiblePendingBindings.map((item) => [item.id, item.status]))
  }, [toast, visiblePendingBindings])
  React.useEffect(() => {
    if (!hasPendingVerifiedEmails && !visiblePendingBindings.some((item) => item.status === forwardingBindingPendingStatus)) return
    const refresh = () => { if (document.visibilityState === "visible") void forwarding.refetch() }
    const onVisibility = () => { if (document.visibilityState === "visible") void forwarding.refetch() }
    const timer = window.setInterval(refresh, 20_000)
    document.addEventListener("visibilitychange", onVisibility)
    return () => { window.clearInterval(timer); document.removeEventListener("visibilitychange", onVisibility) }
  }, [forwarding.refetch, hasPendingVerifiedEmails, visiblePendingBindings])
  React.useEffect(() => { setMailboxPage(1) }, [mailboxPageSize, normalizedMailboxSearch])
  React.useEffect(() => { setMailboxPage((page) => Math.min(page, mailboxTotalPages)) }, [mailboxTotalPages])
  React.useEffect(() => { setSingleForwardPage(1) }, [singleForwardSearch])
  React.useEffect(() => { setSingleForwardPage((page) => Math.min(page, singleForwardTotalPages)) }, [singleForwardTotalPages])
  React.useEffect(() => { setVerifiedPage(1) }, [normalizedVerifiedSearch, verifiedPageSize])
  React.useEffect(() => { setVerifiedPage((page) => Math.min(page, verifiedTotalPages)) }, [verifiedTotalPages])

  function setMailboxView(next: MailboxView) {
    const nextParams = new URLSearchParams(mailboxViewParams)
    if (next === "mine") nextParams.delete("mailboxView")
    else nextParams.set("mailboxView", next)
    setMailboxViewParams(nextParams)
  }

  async function submitMailbox(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!selectedDomain || !localPart.trim()) return
    await onApply({ domainId: selectedDomain.id, localPart: localPart.trim(), displayName: "" })
    setLocalPart("")
    setMobileCreateOpen(false)
  }

  function openMailboxForward(mailbox: Mailbox) {
    const mailboxTargets = mailboxForwards[mailbox.id]
    setForwardingMailbox(mailbox)
    setForwardDraft(withoutForwardingTargets(mailboxTargets || [], accountForwardTargets))
    setForwardingTargetQuery("")
    setForwardingVerificationCandidate("")
  }

  function saveMailboxForward() {
    if (!forwardingMailbox) return
    saveMailboxForwarding.mutate({ mailboxId: forwardingMailbox.id, targetEmails: withoutForwardingTargets(forwardDraft, accountForwardTargets) })
  }

  function submitVerifiedEmail(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const value = verifiedEmailDraft.trim()
    if (!value) return
    addVerifiedEmail.mutate(value)
  }

  function openAddVerifiedEmail() {
    const candidate = verifiedSearch.trim()
    setVerifiedEmailDraft(looksLikeEmailAddress(candidate) ? candidate : "")
    setAddVerifiedOpen(true)
  }

  function removeVerifiedEmail(id: string, email: string) {
    setAccountForwardTargets((items) => items.filter((item) => item !== email))
    setForwardDraft((items) => items.filter((item) => item !== email))
    deleteVerifiedEmail.mutate({ id, email })
  }

  function resendVerification(item: ForwardingVerifiedEmail) {
    resendVerifiedEmail.mutate({ id: item.id, email: item.email })
  }

  function verifiedEmailUsageCount(email: string) {
    const normalized = normalizeForwardingTarget(email)
    let count = accountForwardTargets.some((item) => normalizeForwardingTarget(item) === normalized) ? 1 : 0
    for (const targets of Object.values(mailboxForwards)) if (targets.some((item) => normalizeForwardingTarget(item) === normalized)) count += 1
    return count
  }

  function pendingEmailPurpose(item: ForwardingVerifiedEmail) {
    const bindings = visiblePendingBindings.filter((binding) => binding.verifiedEmailId === item.id && binding.status !== "active")
    if (!bindings.length) return "等待对方确认"
    return bindings.map((binding) => binding.scope === "account" ? "确认后将设为账号级转发目标" : `确认后将自动绑定：${binding.mailboxAddress || binding.mailboxId}`).join(" · ")
  }

  return (
    <div className="space-y-4">
      <section className="overflow-hidden rounded-lg border bg-card">
        <MailboxViewTabs value={mailboxView} onChange={setMailboxView} pendingCount={pendingVerifiedEmailItems.length} />
        {mailboxView === "mine" && (
          <div className="grid grid-cols-3 gap-2 border-t p-3 sm:gap-3 sm:p-4">
            <MailboxStatCard icon={<Mail className="h-5 w-5" />} label="邮箱总数" value={mailboxes.length} tone="primary" />
            <MailboxStatCard icon={<MailCheck className="h-5 w-5" />} label="已验证地址" value={forwarding.isLoading ? "--" : completedVerifiedEmailItems.length} tone="success" />
            <MailboxStatCard icon={<SendHorizontal className="h-5 w-5" />} label="启用转发" value={forwarding.isLoading ? "--" : forwardedMailboxCount} tone="violet" />
          </div>
        )}
        {mailboxView === "verified" && (
          <div className="grid grid-cols-3 gap-2 border-t p-3 sm:gap-3 sm:p-4">
            <MailboxStatCard icon={<MailCheck className="h-5 w-5" />} label="已验证" value={completedVerifiedEmailItems.length} tone="success" />
            <MailboxStatCard icon={<Clock3 className="h-5 w-5" />} label="待验证" value={pendingVerifiedEmailItems.length} tone="warning" />
            <MailboxStatCard icon={<CheckCircle2 className="h-5 w-5" />} label="最近验证" value={latestVerifiedAt ? new Date(latestVerifiedAt).toLocaleDateString() : "--"} tone="violet" compact />
          </div>
        )}
      </section>

      {mailboxView === "mine" && <>
      {isMobile ? (
        <section className="rounded-lg border bg-card p-4">
          <Button type="button" className="h-11 w-full gap-2" onClick={() => setMobileCreateOpen(true)}>
            <Plus className="h-4 w-4" />创建邮箱
          </Button>
          <p className="mt-2 text-sm leading-5 text-muted-foreground">创建新地址时再展开表单，让邮箱列表保持在首屏。</p>
        </section>
      ) : <section className="rounded-lg border bg-card px-5 py-4">
        <h2 className="mb-3 text-lg font-semibold leading-7">创建新邮箱</h2>
        <form className="grid gap-3 md:grid-cols-[minmax(0,1fr)_160px_80px]" onSubmit={submitMailbox}>
          <Input
            value={localPart}
            onChange={(event) => setLocalPart(event.target.value)}
            className="h-[42px] text-base shadow-none"
            placeholder="输入邮箱地址前缀"
            disabled={!canApply || applyPending}
          />
          <select
            value={selectedDomain?.id || ""}
            onChange={(event) => setDomainId(event.target.value)}
            className="h-[42px] rounded-md border border-input bg-background px-3 text-sm outline-none focus:ring-1 focus:ring-ring"
            disabled={!canApply || applyPending}
          >
            {domainOptions.map((domain) => <option key={domain.id} value={domain.id}>@{domain.name}</option>)}
            {domainOptions.length === 0 && <option value="">暂无域名</option>}
          </select>
          <Button className="h-[42px] px-0" disabled={!canApply || applyPending || !selectedDomain || !localPart.trim()}>{applyPending ? "创建中" : "创建"}</Button>
        </form>
        <div className="mt-3 flex flex-wrap items-center gap-x-2 gap-y-1 text-sm text-muted-foreground">
          <span>{canApply
            ? "提示：邮箱数量受账号配额限制，管理员可在后台为单个账号调整可创建数量。"
            : canConfigureApply && applyOptions?.enabled
              ? "提示：尚未选择开放域名。请在“后台管理 → 系统设置 → 邮件”中至少勾选一个已启用域名。"
              : canConfigureApply
                ? "提示：账号自助申请邮箱未开启。请在“后台管理 → 系统设置 → 邮件”中开启，并勾选开放域名。"
                : "提示：当前账号暂不可创建新邮箱，请联系管理员开启账号自助申请邮箱。"}</span>
          {!canApply && canConfigureApply && <Button type="button" variant="link" className="h-auto p-0 text-sm" onClick={onConfigureApply}>前往设置</Button>}
        </div>
      </section>}

      <section className="rounded-lg border bg-card">
        <div className="flex flex-col items-stretch gap-3 px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:px-5">
          <h2 className="text-lg font-semibold leading-7">我的邮箱 ({mailboxes.length})</h2>
          <div className="relative w-full sm:w-64 sm:shrink-0">
            <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input value={mailboxSearch} onChange={(event) => setMailboxSearch(event.target.value)} className="h-[34px] pl-9 text-sm shadow-none" placeholder="搜索邮箱地址..." />
          </div>
        </div>
        <div className="divide-y">
          {pagedMailboxes.map((mailbox) => {
            const forwardTargets = mailboxForwards[mailbox.id] || []
            const mailboxBindings = visiblePendingBindings.filter((item) => item.scope === "mailbox" && item.mailboxId === mailbox.id)
            const waitingBinding = mailboxBindings.find((item) => item.status === forwardingBindingPendingStatus)
            const failedBinding = mailboxBindings.find((item) => item.status === "activation_failed")
            const expiredBinding = mailboxBindings.find((item) => item.status === "expired")
            const status = failedBinding ? "启用失败" : waitingBinding ? "等待对方确认" : expiredBinding ? "验证已过期" : mergeForwardingTargets(accountForwardTargets, forwardTargets).length > 0 ? "转发中" : "未设置"
            return (
              <div key={mailbox.id} className={cn("grid gap-3 px-4 py-3 sm:px-5 md:grid-cols-[minmax(0,1fr)_auto] md:items-center", selectedMailboxId === mailbox.id && "bg-muted/50")}>
                <div className="flex min-w-0 items-center gap-4">
                  <div className="flex size-10 shrink-0 items-center justify-center rounded-full bg-slate-950 text-white">
                    <Mail className="h-5 w-5" />
                  </div>
                  <div className="min-w-0">
                    <div className="flex min-w-0 items-center gap-2">
                      <Button type="button" variant="ghost" size="sm" className="h-auto min-w-0 max-w-full truncate justify-start p-0 text-left text-sm font-semibold hover:bg-transparent hover:underline" onClick={() => { onSelect(mailbox.id); onOpen(mailbox.id) }}>{mailbox.address}</Button>
                      {selectedMailboxId === mailbox.id && <Badge variant="secondary" className="h-5 rounded-md px-1.5 text-[10px]">当前</Badge>}
                    </div>
                    <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
                      <span>创建于 {formatDateTime(mailbox.createdAt)}</span>
                      <span>账号级 {accountForwardTargets.length}</span>
                      <span>单邮箱 {forwardTargets.length}</span>
                      {waitingBinding && <span>等待 {waitingBinding.email} 确认</span>}
                    </div>
                  </div>
                </div>
                <div className="flex shrink-0 flex-wrap items-center gap-2">
                  <Badge variant={status === "转发中" ? "default" : status === "启用失败" || status === "验证已过期" ? "destructive" : "outline"}>{status}</Badge>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="h-[34px] w-full gap-1 px-3 md:w-[104px]"
                    onClick={() => { onSelect(mailbox.id); openMailboxForward(mailbox) }}
                  >
                    <SendHorizontal className="h-3.5 w-3.5" />
                    管理转发
                  </Button>
                </div>
              </div>
            )
          })}
          {filteredMailboxes.length === 0 && <div className="px-6 py-10 text-center text-sm text-muted-foreground">{mailboxes.length === 0 ? "暂无邮箱，请创建一个新邮箱" : "没有匹配邮箱"}</div>}
        </div>
        <PaginationControls page={mailboxPage} pageSize={mailboxPageSize} total={filteredMailboxes.length} onPageChange={setMailboxPage} onPageSizeChange={setMailboxPageSize} />
      </section>
      </>}

      {mailboxView === "forwarding" && <section id="mailbox-forwarding-section" className="rounded-lg border bg-card px-4 py-4 sm:px-5">
        <div className="mb-4 flex items-center gap-4">
          <h2 className="text-lg font-semibold leading-7">邮件转发</h2>
        </div>
        <div className="rounded-xl bg-muted/20 px-4 py-5 sm:px-5">
          <div className="mb-3 text-sm font-medium">账号级转发</div>
          <div className="mb-4 text-sm text-muted-foreground">对所有邮箱生效，可同时转发到多个已验证邮箱；单个邮箱可继续追加自己的转发目标</div>
          <div className="space-y-3">
            <div className="space-y-2">{accountForwardTargets.map((email) => <div key={email} className="mailbox-forwarding-target"><div><strong>{email}</strong><small>已验证 · 应用于 {mailboxes.length} 个邮箱</small></div><Button type="button" variant="ghost" size="sm" onClick={() => setAccountForwardTargets((items) => items.filter((item) => item !== email))}>移除</Button></div>)}{accountForwardTargets.length === 0 && <p className="text-sm text-muted-foreground">尚未设置账号级转发目标。</p>}</div>
            {visiblePendingBindings.filter((item) => item.scope === "account" && item.status !== "active").map((binding) => <div key={binding.id} className="mailbox-forwarding-target is-pending"><div><strong>{binding.email}</strong><small>{binding.status === "activation_failed" ? "启用失败" : binding.status === "expired" ? "验证已过期" : "等待对方确认 · 确认后自动设为账号级目标"}</small></div><div className="flex gap-1">{binding.status === "activation_failed" && <Button type="button" variant="ghost" size="sm" onClick={() => retryPendingBinding.mutate(binding.id)}>重试启用</Button>}<Button type="button" variant="ghost" size="sm" onClick={() => cancelPendingBinding.mutate(binding.id)}>取消</Button></div></div>)}
            <div className="relative"><Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" /><Input value={accountTargetQuery} onChange={(event) => setAccountTargetQuery(event.target.value)} className="h-10 pl-9" placeholder="输入或搜索账号级转发邮箱" /></div>
            {verifiedEmails.filter((email) => email.toLowerCase().includes(accountTargetQuery.trim().toLowerCase()) && !accountForwardTargets.includes(email)).slice(0, 5).map((email) => <Button key={email} type="button" variant="ghost" className="mailbox-forwarding-search-result" onClick={() => { setAccountForwardTargets((items) => mergeForwardingTargets(items, [email])); setAccountTargetQuery("") }}><span><strong>{email}</strong><small>已验证</small></span><Plus className="h-4 w-4" /></Button>)}
            {looksLikeEmailAddress(accountTargetQuery) && !verifiedEmails.some((email) => email.toLowerCase() === accountTargetQuery.trim().toLowerCase()) && <div className="flex items-center justify-between gap-3 rounded-lg border bg-muted/20 p-3"><div className="min-w-0"><strong className="block truncate text-sm">验证并添加 {accountTargetQuery.trim().toLowerCase()}</strong><small className="text-muted-foreground">确认后自动加入账号级转发</small></div><Button type="button" size="sm" disabled={createPendingBinding.isPending} onClick={() => createPendingBinding.mutate({ email: accountTargetQuery.trim().toLowerCase(), scope: "account" })}>发送验证</Button></div>}
            <div className="flex justify-end"><Button type="button" className="h-10" disabled={forwarding.isLoading || saveAccountForwarding.isPending || !accountForwardingChanged} onClick={() => saveAccountForwarding.mutate(accountForwardTargets)}>{saveAccountForwarding.isPending ? "保存中" : "保存更改"}</Button></div>
          </div>
        </div>
        <Button type="button" variant="outline" className="mt-4 h-auto min-h-14 w-full justify-start gap-3 px-4 py-3 text-left font-normal shadow-none" onClick={() => setMailboxView("verified")}>
          <span className="flex size-9 shrink-0 items-center justify-center rounded-md bg-emerald-50 text-emerald-700"><MailCheck className="h-4 w-4" /></span>
          <span className="min-w-0 flex-1">
            <span className="block text-sm font-medium">管理验证邮箱</span>
            <span className="block truncate text-sm text-muted-foreground">已验证 {completedVerifiedEmailItems.length} 个{pendingVerifiedEmailItems.length > 0 ? `，待验证 ${pendingVerifiedEmailItems.length} 个` : ""}</span>
          </span>
          <ChevronDown className="h-4 w-4 shrink-0 -rotate-90 text-muted-foreground" />
        </Button>
        <p className="mt-3 text-sm text-muted-foreground">提示：点击邮箱列表中的「管理转发」，可在账号级目标之外追加该邮箱自己的转发目标。</p>

        <div className="mt-5 border-t pt-4">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <h3 className="text-lg font-semibold">单邮箱转发</h3>
              <p className="mt-1 text-sm text-muted-foreground">仅显示设置了单独目标的邮箱，账号级目标仍会自动叠加。</p>
            </div>
            <div className="relative w-full sm:w-64">
              <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
              <Input value={singleForwardSearch} onChange={(event) => setSingleForwardSearch(event.target.value)} className="h-10 pl-9 text-sm shadow-none" placeholder="搜索单邮箱规则" aria-label="搜索单邮箱转发规则" />
            </div>
          </div>
          <div className="mt-4 divide-y overflow-hidden rounded-lg border">
            {pagedSingleForwardMailboxes.map((mailbox) => (
              <div key={mailbox.id} className="grid gap-3 px-4 py-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center">
                <div className="flex min-w-0 items-center gap-3">
                  <span className="flex size-10 shrink-0 items-center justify-center rounded-full bg-slate-950 text-white"><Mail className="h-5 w-5" /></span>
                  <div className="min-w-0">
                    <div className="break-all text-sm font-semibold">{mailbox.address}</div>
                    <div className="mt-1 text-xs text-muted-foreground">单独目标 {(mailboxForwards[mailbox.id] || []).length} 个{accountForwardTargets.length > 0 ? ` · 继承账号级 ${accountForwardTargets.length} 个` : ""}</div>
                  </div>
                </div>
                <Button type="button" variant="outline" className="h-11 w-full sm:h-9 sm:w-20" onClick={() => openMailboxForward(mailbox)}>管理</Button>
              </div>
            ))}
            {singleForwardMailboxes.length === 0 && (
              <div className="px-5 py-10 text-center text-sm text-muted-foreground">
                {singleForwardSearch ? "没有匹配的单邮箱转发规则" : "暂无单邮箱转发规则，请在「我的邮箱」中选择邮箱进行设置。"}
              </div>
            )}
          </div>
          {singleForwardMailboxes.length > 10 && <PaginationControls page={singleForwardPage} pageSize={10} total={singleForwardMailboxes.length} onPageChange={setSingleForwardPage} />}
        </div>
      </section>}

      {mailboxView === "mine" && externalImapEnabled && (
        <section className="rounded-lg border bg-card">
          <div className="flex flex-wrap items-center justify-between gap-3 px-6 py-4">
            <div>
              <h2 className="text-lg font-semibold leading-7">外部邮箱</h2>
              <p className="text-sm text-muted-foreground">{selectedMailbox ? `同步到 ${selectedMailbox.address}` : "请先选择邮箱"}</p>
            </div>
            <div className="flex flex-wrap gap-2">
              <ExternalImapOAuthDialog provider="gmail" selectedMailbox={selectedMailbox} disabled={!selectedMailbox} pending={externalPending} onStart={onStartExternalOAuth} />
              <ExternalImapOAuthDialog provider="outlook" selectedMailbox={selectedMailbox} disabled={!selectedMailbox} pending={externalPending} onStart={onStartExternalOAuth} />
              <ExternalImapDialog mailboxId={selectedMailbox?.id || ""} disabled={!selectedMailbox || externalPending} pending={externalPending} onSubmit={onCreateExternal} />
            </div>
          </div>
          <div className="divide-y border-t">
            {externalAccounts.map((account) => {
              const expanded = selectedExternalRunAccountId === account.id
              return (
                <div key={account.id} className="px-6 py-4">
                  <div className="grid gap-4 md:grid-cols-[minmax(0,1fr)_auto] md:items-center">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="truncate font-semibold">{account.name || account.username}</span>
                        <Badge variant={account.enabled ? "secondary" : "outline"}>{account.enabled ? "已启用" : "已停用"}</Badge>
                        <Badge variant="outline">{account.authMode === "oauth2" ? externalOAuthProviderLabel(account.oauthProvider) : "IMAP"}</Badge>
                        <Badge variant={account.lastStatus === "error" ? "destructive" : "outline"}>{externalStatusLabel(account.lastStatus)}</Badge>
                      </div>
                      <div className="mt-1 truncate text-sm text-muted-foreground">{account.username} · {account.host}:{account.port} · {account.storageMode === "local" ? "同步到本地" : "远端直连"}</div>
                      {account.lastError && <div className="mt-1 truncate text-xs text-destructive">{account.lastError}</div>}
                    </div>
                    <div className="flex flex-wrap gap-2">
                      {account.authMode !== "oauth2" && <ExternalImapDialog account={account} mailboxId={account.mailboxId} disabled={externalPending} pending={externalPending} onSubmit={(payload) => onUpdateExternal(account.id, payload)} />}
                      <Button type="button" variant="outline" size="sm" disabled={externalPending} onClick={() => onTestExternal(account.id)}>测试</Button>
                      <Button type="button" variant="outline" size="sm" disabled={externalPending || !account.enabled} onClick={() => onSyncExternal(account.id)}><RefreshCcw className="h-4 w-4" />同步</Button>
                      <Button type="button" variant="outline" size="sm" disabled={externalPending} onClick={() => onSelectExternalRunAccount(expanded ? "" : account.id)}>{expanded ? "收起" : "详情"}</Button>
                      <Button type="button" variant="ghost" size="icon" className="size-8 text-destructive hover:text-destructive" disabled={externalPending} onClick={() => setPendingExternalDelete(account)} aria-label={`删除 ${account.name || account.username}`}><Trash2 className="h-4 w-4" /></Button>
                    </div>
                  </div>
                  {expanded && <div className="mt-4"><ExternalImapSyncPanel account={account} folders={externalRunFolders} runs={externalSyncRuns} pending={externalPending} onSyncFolder={onSyncExternalFolder} /></div>}
                </div>
              )
            })}
            {externalAccounts.length === 0 && <div className="px-6 py-10 text-center text-sm text-muted-foreground">当前邮箱尚未添加外部账号</div>}
          </div>
        </section>
      )}

      {mailboxView === "verified" && (
        <section className="rounded-lg border bg-card">
          <div className="flex flex-col gap-3 border-b px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:px-5">
            <div>
              <h2 className="text-lg font-semibold">验证邮箱管理</h2>
              <p className="mt-1 text-sm text-muted-foreground">待验证任务优先显示，完成后自动归入已验证列表。</p>
            </div>
            <div className="flex w-full flex-col gap-2 sm:w-auto sm:flex-row">
              <div className="relative w-full sm:w-64">
                <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                <Input value={verifiedSearch} onChange={(event) => setVerifiedSearch(event.target.value)} className="h-11 pl-9 text-base shadow-none sm:h-10 sm:text-sm" placeholder="搜索或输入邮箱地址" aria-label="搜索或输入验证邮箱" inputMode="email" spellCheck={false} />
              </div>
              <Button type="button" className="h-11 sm:h-10" onClick={openAddVerifiedEmail}>添加验证邮箱</Button>
            </div>
          </div>

          <div className="space-y-5 px-4 py-4 sm:px-5">
            {pendingVerifiedEmailItems.length > 0 && (
              <div>
                <div className="mb-3 flex items-center gap-2">
                  <span className="h-5 w-1 rounded-full bg-amber-500" />
                  <h3 className="font-semibold">待验证邮箱</h3>
                  <Badge variant="outline">{pendingVerifiedEmailItems.length}</Badge>
                </div>
                <div className="overflow-hidden rounded-lg border">
                  {matchingPendingVerifiedEmailItems.map((item) => (
                    <VerifiedEmailRow key={item.id} item={item} detail={pendingEmailPurpose(item)} busy={resendVerifiedEmail.isPending || deleteVerifiedEmail.isPending} onResend={resendVerification} onRemove={() => setPendingVerifiedDelete(item)} />
                  ))}
                  {matchingPendingVerifiedEmailItems.length === 0 && <div className="px-4 py-10 text-center text-sm text-muted-foreground">待验证邮箱中没有匹配地址</div>}
                </div>
              </div>
            )}

            {verifiedEmailItems.length > 0 ? <div>
              <div className="mb-3 flex items-center gap-2">
                <span className="h-5 w-1 rounded-full bg-emerald-500" />
                <h3 className="font-semibold">已验证邮箱</h3>
                <Badge variant="outline">{completedVerifiedEmailItems.length}</Badge>
              </div>
              <div className="overflow-hidden rounded-lg border">
                {pagedVerifiedEmailItems.map((item) => (
                  <VerifiedEmailRow key={item.id} item={item} detail={verifiedEmailUsageCount(item.email) ? `已绑定 ${verifiedEmailUsageCount(item.email)} 条转发规则` : "已验证，尚未绑定"} busy={deleteVerifiedEmail.isPending} onResend={resendVerification} onRemove={() => setPendingVerifiedDelete(item)} />
                ))}
                {pagedVerifiedEmailItems.length === 0 && (
                  <div className="px-4 py-10 text-center text-sm text-muted-foreground">
                    {normalizedVerifiedSearch ? "已验证邮箱中没有匹配地址" : "暂无已验证邮箱"}
                  </div>
                )}
              </div>
              <PaginationControls page={verifiedPage} pageSize={verifiedPageSize} total={matchingCompletedVerifiedEmailItems.length} onPageChange={setVerifiedPage} onPageSizeChange={setVerifiedPageSize} />
            </div> : (
              <div className="rounded-lg border border-dashed px-5 py-10 text-center">
                <MailCheck className="mx-auto h-7 w-7 text-muted-foreground" />
                <p className="mt-3 text-sm font-medium">还没有验证邮箱</p>
                <p className="mt-1 text-sm text-muted-foreground">添加并验证后，才能作为邮件转发目标。</p>
              </div>
            )}
          </div>
        </section>
      )}

      <Sheet open={!!forwardingMailbox} onOpenChange={(open) => { if (!open) { setForwardingMailbox(null); setForwardingVerificationCandidate(""); setForwardingTargetQuery("") } }}>
          <SheetContent side="right" className="mailbox-forwarding-sheet flex w-[min(440px,100vw)] max-w-none flex-col gap-0 p-0 sm:max-w-[440px]">
          <SheetHeader className="border-b px-5 pb-4 pt-5 text-left">
            <SheetTitle>管理转发</SheetTitle>
            <p className="break-all pr-8 text-sm font-medium">{forwardingMailbox?.address}</p>
          </SheetHeader>
          <div className="min-h-0 flex-1 space-y-5 overflow-y-auto px-5 py-5">
            <section className="mailbox-forwarding-section">
              <div className="flex items-start justify-between gap-3"><div><h3 className="text-sm font-semibold">继承的账号级转发</h3><p className="mt-1 text-xs leading-5 text-muted-foreground">账号级目标由全局转发设置控制。</p></div><KeyRound className="mt-0.5 h-4 w-4 text-muted-foreground" /></div>
              <div className="mt-3 space-y-2">{accountForwardTargets.length ? accountForwardTargets.map((email) => <div key={email} className="mailbox-forwarding-target"><div><strong>{email}</strong><small>账号级 · 已启用</small></div><Badge variant="outline">只读</Badge></div>) : <p className="text-sm text-muted-foreground">当前没有账号级转发目标。</p>}</div>
              <Button type="button" variant="link" className="mt-2 h-auto p-0 text-sm" onClick={() => { setForwardingMailbox(null); setMailboxView("forwarding") }}>前往全局设置</Button>
            </section>

            <section className="mailbox-forwarding-section">
              <div><h3 className="text-sm font-semibold">单独转发目标</h3><p className="mt-1 text-xs leading-5 text-muted-foreground">只应用于当前会员邮箱。</p></div>
              <div className="mt-3 space-y-2">
                {forwardDraft.map((email) => <div key={email} className="mailbox-forwarding-target"><div><strong>{email}</strong><small>已启用</small></div><Button type="button" variant="ghost" size="sm" disabled={forwardingBusy} onClick={() => setForwardDraft((items) => items.filter((item) => item !== email))}>移除</Button></div>)}
                {visiblePendingBindings.filter((item) => item.scope === "mailbox" && item.mailboxId === forwardingMailbox?.id && item.status !== "active").map((binding) => {
                  const verification = verifiedEmailItems.find((item) => item.id === binding.verifiedEmailId)
                  return <div key={binding.id} className="mailbox-forwarding-target is-pending"><div><strong>{binding.email}</strong><small>{binding.status === "activation_failed" ? `启用失败${binding.failureReason ? `：${binding.failureReason}` : ""}` : binding.status === "expired" ? "验证已过期" : `等待对方确认 · 确认后自动启用`}</small></div><div className="flex gap-1">{binding.status === "activation_failed" ? <Button type="button" variant="ghost" size="sm" onClick={() => retryPendingBinding.mutate(binding.id)}>重试启用</Button> : verification && <Button type="button" variant="ghost" size="sm" onClick={() => resendVerification(verification)}>重发</Button>}<Button type="button" variant="ghost" size="sm" onClick={() => cancelPendingBinding.mutate(binding.id)}>取消</Button></div></div>
                })}
                {forwardDraft.length === 0 && !visiblePendingBindings.some((item) => item.scope === "mailbox" && item.mailboxId === forwardingMailbox?.id && item.status !== "active") && <p className="text-sm text-muted-foreground">尚未设置单邮箱转发目标。</p>}
              </div>
            </section>

            <section className="mailbox-forwarding-section">
              <div><h3 className="text-sm font-semibold">添加转发目标</h3><p className="mt-1 text-xs leading-5 text-muted-foreground">搜索已验证邮箱，或直接输入新的车友邮箱。</p></div>
              {forwardingVerificationCandidate ? <div className="mt-3 space-y-4 rounded-lg border bg-muted/25 p-4"><dl className="space-y-3 text-sm"><div><dt className="text-muted-foreground">车友邮箱</dt><dd className="mt-1 break-all font-medium">{forwardingVerificationCandidate}</dd></div><div><dt className="text-muted-foreground">绑定会员邮箱</dt><dd className="mt-1 break-all font-medium">{forwardingMailbox?.address}</dd></div></dl><p className="text-sm leading-6 text-muted-foreground">系统将发送确认邮件。对方确认后，此转发目标将自动启用。</p></div> : <div className="mt-3 space-y-2"><div className="relative"><Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" /><Input value={forwardingTargetQuery} onChange={(event) => setForwardingTargetQuery(event.target.value)} className="h-10 pl-9" placeholder="输入或搜索转发邮箱" /></div>{verifiedEmails.filter((email) => email.toLowerCase().includes(forwardingTargetQuery.trim().toLowerCase()) && !forwardDraft.includes(email)).slice(0, 5).map((email) => <Button key={email} type="button" variant="ghost" className="mailbox-forwarding-search-result" onClick={() => { setForwardDraft((items) => mergeForwardingTargets(items, [email])); setForwardingTargetQuery("") }}><span><strong>{email}</strong><small>已验证</small></span><Plus className="h-4 w-4" /></Button>)}{looksLikeEmailAddress(forwardingTargetQuery) && !verifiedEmails.some((email) => email.toLowerCase() === forwardingTargetQuery.trim().toLowerCase()) && <Button type="button" variant="ghost" className="mailbox-forwarding-search-result" onClick={() => setForwardingVerificationCandidate(forwardingTargetQuery.trim().toLowerCase())}><span><strong>验证并添加 {forwardingTargetQuery.trim().toLowerCase()}</strong><small>确认后自动启用转发</small></span><SendHorizontal className="h-4 w-4" /></Button>}</div>}
            </section>
          </div>
          <SheetFooter className="grid grid-cols-2 gap-2 border-t px-5 py-4 pb-[calc(1rem+env(safe-area-inset-bottom))]">
            {forwardingVerificationCandidate ? <><Button type="button" variant="outline" className="h-10" disabled={createPendingBinding.isPending} onClick={() => setForwardingVerificationCandidate("")}>返回</Button><Button type="button" className="h-10" disabled={!forwardingMailbox || createPendingBinding.isPending} onClick={() => forwardingMailbox && createPendingBinding.mutate({ email: forwardingVerificationCandidate, scope: "mailbox", mailboxId: forwardingMailbox.id })}>{createPendingBinding.isPending ? "发送中" : "发送验证邮件"}</Button></> : <><Button type="button" variant="outline" className="h-10" disabled={forwardingBusy} onClick={() => setForwardingMailbox(null)}>取消</Button><Button type="button" className="h-10" disabled={forwardingBusy} onClick={saveMailboxForward}>{saveMailboxForwarding.isPending ? "保存中" : "保存更改"}</Button></>}
          </SheetFooter>
        </SheetContent>
      </Sheet>

      <Dialog open={!!forwardingTargetPreview} onOpenChange={(open) => { if (!open) setForwardingTargetPreview(null) }}>
        <DialogContent className="forwarding-dialog w-[calc(100vw-1.5rem)] max-w-none sm:max-w-md">
          <DialogHeader><DialogTitle>转发目标</DialogTitle></DialogHeader>
          {forwardingTargetPreview && (
            <div className="space-y-4">
              <div className="min-w-0">
                <div className="truncate text-sm text-muted-foreground">{forwardingTargetPreview.title}</div>
                <div className="mt-2 flex items-center gap-2">
                  <Badge variant="secondary">{forwardingTargetPreview.source}</Badge>
                  <span className="text-sm text-muted-foreground">{forwardingTargetPreview.subtitle}</span>
                </div>
              </div>
              <div className="max-h-[320px] overflow-y-auto rounded-lg border">
                {forwardingTargetPreview.targets.map((email) => (
                  <div key={email} className="flex min-h-11 items-center gap-3 border-b px-3 py-2 text-sm last:border-b-0">
                    <span className="size-2 shrink-0 rounded-full bg-emerald-500" />
                    <span className="min-w-0 break-all font-medium">{email}</span>
                  </div>
                ))}
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>

      <Sheet open={mobileCreateOpen} onOpenChange={setMobileCreateOpen}>
        <SheetContent side="bottom" className="mailbox-sheet-content max-h-[85dvh] overflow-y-auto rounded-t-xl pb-[calc(1rem+env(safe-area-inset-bottom))]">
          <SheetHeader className="text-left"><SheetTitle>创建新邮箱</SheetTitle></SheetHeader>
          <MailboxCreateForm
            localPart={localPart}
            onLocalPartChange={setLocalPart}
            selectedDomainId={selectedDomain?.id || ""}
            domainOptions={domainOptions}
            onDomainChange={setDomainId}
            canApply={canApply}
            pending={applyPending}
            onSubmit={submitMailbox}
          />
        </SheetContent>
      </Sheet>

      {isMobile ? (
        <Sheet open={addVerifiedOpen} onOpenChange={(open) => { setAddVerifiedOpen(open); if (!open) setVerifiedEmailDraft("") }}>
          <SheetContent side="bottom" className="mailbox-sheet-content max-h-[85dvh] overflow-y-auto rounded-t-xl pb-[calc(1rem+env(safe-area-inset-bottom))]">
            <SheetHeader className="text-left"><SheetTitle>添加验证邮箱</SheetTitle></SheetHeader>
            <VerifiedEmailForm value={verifiedEmailDraft} onChange={setVerifiedEmailDraft} exists={verifiedEmailDraftExists} pending={addVerifiedEmail.isPending} onSubmit={submitVerifiedEmail} />
          </SheetContent>
        </Sheet>
      ) : (
        <Dialog open={addVerifiedOpen} onOpenChange={(open) => { setAddVerifiedOpen(open); if (!open) setVerifiedEmailDraft("") }}>
          <DialogContent className="verified-email-dialog sm:max-w-md">
            <DialogHeader><DialogTitle>添加验证邮箱</DialogTitle></DialogHeader>
            <VerifiedEmailForm value={verifiedEmailDraft} onChange={setVerifiedEmailDraft} exists={verifiedEmailDraftExists} pending={addVerifiedEmail.isPending} onSubmit={submitVerifiedEmail} />
          </DialogContent>
        </Dialog>
      )}

      <ConfirmDialog
        open={!!pendingVerifiedDelete}
        title="删除验证邮箱？"
        description={pendingVerifiedDelete ? `删除 ${pendingVerifiedDelete.email} 后，它将不能继续作为转发目标。` : undefined}
        confirmText="删除"
        destructive
        pending={deleteVerifiedEmail.isPending}
        onOpenChange={(open) => { if (!open) setPendingVerifiedDelete(null) }}
        onConfirm={() => {
          if (!pendingVerifiedDelete) return
          removeVerifiedEmail(pendingVerifiedDelete.id, pendingVerifiedDelete.email)
        }}
      />

      <ConfirmDialog
        open={!!pendingExternalDelete}
        title="删除外部邮箱？"
        description={pendingExternalDelete ? `${pendingExternalDelete.name || pendingExternalDelete.username} 的连接配置和同步记录将被删除。` : undefined}
        confirmText="删除"
        destructive
        pending={externalPending}
        onOpenChange={(open) => { if (!open) setPendingExternalDelete(null) }}
        onConfirm={() => {
          if (!pendingExternalDelete) return
          onDeleteExternal(pendingExternalDelete.id)
          setPendingExternalDelete(null)
        }}
      />
    </div>
  )
}

function MailboxViewTabs({ value, onChange, pendingCount }: { value: MailboxView; onChange: (view: MailboxView) => void; pendingCount: number }) {
  const items: { key: MailboxView; label: string }[] = [
    { key: "mine", label: "我的邮箱" },
    { key: "forwarding", label: "邮件转发" },
    { key: "verified", label: "验证邮箱" },
  ]
  return (
    <div className="grid grid-cols-3 px-2 sm:px-4" role="tablist" aria-label="邮箱管理视图">
      {items.map((item) => (
        <Button
          key={item.key}
          type="button"
          variant="ghost"
          role="tab"
          aria-selected={value === item.key}
          className={cn(
            "relative flex min-h-12 rounded-none px-1 text-sm font-medium text-muted-foreground transition-colors hover:bg-transparent hover:text-foreground sm:px-5",
            value === item.key && "text-primary",
          )}
          onClick={() => onChange(item.key)}
        >
          <span>{item.label}</span>
          {item.key === "verified" && pendingCount > 0 && <span className="min-w-5 rounded-full bg-amber-500 px-1.5 py-0.5 text-[10px] font-semibold leading-4 text-white">{pendingCount}</span>}
          {value === item.key && <span className="absolute inset-x-3 bottom-0 h-0.5 rounded-full bg-primary sm:inset-x-8" />}
        </Button>
      ))}
    </div>
  )
}

function MailboxStatCard({ icon, label, value, tone, compact }: { icon: React.ReactNode; label: string; value: string | number; tone: "primary" | "success" | "warning" | "violet"; compact?: boolean }) {
  const toneClass = {
    primary: "bg-primary/10 text-primary",
    success: "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
    warning: "bg-amber-500/10 text-amber-700 dark:text-amber-300",
    violet: "bg-violet-500/10 text-violet-700 dark:text-violet-300",
  }[tone]
  return (
    <div className="flex min-w-0 items-center gap-2 rounded-lg border bg-background/55 px-2 py-3 sm:gap-3 sm:px-4">
      <span className={cn("hidden size-10 shrink-0 items-center justify-center rounded-full sm:flex", toneClass)}>{icon}</span>
      <span className="min-w-0">
        <span className="block text-[11px] leading-4 text-muted-foreground sm:text-xs">{label}</span>
        <span className={cn("mt-0.5 block truncate font-semibold tabular-nums", compact ? "text-sm sm:text-base" : "text-lg sm:text-xl")}>{value}</span>
      </span>
    </div>
  )
}

function PaginationControls({ page, pageSize, total, onPageChange, onPageSizeChange }: { page: number; pageSize: number; total: number; onPageChange: (page: number) => void; onPageSizeChange?: (pageSize: number) => void }) {
  if (total === 0) return null
  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  const firstItem = (page - 1) * pageSize + 1
  const lastItem = Math.min(total, page * pageSize)
  return (
    <div className="flex flex-col gap-3 border-t px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:px-6">
      <div className="flex items-center justify-between gap-3 text-xs text-muted-foreground sm:justify-start">
        <span>{firstItem}-{lastItem} / {total}</span>
        {onPageSizeChange && (
          <div className="flex items-center gap-2">
            <span>每页</span>
            <Select value={String(pageSize)} onValueChange={(value) => onPageSizeChange(Number(value))}>
              <SelectTrigger className="h-9 w-[68px] text-xs" aria-label="每页邮箱数量"><SelectValue /></SelectTrigger>
              <SelectContent>{[10, 20, 50].map((size) => <SelectItem key={size} value={String(size)}>{size}</SelectItem>)}</SelectContent>
            </Select>
          </div>
        )}
      </div>
      <div className="grid grid-cols-2 gap-2 sm:flex">
        <Button type="button" variant="outline" size="sm" className="h-11 sm:h-9" disabled={page <= 1} onClick={() => onPageChange(page - 1)}>上一页</Button>
        <Button type="button" variant="outline" size="sm" className="h-11 sm:h-9" disabled={page >= totalPages} onClick={() => onPageChange(page + 1)}>下一页</Button>
      </div>
    </div>
  )
}

function MailboxCreateForm({ localPart, onLocalPartChange, selectedDomainId, domainOptions, onDomainChange, canApply, pending, onSubmit }: {
  localPart: string
  onLocalPartChange: (value: string) => void
  selectedDomainId: string
  domainOptions: MailboxApplyOptions["domains"]
  onDomainChange: (value: string) => void
  canApply: boolean
  pending: boolean
  onSubmit: (event: React.FormEvent<HTMLFormElement>) => void
}) {
  return (
    <form className="mt-5 space-y-4" onSubmit={onSubmit}>
      <div className="space-y-2">
        <Label htmlFor="mobile-mailbox-prefix">邮箱地址前缀</Label>
        <Input id="mobile-mailbox-prefix" value={localPart} onChange={(event) => onLocalPartChange(event.target.value)} className="h-11 text-base" placeholder="例如 netflix01" disabled={!canApply || pending} />
      </div>
      <div className="space-y-2">
        <Label>邮箱域名</Label>
        <Select value={selectedDomainId} onValueChange={onDomainChange} disabled={!canApply || pending || domainOptions.length === 0}>
          <SelectTrigger className="h-11 text-base" aria-label="邮箱域名"><SelectValue placeholder="暂无域名" /></SelectTrigger>
          <SelectContent>{domainOptions.map((domain) => <SelectItem key={domain.id} value={domain.id}>@{domain.name}</SelectItem>)}</SelectContent>
        </Select>
      </div>
      <p className="text-sm leading-5 text-muted-foreground">邮箱数量受账号配额限制，创建成功后会自动刷新列表。</p>
      <Button className="h-11 w-full" disabled={!canApply || pending || !selectedDomainId || !localPart.trim()}>{pending ? "创建中" : "创建邮箱"}</Button>
    </form>
  )
}

function VerifiedEmailForm({ value, onChange, exists, pending, onSubmit }: { value: string; onChange: (value: string) => void; exists: boolean; pending: boolean; onSubmit: (event: React.FormEvent<HTMLFormElement>) => void }) {
  return (
    <form className="mt-4 space-y-4" onSubmit={onSubmit}>
      <div className="space-y-2">
        <Label htmlFor="verified-email-address">外部邮箱地址</Label>
        <Input id="verified-email-address" name="forwarding-verification-target" type="email" value={value} onChange={(event) => onChange(event.target.value)} className="h-11 text-base" placeholder="name@example.com" autoComplete="off" inputMode="email" spellCheck={false} disabled={pending} autoFocus={!value} />
      </div>
      <p className="text-sm leading-5 text-muted-foreground">系统会发送一封验证邮件，完成验证后才能作为转发目标。</p>
      <Button className="h-11 w-full" disabled={pending || !value.trim() || exists}>{pending ? "发送中" : exists ? "该邮箱已添加" : "发送验证邮件"}</Button>
    </form>
  )
}

function looksLikeEmailAddress(value: string) {
  return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value.trim())
}

function forwardingItemTime(item: ForwardingVerifiedEmail, verified: boolean) {
  const value = verified ? item.verifiedAt || item.createdAt : item.verificationSentAt || item.createdAt
  const time = new Date(value).getTime()
  return Number.isNaN(time) ? 0 : time
}

function ExternalImapOAuthDialog({ provider, selectedMailbox, disabled, pending, onStart }: { provider: ExternalImapOAuthProvider; selectedMailbox?: Mailbox; disabled?: boolean; pending: boolean; onStart: (provider: ExternalImapOAuthProvider, payload: { mailboxId: string; email: string; storageMode: ExternalImapStorageMode }) => void }) {
  const [open, setOpen] = React.useState(false)
  const [storageMode, setStorageMode] = React.useState<ExternalImapStorageMode>("local")
  const label = provider === "gmail" ? "Gmail OAuth" : "Microsoft 365 / Outlook OAuth"

  function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!selectedMailbox) return
    const form = new FormData(event.currentTarget)
    onStart(provider, {
      mailboxId: selectedMailbox.id,
      email: String(form.get("email") || ""),
      storageMode,
    })
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <Button type="button" variant="outline" disabled={disabled || pending} onClick={() => setOpen(true)}>{label}</Button>
      <DialogContent className="max-h-[92dvh] overflow-y-auto sm:max-w-lg">
        <DialogHeader><DialogTitle>{label}</DialogTitle></DialogHeader>
        <form className="space-y-4" onSubmit={submit}>
          <div className="rounded-lg border bg-muted/30 p-3 text-sm text-muted-foreground">
            OAuth 只适用于 {provider === "gmail" ? "Google Gmail" : "Microsoft 365 / Outlook / Exchange Online"} 托管邮箱。自建域名邮箱请使用“添加外部邮箱”的普通 IMAP 方式。
          </div>
          <Field label="外部邮箱地址（可选）"><Input name="email" type="email" placeholder={selectedMailbox?.address || "name@example.com"} /></Field>
          <div className="text-xs text-muted-foreground">留空时会以 OAuth 服务商返回的真实授权邮箱为准；填写后，回调时会校验它和真实授权邮箱一致。</div>
          <Field label="存储模式">
            <Select value={storageMode} onValueChange={(value) => setStorageMode(value as ExternalImapStorageMode)}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent><SelectItem value="local">同步到本地</SelectItem><SelectItem value="remote">远端直连</SelectItem></SelectContent>
            </Select>
          </Field>
          <DialogFooter className="gap-2 [&>button]:w-full sm:[&>button]:w-auto">
            <Button type="button" variant="outline" onClick={() => setOpen(false)}>取消</Button>
            <Button disabled={pending || !selectedMailbox}>{pending ? "跳转中..." : "前往授权"}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function ExternalImapDialog({ account, mailboxId, disabled, pending, onSubmit }: { account?: ExternalImapAccount; mailboxId: string; disabled?: boolean; pending: boolean; onSubmit: (payload: ExternalImapAccountPayload) => void }) {
  const [open, setOpen] = React.useState(false)
  const [tlsMode, setTlsMode] = React.useState<ExternalImapTlsMode>(account?.tlsMode || "tls")
  const [storageMode, setStorageMode] = React.useState<ExternalImapStorageMode>(account?.storageMode || "local")
  const [syncReadState, setSyncReadState] = React.useState(account?.syncReadState ?? true)
  const [enabled, setEnabled] = React.useState(account?.enabled ?? true)
  React.useEffect(() => {
    if (!open) return
    setTlsMode(account?.tlsMode || "tls")
    setStorageMode(account?.storageMode || "local")
    setSyncReadState(account?.syncReadState ?? true)
    setEnabled(account?.enabled ?? true)
  }, [account, open])

  function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const payload: ExternalImapAccountPayload = {
      mailboxId,
      name: String(form.get("name") || ""),
      host: String(form.get("host") || ""),
      port: Number(form.get("port") || (tlsMode === "tls" ? 993 : 143)),
      tlsMode,
      username: String(form.get("username") || ""),
      password: String(form.get("password") || ""),
      storageMode,
      syncReadState,
      enabled,
    }
    onSubmit(payload)
    if (!pending) setOpen(false)
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <Button type="button" variant={account ? "outline" : "default"} size={account ? "sm" : "default"} disabled={disabled} onClick={() => setOpen(true)}>
        {account ? "编辑" : "添加外部邮箱"}
      </Button>
      <DialogContent className="max-h-[92dvh] overflow-y-auto sm:max-w-xl">
        <DialogHeader><DialogTitle>{account ? "编辑外部 IMAP" : "添加外部 IMAP"}</DialogTitle></DialogHeader>
        <form className="space-y-4" onSubmit={submit}>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="显示名称"><Input name="name" defaultValue={account?.name || ""} placeholder="Gmail / 工作邮箱" /></Field>
            <Field label="用户名"><Input name="username" defaultValue={account?.username || ""} required placeholder="name@example.com" /></Field>
            <Field label="服务器"><Input name="host" defaultValue={account?.host || ""} required placeholder="imap.example.com" /></Field>
            <Field label="端口"><Input name="port" type="number" min={1} max={65535} defaultValue={account?.port || (tlsMode === "tls" ? 993 : 143)} /></Field>
            <Field label="加密方式">
              <Select value={tlsMode} onValueChange={(value) => setTlsMode(value as ExternalImapTlsMode)}>
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent><SelectItem value="tls">SSL/TLS</SelectItem><SelectItem value="starttls">STARTTLS</SelectItem><SelectItem value="plain">不加密</SelectItem></SelectContent>
              </Select>
            </Field>
            <Field label="存储模式">
              <Select value={storageMode} onValueChange={(value) => setStorageMode(value as ExternalImapStorageMode)}>
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent><SelectItem value="local">同步到本地</SelectItem><SelectItem value="remote">远端直连</SelectItem></SelectContent>
              </Select>
            </Field>
          </div>
          <Field label={account ? "密码（留空则不修改）" : "密码"}><PasswordInput name="password" required={!account} placeholder={account ? "不修改请留空" : "外部邮箱密码或授权码"} /></Field>
          <div className="grid gap-3 sm:grid-cols-2">
            <label className="flex items-center gap-2 rounded-lg border p-3 text-sm"><Checkbox checked={syncReadState} onCheckedChange={(checked) => setSyncReadState(checked === true)} />同步已读状态</label>
            <label className="flex items-center gap-2 rounded-lg border p-3 text-sm"><Checkbox checked={enabled} onCheckedChange={(checked) => setEnabled(checked === true)} />启用此账号</label>
          </div>
          <DialogFooter className="gap-2 [&>button]:w-full sm:[&>button]:w-auto">
            <Button type="button" variant="outline" onClick={() => setOpen(false)}>取消</Button>
            <Button disabled={pending || !mailboxId}>{pending ? "保存中..." : "保存"}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function externalStatusLabel(status: string) {
  return ({ idle: "未同步", ok: "正常", partial: "部分成功", error: "错误", running: "同步中" } as Record<string, string>)[status] || status || "未知"
}

function externalOAuthProviderLabel(provider?: ExternalImapOAuthProvider) {
  return provider === "gmail" ? "Gmail OAuth" : provider === "outlook" ? "Microsoft 365 / Outlook OAuth" : "OAuth"
}

function ExternalImapSyncPanel({ account, folders, runs, pending, onSyncFolder }: { account: ExternalImapAccount; folders: ExternalImapFolder[]; runs: ExternalImapSyncRun[]; pending: boolean; onSyncFolder: (id: string, folder: string) => void }) {
  const [folder, setFolder] = React.useState("")
  React.useEffect(() => {
    if (folder && folders.some((item) => item.name === folder)) return
    setFolder(folders[0]?.name || "INBOX")
  }, [folder, folders])
  return (
    <div className="rounded-lg bg-muted/40 p-3">
      <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_auto] md:items-end">
        <Field label="单文件夹同步">
          <Select value={folder} onValueChange={setFolder}>
            <SelectTrigger><SelectValue placeholder="选择远端文件夹" /></SelectTrigger>
            <SelectContent>{folders.map((item) => <SelectItem key={item.name} value={item.name}>{folderLabel(item.name)}</SelectItem>)}</SelectContent>
          </Select>
        </Field>
        <Button type="button" variant="outline" disabled={pending || !folder} onClick={() => onSyncFolder(account.id, folder)}><RefreshCcw className="h-4 w-4" />同步文件夹</Button>
      </div>
      <div className="mt-3 space-y-2">
        <div className="text-xs font-medium text-muted-foreground">最近同步记录</div>
        {runs.length === 0 && <div className="rounded-md border bg-background p-3 text-sm text-muted-foreground">暂无同步记录</div>}
        {runs.slice(0, 6).map((run) => (
          <div key={run.id} className="grid gap-2 rounded-md border bg-background p-3 text-sm md:grid-cols-[minmax(0,1fr)_auto] md:items-center">
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant={run.status === "ok" ? "secondary" : run.status === "failed" ? "destructive" : "outline"}>{externalStatusLabel(run.status)}</Badge>
                <span className="truncate">{run.folder ? folderLabel(run.folder) : "全部文件夹"}</span>
              </div>
              {run.error && <div className="mt-1 truncate text-xs text-destructive">{run.error}</div>}
            </div>
            <div className="text-xs text-muted-foreground md:text-right">
              <div>导入 {run.imported} · 跳过 {run.skipped} · 失败 {run.failed}</div>
              <div>{formatDateTime(run.startedAt)}</div>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

function formatDateTime(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

function normalizeForwardingTarget(value: string) {
  return value.trim().toLowerCase()
}

function sortForwardingTargets(targets: string[]) {
  return Array.from(new Set(targets.map((item) => item.trim()).filter(Boolean))).sort((a, b) => forwardingTargetCollator.compare(a, b))
}

function mergeForwardingTargets(...groups: string[][]) {
  const seen = new Set<string>()
  const merged: string[] = []
  for (const target of groups.flat()) {
    const value = target.trim()
    const key = normalizeForwardingTarget(value)
    if (!value || seen.has(key)) continue
    seen.add(key)
    merged.push(value)
  }
  return sortForwardingTargets(merged)
}

function withoutForwardingTargets(targets: string[], lockedTargets: string[]) {
  const locked = new Set(lockedTargets.map(normalizeForwardingTarget))
  return sortForwardingTargets(targets.filter((target) => !locked.has(normalizeForwardingTarget(target))))
}

function forwardingTargetsFromRule(rule: { targetEmail?: string; targetEmails?: string[] }) {
  const targets = rule.targetEmails?.length ? rule.targetEmails : rule.targetEmail ? [rule.targetEmail] : []
  return sortForwardingTargets(targets)
}

function forwardingTargetsFromSettings(settings?: ForwardingSettings) {
  const targets = settings?.accountTargetEmails?.length ? settings.accountTargetEmails : settings?.accountTargetEmail ? [settings.accountTargetEmail] : []
  return sortForwardingTargets(targets)
}

function ForwardingTargetPicker({ emails, selected, lockedSelected = [], lockedLabel = "账号级", onChange, disabled, placement = "bottom" }: { emails: string[]; selected: string[]; lockedSelected?: string[]; lockedLabel?: string; onChange: (targets: string[]) => void; disabled?: boolean; placement?: "bottom" | "top" }) {
  const [open, setOpen] = React.useState(false)
  const [query, setQuery] = React.useState("")
  const lockedEmails = React.useMemo(() => sortForwardingTargets(lockedSelected), [lockedSelected])
  const lockedSet = React.useMemo(() => new Set(lockedEmails.map(normalizeForwardingTarget)), [lockedEmails])
  const selectedEmailsOnly = React.useMemo(() => withoutForwardingTargets(selected, lockedEmails), [lockedEmails, selected])
  const selectedSet = React.useMemo(() => new Set(selectedEmailsOnly.map(normalizeForwardingTarget)), [selectedEmailsOnly])
  const sortedEmails = React.useMemo(() => {
    return sortForwardingTargets(emails)
  }, [emails])
  const regularEmails = React.useMemo(() => sortedEmails.filter((email) => !lockedSet.has(normalizeForwardingTarget(email))), [lockedSet, sortedEmails])
  const selectedEmails = React.useMemo(() => [...lockedEmails, ...regularEmails.filter((email) => selectedSet.has(normalizeForwardingTarget(email)))], [lockedEmails, regularEmails, selectedSet])
  const normalizedQuery = query.trim().toLowerCase()
  const filteredLockedEmails = normalizedQuery ? lockedEmails.filter((email) => email.toLowerCase().includes(normalizedQuery)) : lockedEmails
  const filteredEmails = normalizedQuery ? regularEmails.filter((email) => email.toLowerCase().includes(normalizedQuery)) : regularEmails
  const label = selectedEmails.length ? `调整转发目标（已选 ${selectedEmails.length} 个）` : "添加转发目标"
  function toggle(email: string, checked: boolean) {
    if (disabled) return
    const next = checked ? mergeForwardingTargets(selectedEmailsOnly, [email]) : selectedEmailsOnly.filter((item) => normalizeForwardingTarget(item) !== normalizeForwardingTarget(email))
    onChange(withoutForwardingTargets(next, lockedEmails))
  }
  if (sortedEmails.length === 0 && lockedEmails.length === 0) {
    return <div className="rounded-md border border-dashed px-3 py-2 text-sm text-muted-foreground">暂无已验证邮箱</div>
  }
  return (
    <div className="relative min-w-0 space-y-2">
      {selectedEmails.length > 0 && (
        <div className="flex max-h-32 min-w-0 max-w-full flex-wrap gap-2 overflow-y-auto rounded-md border bg-background/55 p-2" aria-label="已选择的转发目标">
          {selectedEmails.map((email) => {
            const locked = lockedSet.has(normalizeForwardingTarget(email))
            return (
              <span key={email} className="inline-flex min-w-0 max-w-full items-center gap-1 rounded-md border bg-primary/10 px-2 py-1 text-xs font-medium text-foreground">
                <span className="min-w-0 max-w-[15rem] break-all">{email}</span>
                {locked ? <span className="shrink-0 text-[10px] text-muted-foreground">{lockedLabel}</span> : (
                  <Button type="button" variant="ghost" size="icon" className="size-6 shrink-0 text-muted-foreground hover:bg-muted hover:text-foreground" aria-label={`移除 ${email}`} disabled={disabled} onClick={() => toggle(email, false)}>
                    <X className="h-3.5 w-3.5" />
                  </Button>
                )}
              </span>
            )
          })}
        </div>
      )}
      <Button
        type="button"
        variant="outline"
        className={cn(
          "h-11 w-full justify-between gap-2 bg-background px-3 text-left text-sm font-normal hover:bg-accent/60 hover:text-foreground md:h-[37px]",
          open && "border-primary/35 ring-1 ring-primary/15",
          disabled && "cursor-not-allowed opacity-60 hover:bg-background",
        )}
        disabled={disabled}
        onClick={() => setOpen((value) => !value)}
      >
        <span className={cn("min-w-0 flex-1 truncate", selectedEmails.length ? "font-medium text-foreground" : "text-muted-foreground")}>{label}</span>
        <ChevronDown className={cn("h-4 w-4 shrink-0 text-muted-foreground transition-transform", open && "rotate-180")} />
      </Button>
      {open && (
        <div className={cn("absolute left-0 right-0 z-30 rounded-md border bg-popover p-2 shadow-sm", placement === "top" ? "bottom-full mb-2" : "mt-2")}>
          <div className="grid grid-cols-[minmax(0,1fr)_auto] gap-2">
            <div className="relative min-w-0">
              <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                className="h-8 pl-8 pr-8 text-sm shadow-none"
                placeholder="搜索已验证邮箱"
                autoFocus
              />
              {query && (
                <Button type="button" variant="ghost" size="icon" className="absolute right-0.5 top-0.5 size-7 text-muted-foreground" aria-label="清除搜索" title="清除搜索" onClick={() => setQuery("")}>
                  <X className="h-3.5 w-3.5" />
                </Button>
              )}
            </div>
            <Button type="button" variant="ghost" size="sm" className="h-8 px-2 text-xs font-normal text-muted-foreground hover:bg-muted hover:text-foreground" onClick={() => onChange([])}>
              清空
            </Button>
          </div>
          <div className="mt-2 max-h-[132px] overflow-y-auto pr-1">
            <div className="space-y-1">
              {filteredLockedEmails.map((email) => (
                <label
                  key={`locked-${email}`}
                  className="flex min-h-[36px] cursor-not-allowed items-center gap-2 rounded-md border border-border bg-muted/70 px-2.5 py-1.5 text-sm font-semibold text-muted-foreground"
                >
                  <Checkbox checked disabled />
                  <span className="min-w-0 flex-1 truncate">{email}</span>
                  <Badge variant="outline" className="h-5 shrink-0 rounded px-1.5 text-[10px]">{lockedLabel}</Badge>
                </label>
              ))}
              {filteredEmails.map((email) => {
                const checked = selectedSet.has(normalizeForwardingTarget(email))
                return (
                  <label
                    key={email}
                    className={cn(
                      "flex min-h-[36px] cursor-pointer items-center gap-2 rounded-md border border-transparent px-2.5 py-1.5 text-sm transition-colors hover:border-border hover:bg-muted/70",
                      checked && "border-primary/30 bg-primary/10 font-semibold text-primary",
                    )}
                  >
                    <Checkbox checked={checked} disabled={disabled} onCheckedChange={(value) => toggle(email, value === true)} />
                    <span className="min-w-0 flex-1 truncate">{email}</span>
                  </label>
                )
              })}
              {filteredLockedEmails.length === 0 && filteredEmails.length === 0 && <div className="px-2 py-8 text-center text-sm text-muted-foreground">没有匹配的已验证邮箱</div>}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function VerifiedEmailRow({ item, detail, busy, onResend, onRemove }: {
  item: ForwardingVerifiedEmail
  detail?: string
  busy: boolean
  onResend: (item: ForwardingVerifiedEmail) => void
  onRemove: (id: string, email: string) => void
}) {
  const tone = forwardingEmailTone(item)
  const [clock, setClock] = React.useState(() => Date.now())
  const sentAt = item.verificationSentAt ? Date.parse(item.verificationSentAt) : 0
  const resendWaitSeconds = item.verified || !Number.isFinite(sentAt) ? 0 : Math.max(0, Math.ceil((sentAt + 60_000 - clock) / 1000))
  React.useEffect(() => {
    if (item.verified || !Number.isFinite(sentAt) || sentAt <= 0 || sentAt + 60_000 <= Date.now()) return
    const timer = window.setInterval(() => setClock(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [item.verified, sentAt])
  return (
    <div className="grid min-h-[72px] gap-3 border-b px-4 py-3 last:border-b-0 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center">
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <span className={cn("size-2 shrink-0 rounded-full", tone.dotClass)} />
          <span className="min-w-0 break-all text-sm font-semibold" title={item.email}>{item.email}</span>
        </div>
        <div className={cn("mt-1 pl-4 text-xs leading-5", item.verified ? "text-muted-foreground" : tone.detailClass)}>
          {detail || (item.verified ? `验证于 ${formatDateTime(item.verifiedAt || item.createdAt)}` : forwardingEmailStatusText(item))}
        </div>
      </div>
      <div className="flex shrink-0 items-center justify-start gap-2 sm:justify-end">
        {!item.verified && (
          <Button type="button" variant="outline" size="sm" className="h-11 min-w-[5.5rem] sm:h-9" disabled={busy || resendWaitSeconds > 0} onClick={() => onResend(item)}>{resendWaitSeconds > 0 ? `${resendWaitSeconds}s 后重发` : "重发"}</Button>
        )}
        <Button type="button" variant="ghost" size="sm" className="h-11 text-destructive hover:bg-destructive/10 hover:text-destructive sm:h-9" disabled={busy} onClick={() => onRemove(item.id, item.email)} aria-label={`删除 ${item.email}`}>删除</Button>
      </div>
    </div>
  )
}

function forwardingEmailTone(item: ForwardingVerifiedEmail) {
  if (item.verified) {
    return {
      dotClass: "bg-emerald-500",
      detailClass: "text-emerald-700",
    }
  }
  if (item.deliveryStatus === "failed") {
    return {
      dotClass: "bg-destructive",
      detailClass: "text-destructive",
    }
  }
  if (item.deliveryStatus === "delivered") {
    return {
      dotClass: "bg-amber-500",
      detailClass: "text-amber-700",
    }
  }
  return {
    dotClass: "bg-muted-foreground",
    detailClass: "text-foreground",
  }
}

function forwardingEmailStatusText(item: ForwardingVerifiedEmail) {
  const time = item.verificationSentAt ? ` · 最近尝试 ${formatDateTime(item.verificationSentAt)}` : ""
  if (item.deliveryStatus === "failed") {
    return `验证邮件发送失败${item.deliveryError ? `：${item.deliveryError}` : ""}${time}`
  }
  if (item.deliveryStatus === "delivered") {
    return `验证邮件已交给邮件服务器，请检查收件箱和垃圾邮件${time}`
  }
  if (item.deliveryStatus === "sending") {
    return `验证邮件发送中${time}`
  }
  return `验证邮件排队发送中${time}`
}

function dateInputValue(date: Date) {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, "0")
  const day = String(date.getDate()).padStart(2, "0")
  return `${year}-${month}-${day}`
}

function dateInputToISOString(value: string) {
  if (!value) return undefined
  const [year, month, day] = value.split("-").map(Number)
  return new Date(year, month - 1, day, 23, 59, 59, 999).toISOString()
}

function ClientSettingsSection({ mailboxes, selectedMailboxId, hostname, onSelectMailbox, onCopy }: { mailboxes: Mailbox[]; selectedMailboxId: string; hostname?: string; onSelectMailbox: (id: string) => void; onCopy: (text: string) => void }) {
  const qc = useQueryClient()
  const { toast } = useToast()
  const telegram = useQuery({ queryKey: ["user-telegram-settings"], queryFn: api.userTelegramSettings })
  const [telegramEnabled, setTelegramEnabled] = React.useState(false)
  const [telegramBotToken, setTelegramBotToken] = React.useState("")
  const [telegramChatId, setTelegramChatId] = React.useState("")
  const [telegramMailboxIds, setTelegramMailboxIds] = React.useState<string[]>([])
  const [telegramPairing, setTelegramPairing] = React.useState<import("@/lib/api").TelegramPairing | null>(null)
  React.useEffect(() => {
    if (!telegram.data) return
    setTelegramEnabled(telegram.data.enabled)
    setTelegramChatId(telegram.data.privateChatId || "")
    setTelegramMailboxIds(telegram.data.mailboxIds || [])
  }, [telegram.data])
  const saveTelegram = useMutation({
    mutationFn: () => api.updateUserTelegramSettings({ enabled: telegramEnabled, botToken: telegramBotToken, privateChatId: telegramChatId, mailboxIds: telegramMailboxIds }),
    onSuccess: (data) => { setTelegramBotToken(""); qc.setQueryData(["user-telegram-settings"], data); toast({ title: "Telegram 邮件提醒已保存" }) },
    onError: (error) => toast({ title: "保存失败", description: error.message }),
  })
  const createTelegramPairing = useMutation({
    mutationFn: () => api.createUserTelegramPairing(telegramBotToken),
    onSuccess: (pairing) => { setTelegramPairing(pairing); toast({ title: "Telegram 绑定码已生成" }) },
    onError: (error) => toast({ title: "无法生成绑定码", description: error.message }),
  })
  const discoverTelegram = useMutation({
    mutationFn: () => api.discoverUserTelegramChat(telegramBotToken, telegramPairing?.code || ""),
    onSuccess: (chat) => { setTelegramChatId(chat.chatId); setTelegramEnabled(true); setTelegramPairing(null); toast({ title: "Telegram 私聊已绑定", description: "已自动开启新邮件提醒，请保存设置" }) },
    onError: (error) => toast({ title: "绑定失败", description: error.message }),
  })
  const testTelegram = useMutation({
    mutationFn: () => api.testUserTelegram(telegramBotToken, telegramChatId),
    onSuccess: () => toast({ title: "测试通知已发送" }),
    onError: (error) => toast({ title: "测试失败", description: error.message }),
  })
  const selected = mailboxes.find((item) => item.id === selectedMailboxId) || mailboxes[0]
  const server = clientServerHost(hostname, selected?.address)
  const rows = [
    { label: "IMAP 服务器", value: `${server}:993`, security: "SSL" },
    { label: "POP3 服务器", value: `${server}:995`, security: "SSL" },
    { label: "SMTP 服务器", value: `${server}:465`, security: "SSL" },
  ]
  return (
    <div className="space-y-6">
      <SettingsCard
        title="Telegram 邮件提醒"
        subtitle="每个账号使用自己的机器人，只提醒本人选择的邮箱。"
        action={<div className="flex flex-wrap justify-end gap-2"><Badge variant={telegram.data?.botConfigured ? "default" : "secondary"}>{telegram.data?.botConfigured ? "机器人已配置" : "机器人未配置"}</Badge><Badge variant={telegramEnabled ? "default" : "destructive"}>{telegramEnabled ? "提醒已开启" : "提醒已关闭"}</Badge></div>}
      >
        {telegram.isLoading ? <Skeleton className="h-32 w-full" /> : (
          <div className="space-y-5">
            <div className="flex items-center justify-between gap-4">
              <div className="min-w-0">
                <div className="text-sm font-medium">新邮件提醒</div>
                <div className="mt-1 truncate text-xs text-muted-foreground">{telegramChatId ? `已绑定私聊 ${telegramChatId}` : "尚未绑定 Telegram 私聊"}</div>
              </div>
              <Switch aria-label="开启新邮件提醒" checked={telegramEnabled} onCheckedChange={setTelegramEnabled} disabled={!telegram.data?.botConfigured && !telegramBotToken} />
            </div>
            <div className="space-y-2 border-t pt-4">
              <Label htmlFor="user-telegram-bot-token">Bot Token</Label>
              <Input id="user-telegram-bot-token" type="password" value={telegramBotToken} onChange={(event) => setTelegramBotToken(event.target.value)} placeholder={telegram.data?.botConfigured ? "已安全保存，留空不变" : "123456789:..."} />
              <p className="text-xs text-muted-foreground">Token 仅供当前账号发送邮件提醒，与后台备份机器人完全独立。</p>
            </div>
            <div className="flex flex-wrap gap-2 border-t pt-4">
              <Button type="button" variant="outline" size="sm" disabled={(!telegram.data?.botConfigured && !telegramBotToken) || createTelegramPairing.isPending} onClick={() => createTelegramPairing.mutate()}>
                <ShieldCheck className="h-4 w-4" />{createTelegramPairing.isPending ? "生成中" : telegramChatId ? "重新绑定" : "安全绑定"}
              </Button>
              <Button type="button" variant="outline" size="sm" disabled={!telegramChatId || testTelegram.isPending} onClick={() => testTelegram.mutate()}>
                <MailCheck className="h-4 w-4" />{testTelegram.isPending ? "发送中" : "测试提醒"}
              </Button>
            </div>
            {telegramPairing && (
              <div className="space-y-3 rounded-md border p-3">
                <div className="flex items-center gap-2">
                  <code className="min-w-0 flex-1 truncate font-mono text-sm font-semibold">{telegramPairing.code}</code>
                  <Button type="button" variant="ghost" size="icon" className="size-8" aria-label="复制绑定码" title="复制绑定码" onClick={() => onCopy(telegramPairing.code)}><Copy className="h-4 w-4" /></Button>
                </div>
                <div className="flex flex-wrap gap-2">
                  <Button asChild type="button" variant="outline" size="sm"><a href={telegramPairing.deepLink} target="_blank" rel="noreferrer"><ExternalLink className="h-4 w-4" />打开机器人</a></Button>
                  <Button type="button" size="sm" disabled={discoverTelegram.isPending} onClick={() => discoverTelegram.mutate()}><CheckCircle2 className="h-4 w-4" />{discoverTelegram.isPending ? "绑定中" : "完成绑定"}</Button>
                </div>
              </div>
            )}
            <div className="space-y-2 border-t pt-4">
              <Label>提醒邮箱</Label>
              <div className="grid gap-2 sm:grid-cols-2">
                {mailboxes.filter((mailbox) => mailbox.status === "active").map((mailbox) => (
                  <label key={mailbox.id} className="flex min-h-10 items-center gap-3 rounded-md border px-3 py-2">
                    <Checkbox checked={telegramMailboxIds.includes(mailbox.id)} onCheckedChange={(checked) => setTelegramMailboxIds((items) => checked === true ? Array.from(new Set([...items, mailbox.id])) : items.filter((id) => id !== mailbox.id))} />
                    <span className="min-w-0 truncate text-sm">{mailbox.address}</span>
                  </label>
                ))}
              </div>
            </div>
            <div className="flex justify-end"><Button type="button" size="sm" disabled={saveTelegram.isPending || (!telegram.data?.botConfigured && !telegramBotToken)} onClick={() => saveTelegram.mutate()}>{saveTelegram.isPending ? "保存中" : "保存设置"}</Button></div>
          </div>
        )}
      </SettingsCard>
      <Card>
        <CardHeader>
          <div className="flex items-start justify-between gap-4">
            <div>
              <CardTitle>第三方客户端</CardTitle>
              <div className="mt-1 text-sm text-muted-foreground">IMAP / POP3 / SMTP 配置用于 Thunderbird、Apple Mail、手机邮件客户端等。</div>
            </div>
            {!!selected && <Badge variant="secondary">{selected.address}</Badge>}
          </div>
        </CardHeader>
        <CardContent className="space-y-5">
          <Field label="选择邮箱">
            <Select value={selected?.id || ""} onValueChange={onSelectMailbox}>
              <SelectTrigger><SelectValue placeholder="选择邮箱" /></SelectTrigger>
              <SelectContent>{mailboxes.map((mailbox) => <SelectItem key={mailbox.id} value={mailbox.id}>{mailbox.address}</SelectItem>)}</SelectContent>
            </Select>
          </Field>

          {selected ? (
            <>
              <div className="rounded-lg border p-4">
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <div className="min-w-0">
                    <div className="truncate font-medium">{selected.address}</div>
                    <div className="mt-3 flex flex-wrap gap-2">
                      <Badge variant="secondary" className="bg-emerald-100 text-emerald-700">● IMAP</Badge>
                      <Badge variant="secondary" className="bg-emerald-100 text-emerald-700">● POP3</Badge>
                      <Badge variant="secondary" className="bg-emerald-100 text-emerald-700">● SMTP</Badge>
                    </div>
                  </div>
                  <Badge variant="outline">已启用</Badge>
                </div>
              </div>

              <div className="rounded-lg bg-muted p-5">
                <div className="mb-4 font-medium">客户端配置</div>
                <div className="space-y-3">
                  {rows.map((row) => (
                    <ClientConfigRow key={row.label} label={row.label} value={row.value} security={row.security} onCopy={onCopy} />
                  ))}
                </div>
                <Separator className="my-4" />
                <div className="grid gap-3 text-sm sm:grid-cols-[120px_minmax(0,1fr)]">
                  <div className="text-muted-foreground">用户名</div>
                  <div className="flex min-w-0 items-center justify-between gap-2">
                    <span className="truncate text-right sm:text-left">{selected.address}</span>
                    <Button type="button" variant="ghost" size="icon" className="size-7" aria-label="复制邮箱地址" title="复制邮箱地址" onClick={() => onCopy(selected.address)}><Copy className="h-4 w-4" /></Button>
                  </div>
                  <div className="text-muted-foreground">密码</div>
                  <div>邮箱登录密码</div>
                </div>
              </div>
            </>
          ) : (
            <EmptyState text="暂无邮箱，创建邮箱后可查看客户端配置" />
          )}
        </CardContent>
      </Card>
    </div>
  )
}

function ClientConfigRow({ label, value, security, onCopy }: { label: string; value: string; security: string; onCopy: (text: string) => void }) {
  return (
    <div className="grid items-center gap-2 text-sm sm:grid-cols-[120px_minmax(0,1fr)]">
      <div className="text-muted-foreground">{label}</div>
      <div className="flex min-w-0 items-center justify-between gap-2">
        <code className="truncate rounded border bg-background px-2 py-1 text-xs">{value}</code>
        <div className="flex shrink-0 items-center gap-1">
          <span className="text-xs font-medium text-emerald-600">{security}</span>
          <Button type="button" variant="ghost" size="icon" className="size-7" aria-label={`复制${label}`} title={`复制${label}`} onClick={() => onCopy(value)}><Copy className="h-4 w-4" /></Button>
        </div>
      </div>
    </div>
  )
}

const apiTokenScopeOptions = [
  ["messages:send", "发送邮件"], ["messages:read", "读取邮件与投递状态"], ["messages:manage", "重试或取消发送"],
  ["domains:read", "查看域名"], ["domains:write", "管理域名"], ["mailboxes:read", "查看邮箱"], ["mailboxes:write", "管理邮箱"],
  ["dns:read", "查看 DNS"], ["dns:check", "执行 DNS 检测"], ["aliases:read", "查看邮件转发"], ["aliases:write", "管理邮件转发"],
] as const

function ApiTokensSection({ items, loading, pending, onCreate, onUpdate, onDelete, onCopy }: { items: APIToken[]; loading: boolean; pending: boolean; onCreate: (payload: { name: string; expiresAt?: string; scopes: string[] }) => Promise<{ token: string; item: APIToken }>; onUpdate: (id: string, payload: { name?: string; expiresAt?: string; disabled?: boolean; scopes?: string[] }) => void; onDelete: (id: string) => void; onCopy: (text: string) => void }) {
  const [createDialogOpen, setCreateDialogOpen] = React.useState(false)
  const [createdToken, setCreatedToken] = React.useState("")
  const [pendingConfirm, setPendingConfirm] = React.useState<PendingConfirm | null>(null)
  const [scopes, setScopes] = React.useState<string[]>(["messages:send", "messages:read"])
  const [editingToken, setEditingToken] = React.useState<APIToken | null>(null)
  const [editingScopes, setEditingScopes] = React.useState<string[]>([])
  const defaultExpiresAt = React.useMemo(() => dateInputValue(new Date(Date.now() + 90 * 24 * 60 * 60 * 1000)), [])

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const target = event.currentTarget
    const form = new FormData(target)
    const expiresAt = dateInputToISOString(String(form.get("expiresAt") || ""))
    try {
      const res = await onCreate({ name: String(form.get("name") || "").trim(), expiresAt, scopes })
      setCreatedToken(res.token)
      target.reset()
      setScopes(["messages:send", "messages:read"])
      setCreateDialogOpen(false)
    } catch {
      // Mutation-level error handling already shows the toast.
    }
  }

  function openCreateDialog() {
    setCreatedToken("")
    setScopes(["messages:send", "messages:read"])
    setCreateDialogOpen(true)
  }

  return (
    <div>
      <section className="rounded-lg border bg-card">
        <div className="flex min-h-16 items-center justify-between gap-3 px-4 py-3">
          <h2 className="text-lg font-semibold leading-7">API 密钥</h2>
          <Button type="button" size="sm" className="h-8 rounded-md px-3 text-xs" onClick={openCreateDialog}>创建密钥</Button>
        </div>
        <div className="space-y-2 border-t px-4 py-4">
        {createdToken && (
          <div className="rounded-lg border border-amber-300 bg-amber-50 p-3 text-amber-950">
            <div className="flex items-center gap-2 text-sm font-semibold"><KeyRound className="h-4 w-4" />只显示一次</div>
            <div className="mt-2 flex min-w-0 flex-col gap-2 sm:flex-row">
              <code className="min-w-0 flex-1 overflow-x-auto rounded border bg-background px-3 py-2 text-xs">{createdToken}</code>
              <Button type="button" variant="outline" size="sm" onClick={() => onCopy(createdToken)}><Copy className="h-4 w-4" />复制</Button>
            </div>
          </div>
        )}

        {items.map((item) => {
          const expired = item.expiresAt ? new Date(item.expiresAt).getTime() <= Date.now() : false
          return (
            <div key={item.id} className="grid min-h-[74px] gap-3 rounded-lg border bg-background px-3 py-3 transition-colors hover:bg-muted/30 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-center">
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <div className="truncate text-sm font-semibold">{item.name}</div>
                  <Badge variant={item.disabled || expired ? "secondary" : "default"} className="h-5 px-1.5 text-[10px]">{item.disabled ? "已禁用" : expired ? "已过期" : "可用"}</Badge>
                </div>
                <div className="mt-2 grid gap-1 text-xs leading-5 text-muted-foreground sm:grid-cols-3">
                  <span>创建：{formatDateTime(item.createdAt)}</span>
                  <span>过期：{item.expiresAt ? formatDateTime(item.expiresAt) : "未设置"}</span>
                  <span>最后使用：{item.lastUsedAt ? formatDateTime(item.lastUsedAt) : "从未使用"}</span>
                </div>
                <div className="mt-2 flex flex-wrap gap-1">
                  {(item.scopes || ["*"]).map((scope) => <Badge key={scope} variant="outline" className="h-5 px-1.5 text-[10px] font-normal">{scope}</Badge>)}
                </div>
              </div>
              <div className="flex flex-wrap gap-2 lg:justify-end">
                <Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-xs" disabled={pending} onClick={() => { setEditingToken(item); setEditingScopes(item.scopes?.includes("*") ? ["messages:send", "messages:read"] : item.scopes || []) }}>编辑权限</Button>
                <Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-xs" disabled={pending || expired} onClick={() => onUpdate(item.id, { disabled: !item.disabled })}>{item.disabled ? "启用" : "禁用"}</Button>
                <Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-xs text-destructive hover:text-destructive" disabled={pending} onClick={() => setPendingConfirm({ title: "撤销 API 密钥？", description: `密钥“${item.name}”撤销后无法恢复，正在使用它的集成会立即失效。`, confirmText: "撤销密钥", destructive: true, onConfirm: () => { onDelete(item.id); setPendingConfirm(null) } })}>撤销</Button>
              </div>
            </div>
          )
        })}
        {!loading && items.length === 0 && <div className="grid min-h-[56px] place-items-center text-sm text-muted-foreground">暂无 API 密钥，点击上方按钮创建</div>}
        {loading && items.length === 0 && <div className="grid min-h-[56px] place-items-center text-sm text-muted-foreground">正在加载 API 密钥</div>}
        </div>
      </section>

      <Dialog open={createDialogOpen} onOpenChange={setCreateDialogOpen}>
        <DialogContent className="max-h-[92dvh] overflow-y-auto sm:max-w-2xl">
          <DialogHeader><DialogTitle>创建 API 密钥</DialogTitle></DialogHeader>
          <form className="space-y-4" onSubmit={submit}>
            <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_12rem]">
              <Field label="名称"><Input name="name" required maxLength={80} placeholder="billing-system" autoFocus /></Field>
              <Field label="到期日期"><Input name="expiresAt" type="date" defaultValue={defaultExpiresAt} min={dateInputValue(new Date())} required /></Field>
            </div>
            <Field label="授权范围">
              <div className="grid gap-2 sm:grid-cols-2">
                {apiTokenScopeOptions.map(([value, label]) => (
                  <label key={value} className="flex items-center gap-2 rounded-md border bg-background px-3 py-2 text-sm transition-colors hover:bg-muted/50">
                    <Checkbox checked={scopes.includes(value)} onCheckedChange={(checked) => setScopes((current) => checked === true ? [...current, value] : current.filter((scope) => scope !== value))} />
                    <span>{label}</span>
                  </label>
                ))}
              </div>
            </Field>
            <DialogFooter className="gap-2 [&>button]:w-full sm:[&>button]:w-auto">
              <Button type="button" variant="outline" onClick={() => setCreateDialogOpen(false)}>取消</Button>
              <Button disabled={pending || scopes.length === 0}>{pending ? "创建中..." : "创建密钥"}</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={!!editingToken} onOpenChange={(open) => { if (!open) setEditingToken(null) }}>
        <DialogContent className="max-h-[92dvh] overflow-y-auto sm:max-w-2xl">
          <DialogHeader><DialogTitle>编辑密钥权限</DialogTitle></DialogHeader>
          <div className="grid gap-2 sm:grid-cols-2">
            {apiTokenScopeOptions.map(([value, label]) => (
              <label key={value} className="flex items-center gap-2 rounded-md border px-3 py-2 text-sm">
                <Checkbox checked={editingScopes.includes(value)} onCheckedChange={(checked) => setEditingScopes((current) => checked === true ? [...current, value] : current.filter((scope) => scope !== value))} />
                <span>{label}</span>
              </label>
            ))}
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setEditingToken(null)}>取消</Button>
            <Button type="button" disabled={pending || editingScopes.length === 0} onClick={() => { if (editingToken) onUpdate(editingToken.id, { scopes: editingScopes }); setEditingToken(null) }}>保存权限</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog open={!!pendingConfirm} title={pendingConfirm?.title || ""} description={pendingConfirm?.description} confirmText={pendingConfirm?.confirmText || "撤销"} destructive={!!pendingConfirm?.destructive} pending={pending} onOpenChange={(open) => { if (!open) setPendingConfirm(null) }} onConfirm={() => pendingConfirm?.onConfirm()} />
    </div>
  )
}

function ContactsSection({ items, loading, onCreate, onDelete, onCopy, pending }: { items: { id: string; name: string; email: string; note: string }[]; loading: boolean; onCreate: (form: FormData) => void; onDelete: (id: string) => void; onCopy: (text: string) => void; pending: boolean }) {
  const [pendingConfirm, setPendingConfirm] = React.useState<PendingConfirm | null>(null)
  return (
    <div className="grid gap-6 lg:grid-cols-[380px_minmax(0,1fr)]">
      <Card>
        <CardHeader><CardTitle>新增联系人</CardTitle></CardHeader>
        <CardContent>
          <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); onCreate(new FormData(e.currentTarget)); e.currentTarget.reset() }}>
            <Field label="姓名"><Input name="name" placeholder="张三" /></Field>
            <Field label="邮箱"><Input name="email" type="email" required /></Field>
            <Field label="备注"><Input name="note" /></Field>
            <Button className="w-full" disabled={pending}>{pending ? "保存中..." : "保存联系人"}</Button>
          </form>
        </CardContent>
      </Card>
      <Card>
        <CardHeader><CardTitle>联系人列表</CardTitle></CardHeader>
        <CardContent className="space-y-2">
          {items.map((item) => (
            <div key={item.id} className="flex items-center justify-between gap-3 rounded-lg border p-3">
              <div className="min-w-0">
                <div className="truncate text-sm font-medium">{item.name}</div>
                <div className="truncate text-xs text-muted-foreground">{item.email}{item.note ? ` · ${item.note}` : ""}</div>
              </div>
              <div className="flex shrink-0 gap-1">
                <Button variant="ghost" size="icon" className="size-8" aria-label={`复制联系人邮箱 ${item.email}`} title="复制邮箱" onClick={() => onCopy(item.email)}><Copy className="h-4 w-4" /></Button>
                <Button variant="ghost" size="icon" className="size-8 text-destructive" aria-label={`删除联系人 ${item.email}`} title="删除联系人" onClick={() => setPendingConfirm({ title: "删除联系人？", description: `${item.email} 将从联系人列表中移除。`, confirmText: "删除联系人", onConfirm: () => { onDelete(item.id); setPendingConfirm(null) } })}><Trash2 className="h-4 w-4" /></Button>
              </div>
            </div>
          ))}
          {!loading && items.length === 0 && <EmptyState text="暂无联系人" />}
        </CardContent>
      </Card>
      <ConfirmDialog open={!!pendingConfirm} title={pendingConfirm?.title || ""} description={pendingConfirm?.description} confirmText={pendingConfirm?.confirmText || "删除"} destructive onOpenChange={(open) => { if (!open) setPendingConfirm(null) }} onConfirm={() => pendingConfirm?.onConfirm()} />
    </div>
  )
}

function CleanupSection({ mailbox, stats, showStats, pending, onCleanup }: { mailbox?: Mailbox; stats?: MailStats; showStats: boolean; pending: boolean; onCleanup: (target: "empty-trash" | "empty-spam" | "archive-read-inbox") => void }) {
  const [pendingConfirm, setPendingConfirm] = React.useState<PendingConfirm | null>(null)
  function confirmCleanup(target: "empty-trash" | "empty-spam" | "archive-read-inbox", title: string, destructive = false) {
    setPendingConfirm({
      title,
      description: mailbox ? `将对 ${mailbox.address} 执行此清理操作。` : "请先选择邮箱。",
      confirmText: destructive ? "确认清空" : "确认处理",
      destructive,
      onConfirm: () => { onCleanup(target); setPendingConfirm(null) },
    })
  }
  return (
    <div className="space-y-4">
      {showStats && <StatsSummary stats={stats} />}
      <SettingsCard title="清理当前邮箱" subtitle={mailbox ? `当前邮箱：${mailbox.address}` : "请先选择邮箱"} contentClassName="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          <CleanupButton icon={<MailCheck className="h-4 w-4" />} title="归档已读收件箱" disabled={!mailbox || pending} onClick={() => confirmCleanup("archive-read-inbox", "归档已读收件箱？")} />
          <CleanupButton icon={<MailX className="h-4 w-4" />} title="清空垃圾邮件" disabled={!mailbox || pending} onClick={() => confirmCleanup("empty-spam", "清空垃圾邮件？", true)} />
          <CleanupButton icon={<Trash2 className="h-4 w-4" />} title="清空回收站" disabled={!mailbox || pending} onClick={() => confirmCleanup("empty-trash", "清空回收站？", true)} />
      </SettingsCard>
      <ConfirmDialog open={!!pendingConfirm} title={pendingConfirm?.title || ""} description={pendingConfirm?.description} confirmText={pendingConfirm?.confirmText || "确认"} destructive={!!pendingConfirm?.destructive} pending={pending} onOpenChange={(open) => { if (!open) setPendingConfirm(null) }} onConfirm={() => pendingConfirm?.onConfirm()} />
    </div>
  )
}

type RuleCreatePayload = {
  mailboxId: string
  name: string
  matchMode: "all" | "any"
  conditions: MailRuleCondition[]
  actions: MailRuleAction[]
  applyToExisting: boolean
  stopProcessing: boolean
  enabled: boolean
}

type RuleConditionField = NonNullable<MailRuleCondition["field"]>
type RuleConditionOperator = NonNullable<MailRuleCondition["operator"]>
const conditionFieldLabels: Record<RuleConditionField, string> = { from: "发件人地址", to: "收件人地址", cc: "抄送地址", subject: "邮件主题", body: "邮件正文", attachment: "附件名称", size: "邮件大小", date: "收信日期" }
const conditionFieldShortLabels: Record<RuleConditionField, string> = { from: "发件人", to: "收件人", cc: "抄送", subject: "邮件主题", body: "body", attachment: "附件", size: "邮件大小", date: "收信日期" }
const conditionOperatorLabels: Record<RuleConditionOperator, string> = { contains: "包含", "not-contains": "不包含", equals: "等于", "not-equals": "不等于", "starts-with": "开头是", "ends-with": "结尾是", gt: "大于", gte: "大于等于", lt: "小于", lte: "小于等于", before: "早于", after: "晚于", on: "当天" }
const textConditionOperators: RuleConditionOperator[] = ["contains", "not-contains", "equals", "not-equals", "starts-with", "ends-with"]
const sizeConditionOperators: RuleConditionOperator[] = ["gt", "gte", "lt", "lte", "equals", "not-equals"]
const dateConditionOperators: RuleConditionOperator[] = ["before", "after", "on", "equals", "not-equals"]
const conditionFields = Object.keys(conditionFieldLabels) as RuleConditionField[]
const commonRuleFolders = ["Inbox", "Archive", "Spam", "Trash"]
const ruleActionLabels: Record<MailRuleAction["type"], string> = { archive: "移入归档", trash: "移入回收站", star: "添加星标", "mark-read": "标记已读", label: "添加标签", move: "移动到", forward: "邮件转发" }

function RulesSection({ items, mailboxes, labels, verifiedEmails, open, onOpenChange, onCreate, onUpdate, onToggle, onMove, onApply, onDelete, pending }: { items: MailRule[]; mailboxes: Mailbox[]; labels: MailLabel[]; verifiedEmails: string[]; open: boolean; onOpenChange: (open: boolean) => void; onCreate: (payload: RuleCreatePayload) => void; onUpdate: (id: string, payload: RuleCreatePayload) => void; onToggle: (item: MailRule) => void; onMove: (id: string, direction: "up" | "down") => void; onApply: (id: string) => void; onDelete: (id: string) => void; pending: boolean }) {
  const [editingRule, setEditingRule] = React.useState<MailRule | null>(null)
  const [query, setQuery] = React.useState("")
  const normalizedQuery = query.trim().toLocaleLowerCase()
  const filteredItems = items.map((item, index) => ({ item, index })).filter(({ item }) => !normalizedQuery || item.name.toLocaleLowerCase().includes(normalizedQuery))

  function setDialogOpen(next: boolean) {
    if (!next) setEditingRule(null)
    onOpenChange(next)
  }

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-2">
        <div className="relative min-w-0">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索规则名称" aria-label="搜索规则" className="h-10 pl-9 pr-10" />
          {query && <Button type="button" variant="ghost" size="icon" className="absolute right-1 top-1 size-8 text-muted-foreground" onClick={() => setQuery("")} aria-label="清除规则搜索" title="清除搜索"><X className="h-4 w-4" /></Button>}
        </div>
        <Button className="h-10 shrink-0 rounded-md px-3 text-sm font-normal sm:px-4" onClick={() => { setEditingRule(null); onOpenChange(true) }}>新建规则</Button>
      </div>
      <div className="space-y-3">
        {filteredItems.map(({ item, index }) => <RuleListItem key={item.id} item={item} index={index} count={items.length} mailboxLabel={item.mailboxId ? mailboxes.find((mailbox) => mailbox.id === item.mailboxId)?.address || "指定邮箱" : "全部邮箱"} pending={pending} onEdit={() => { setEditingRule(item); onOpenChange(true) }} onToggle={() => onToggle(item)} onMove={(direction) => onMove(item.id, direction)} onApply={() => onApply(item.id)} onDelete={onDelete} />)}
        {items.length === 0 && <EmptyState icon={<SlidersHorizontal />} text="暂无收件规则" description="新建规则后，可自动标记、移动或转发符合条件的邮件。" className="min-h-[180px] border-solid bg-card" />}
        {items.length > 0 && filteredItems.length === 0 && <EmptyState icon={<Search />} text="没有找到规则" description={`没有名称包含“${query.trim()}”的规则。`} className="min-h-[180px] border-solid bg-card" />}
      </div>
      <RuleDialog open={open} onOpenChange={setDialogOpen} mailboxes={mailboxes} labels={labels} verifiedEmails={verifiedEmails} pending={pending} initialRule={editingRule} onSave={(payload) => editingRule ? onUpdate(editingRule.id, payload) : onCreate(payload)} />
    </div>
  )
}

function RuleDialog({ open, onOpenChange, mailboxes, labels, verifiedEmails, pending, initialRule, onSave }: { open: boolean; onOpenChange: (open: boolean) => void; mailboxes: Mailbox[]; labels: MailLabel[]; verifiedEmails: string[]; pending: boolean; initialRule: MailRule | null; onSave: (payload: RuleCreatePayload) => void }) {
  const [name, setName] = React.useState("我的规则")
  const [mailboxId, setMailboxId] = React.useState("all")
  const [matchMode, setMatchMode] = React.useState<"all" | "any">("all")
  const [conditions, setConditions] = React.useState<MailRuleCondition[]>([{ field: "to", operator: "contains", value: "" }])
  const [actions, setActions] = React.useState<MailRuleAction[]>([{ type: "forward", value: "" }])
  const [enabled, setEnabled] = React.useState(true)
  const [applyToExisting, setApplyToExisting] = React.useState(false)
  const [stopProcessing, setStopProcessing] = React.useState(false)
  const selectedMailboxId = mailboxId === "all" ? "" : mailboxId
  const labelQuery = useQuery({ queryKey: ["labels", "rule-dialog", selectedMailboxId], queryFn: () => api.labels(selectedMailboxId), enabled: !!selectedMailboxId })
  const folderQueryMailboxId = selectedMailboxId || "all"
  const folderQuery = useQuery({ queryKey: ["folders", "rule-dialog", folderQueryMailboxId], queryFn: () => api.folders(folderQueryMailboxId), enabled: open })
  const availableLabels = selectedMailboxId ? (labelQuery.data?.items || []) : labels
  const availableFolders = folderQuery.data?.items || []

  React.useEffect(() => {
    if (!open) return
    setName(initialRule?.name || "我的规则")
    setMailboxId(initialRule?.mailboxId || "all")
    setMatchMode(initialRule?.matchMode || "all")
    setConditions(initialRule?.conditions.length ? initialRule.conditions : [{ field: "to", operator: "contains", value: "" }])
    setActions(initialRule?.actions.length ? initialRule.actions : [{ type: "forward", value: "" }])
    setEnabled(initialRule?.enabled ?? true)
    setApplyToExisting(initialRule?.applyToExisting ?? false)
    setStopProcessing(initialRule?.stopProcessing ?? false)
  }, [initialRule, open])

  function updateCondition(index: number, patch: Partial<MailRuleCondition>) {
    setConditions((items) => items.map((item, i) => {
      if (i !== index) return item
      const next = { ...item, ...patch }
      if (patch.field && !conditionOperatorsForField(patch.field).includes(next.operator || "contains")) {
        next.operator = defaultConditionOperator(patch.field)
      }
      return next
    }))
  }
  function updateAction(index: number, patch: Partial<MailRuleAction>) {
    setActions((items) => items.map((item, i) => i === index ? normalizeDraftAction({ ...item, ...patch }, availableLabels) : item))
  }
  function addCondition() { setConditions((items) => [...items, { field: "subject", operator: "contains", value: "" }]) }
  function addAction() { setActions((items) => [...items, { type: "forward", value: "" }]) }
  function removeCondition(index: number) { setConditions((items) => items.length > 1 ? items.filter((_, i) => i !== index) : items) }
  function removeAction(index: number) { setActions((items) => items.length > 1 ? items.filter((_, i) => i !== index) : items) }

  const validConditions = conditions.map((item) => ({ ...item, value: (item.value || "").trim() })).filter((item) => item.field && item.operator && item.value)
  const validActions = actions.map((item) => normalizeDraftAction(item, availableLabels)).map((item) => item.type === "forward" ? { ...item, value: verifiedRuleForwardTargets(item.value || "", verifiedEmails).join(", ") } : item).filter((item) => item.type !== "label" || item.value || item.labelId).filter((item) => item.type !== "move" || item.value).filter((item) => item.type !== "forward" || item.value)
  const canCreate = validConditions.length > 0 && validActions.length > 0 && !pending

  function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!canCreate) return
    onSave({ mailboxId: selectedMailboxId, name: name.trim() || "我的规则", matchMode, conditions: validConditions, actions: validActions, applyToExisting, stopProcessing, enabled })
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="!flex h-dvh w-screen max-w-none flex-col gap-0 overflow-hidden border-0 p-0 sm:h-auto sm:max-h-[92vh] sm:w-[min(94vw,56rem)] sm:border">
        <DialogHeader className="shrink-0 border-b px-4 py-4 pr-16 text-left sm:px-8 sm:py-6 sm:pr-16">
          <DialogTitle className="text-xl sm:text-2xl">{initialRule ? "编辑规则" : "新建规则"}</DialogTitle>
        </DialogHeader>
        <form className="flex min-h-0 flex-1 flex-col" onSubmit={submit}>
          <div className="min-h-0 flex-1 space-y-6 overflow-y-auto overscroll-contain px-4 py-5 sm:space-y-7 sm:px-8 sm:py-7">
            <Field label="名称"><Input className="h-11 sm:h-9" value={name} onChange={(event) => setName(event.target.value)} placeholder="我的规则" /></Field>
            <Field label="适用邮箱"><MailboxSelect value={mailboxId} mailboxes={mailboxes} onChange={setMailboxId} /></Field>

            <div className="space-y-4">
              <div className="space-y-2 text-sm">
                <div>当新邮件到达时，满足以下</div>
                <Select value={matchMode} onValueChange={(value) => setMatchMode(value as "all" | "any")}>
                  <SelectTrigger className="h-11 w-full md:h-9 md:w-[180px]"><SelectValue /></SelectTrigger>
                  <SelectContent><SelectItem value="all">所有条件</SelectItem><SelectItem value="any">任一条件</SelectItem></SelectContent>
                </Select>
              </div>
              <div className="space-y-3">
                {conditions.map((condition, index) => (
                  <div key={index} className="grid grid-cols-2 gap-2 rounded-md border p-3 md:grid-cols-[180px_128px_minmax(0,1fr)_auto_auto] md:gap-3 md:border-0 md:p-0">
                    <Select value={condition.field || "from"} onValueChange={(value) => updateCondition(index, { field: value as RuleConditionField })}>
                      <SelectTrigger className="h-11 md:h-9"><SelectValue /></SelectTrigger>
                      <SelectContent>{conditionFields.map((value) => <SelectItem key={value} value={value}>{conditionFieldLabels[value]}</SelectItem>)}</SelectContent>
                    </Select>
                    <Select value={condition.operator || defaultConditionOperator(condition.field)} onValueChange={(value) => updateCondition(index, { operator: value as RuleConditionOperator })}>
                      <SelectTrigger className="h-11 md:h-9"><SelectValue /></SelectTrigger>
                      <SelectContent>{conditionOperatorsForField(condition.field).map((value) => <SelectItem key={value} value={value}>{conditionOperatorLabels[value]}</SelectItem>)}</SelectContent>
                    </Select>
                    <Input className="col-span-2 h-11 md:col-span-1 md:h-9" type={condition.field === "date" ? "date" : "text"} value={condition.value || ""} onChange={(event) => updateCondition(index, { value: event.target.value })} placeholder={conditionPlaceholder(condition.field)} />
                    <div className="col-span-2 flex justify-end gap-2 md:contents">
                      <Button type="button" variant="ghost" size="icon" className="size-11 text-muted-foreground md:size-9" aria-label={`删除第 ${index + 1} 个条件`} title="删除条件" onClick={() => removeCondition(index)} disabled={conditions.length === 1}><X className="h-4 w-4" /></Button>
                      <Button type="button" variant="outline" size="icon" className="size-11 md:size-9" aria-label="添加条件" title="添加条件" onClick={addCondition}><Plus className="h-4 w-4" /></Button>
                    </div>
                  </div>
                ))}
              </div>
            </div>

            <div className="space-y-4">
              <div className="text-sm">执行以下动作</div>
              <div className="space-y-3">
                {actions.map((action, index) => (
                  <div key={index} className="grid gap-2 rounded-md border p-3 md:grid-cols-[180px_minmax(0,1fr)_auto_auto] md:gap-3 md:border-0 md:p-0">
                    <Select value={action.type} onValueChange={(value) => updateAction(index, { type: value as MailRuleAction["type"], value: "", labelId: "" })}>
                      <SelectTrigger className="h-11 md:h-9"><SelectValue /></SelectTrigger>
                      <SelectContent>{(Object.keys(ruleActionLabels) as MailRuleAction["type"][]).map((value) => <SelectItem key={value} value={value}>{ruleActionLabels[value]}</SelectItem>)}</SelectContent>
                    </Select>
                    <div className="min-w-0"><RuleActionValue action={action} folders={availableFolders} labels={availableLabels} verifiedEmails={verifiedEmails} onChange={(patch) => updateAction(index, patch)} /></div>
                    <div className="flex justify-end gap-2 md:contents">
                      <Button type="button" variant="ghost" size="icon" className="size-11 text-muted-foreground md:size-9" aria-label={`删除第 ${index + 1} 个操作`} title="删除操作" onClick={() => removeAction(index)} disabled={actions.length === 1}><X className="h-4 w-4" /></Button>
                      <Button type="button" variant="outline" size="icon" className="size-11 md:size-9" aria-label="添加操作" title="添加操作" onClick={addAction}><Plus className="h-4 w-4" /></Button>
                    </div>
                  </div>
                ))}
              </div>
            </div>

            <Separator />

            <div className="space-y-4">
              <RuleCheckbox checked={enabled} onCheckedChange={setEnabled} label="立即启用" />
              <RuleCheckbox checked={applyToExisting} onCheckedChange={setApplyToExisting} label="应用于现有邮件" />
              <div className="flex items-start gap-2">
                <RuleCheckbox checked={stopProcessing} onCheckedChange={setStopProcessing} label="终止规则：命中此规则后不再应用其他规则" />
                <Info className="mt-1 h-4 w-4 shrink-0 text-muted-foreground" />
              </div>
            </div>
          </div>
          <DialogFooter className="shrink-0 gap-2 border-t px-4 pb-[calc(1rem+env(safe-area-inset-bottom))] pt-4 sm:px-8 sm:py-5 [&>button]:min-h-11 [&>button]:w-full sm:[&>button]:min-h-9 sm:[&>button]:w-auto">
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>取消</Button>
            <Button disabled={!canCreate}>{pending ? "保存中..." : initialRule ? "保存修改" : "创建"}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function RuleActionValue({ action, folders, labels, verifiedEmails, onChange }: { action: MailRuleAction; folders: MailFolder[]; labels: MailLabel[]; verifiedEmails: string[]; onChange: (patch: Partial<MailRuleAction>) => void }) {
  if (action.type === "label") {
    if (labels.length > 0) {
      return (
        <Select value={action.value || labels[0].name} onValueChange={(value) => onChange({ value, labelId: labels.find((item) => item.name === value)?.id || "" })}>
          <SelectTrigger><SelectValue placeholder="选择标签" /></SelectTrigger>
          <SelectContent>{labels.map((label) => <SelectItem key={label.id} value={label.name}>{label.name}</SelectItem>)}</SelectContent>
        </Select>
      )
    }
    return <Input value={action.value || ""} onChange={(event) => onChange({ value: event.target.value, labelId: "" })} placeholder="标签名称" />
  }
  if (action.type === "move") {
    const value = action.value || "Archive"
    const customFolders = ruleCustomFolders(folders, value)
    return (
      <Select value={value} onValueChange={(next) => onChange({ value: next })}>
        <SelectTrigger className="h-11 md:h-9"><SelectValue placeholder="选择文件夹" /></SelectTrigger>
        <SelectContent>
          <SelectGroup>
            <SelectLabel className="text-xs font-medium text-muted-foreground">系统文件夹</SelectLabel>
            <SelectItem value="Inbox">收件箱</SelectItem>
            <SelectItem value="Archive">归档</SelectItem>
            <SelectItem value="Spam">垃圾邮件</SelectItem>
            <SelectItem value="Trash">已删除</SelectItem>
          </SelectGroup>
          <SelectSeparator />
          <SelectGroup>
            <SelectLabel className="text-xs font-medium text-muted-foreground">自定义文件夹</SelectLabel>
            {customFolders.map((folder) => <SelectItem key={folder} value={folder}>{folder}</SelectItem>)}
            {customFolders.length === 0 && <div className="px-2 py-2 text-sm text-muted-foreground">暂无自定义文件夹</div>}
          </SelectGroup>
        </SelectContent>
      </Select>
    )
  }
  if (action.type === "forward") {
    return <RuleForwardTargets value={action.value || ""} emails={verifiedEmails} onChange={(value) => onChange({ value })} />
  }
  return <Input value="无需填写" readOnly />
}

function ruleCustomFolders(folders: MailFolder[], selectedValue: string) {
  const names = folders
    .map((folder) => folder.name.trim())
    .filter((name) => name && !commonRuleFolders.some((systemName) => systemName.toLowerCase() === name.toLowerCase()) && !["Sent", "Drafts"].some((systemName) => systemName.toLowerCase() === name.toLowerCase()))
  if (selectedValue && !commonRuleFolders.includes(selectedValue) && !names.some((name) => name.toLowerCase() === selectedValue.toLowerCase())) names.push(selectedValue)
  return Array.from(new Set(names)).sort((a, b) => a.localeCompare(b, "zh-CN", { numeric: true, sensitivity: "base" }))
}

function RuleForwardTargets({ value, emails, onChange }: { value: string; emails: string[]; onChange: (value: string) => void }) {
  const selected = React.useMemo(() => verifiedRuleForwardTargets(value, emails), [emails, value])
  return <ForwardingTargetPicker emails={emails} selected={selected} onChange={(targets) => onChange(targets.join(", "))} placement="top" />
}

function verifiedRuleForwardTargets(value: string, verifiedEmails: string[]) {
  const verifiedByAddress = new Map(verifiedEmails.map((email) => [email.trim().toLowerCase(), email.trim()]))
  return Array.from(new Set(value.split(/[\n\r,，;；]+/).map((item) => verifiedByAddress.get(item.trim().toLowerCase())).filter((item): item is string => !!item)))
}

function RuleCheckbox({ checked, onCheckedChange, label }: { checked: boolean; onCheckedChange: (checked: boolean) => void; label: string }) {
  const id = React.useId()
  return <div className="flex min-w-0 items-start gap-3"><Checkbox id={id} className="mt-0.5" checked={checked} onCheckedChange={(value) => onCheckedChange(value === true)} /><Label htmlFor={id} className="text-sm font-medium leading-5 sm:text-base sm:leading-6">{label}</Label></div>
}

function RuleListItem({ item, index, count, mailboxLabel, pending, onEdit, onToggle, onMove, onApply, onDelete }: { item: MailRule; index: number; count: number; mailboxLabel: string; pending: boolean; onEdit: () => void; onToggle: () => void; onMove: (direction: "up" | "down") => void; onApply: () => void; onDelete: (id: string) => void }) {
  const [confirmOpen, setConfirmOpen] = React.useState(false)
  const conditionText = ruleConditionSummary(item.conditions, item.fromContains, item.subjectContains)
  const actionText = item.actions.map(ruleActionSummary).filter(Boolean).join("；") || "无动作"
  return (
    <div className="grid min-h-[110px] grid-cols-1 gap-3 rounded-lg border bg-card px-4 py-4 transition-colors hover:bg-muted/20 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center">
      <div className="min-w-0 space-y-2 sm:space-y-1">
        <div className="flex min-w-0 items-start gap-2 leading-6 sm:items-center">
          <h3 className="min-w-0 flex-1 break-words text-base font-semibold text-foreground [overflow-wrap:anywhere] sm:truncate">{item.name}</h3>
          <span className={cn("shrink-0 rounded bg-emerald-100 px-1.5 py-0.5 text-xs font-medium", item.enabled ? "text-emerald-700" : "bg-muted text-muted-foreground")}>{item.enabled ? "已启用" : "已停用"}</span>
        </div>
        <div className="grid gap-1 text-sm leading-5 text-muted-foreground sm:gap-0 sm:leading-6">
          <p className="flex min-w-0 items-start gap-1"><span className="shrink-0">适用：</span><span className="min-w-0 break-words [overflow-wrap:anywhere] sm:truncate">{mailboxLabel}</span></p>
          <p className="flex min-w-0 items-start gap-1"><span className="shrink-0">条件：</span><span className="min-w-0 break-words [overflow-wrap:anywhere] sm:truncate">{conditionText}</span></p>
          <p className="flex min-w-0 items-start gap-1"><span className="shrink-0">动作：</span><span className="min-w-0 break-words [overflow-wrap:anywhere] sm:truncate">{actionText}</span></p>
        </div>
      </div>
      <div className="flex shrink-0 items-center justify-end gap-1 border-t pt-3 sm:border-t-0 sm:pt-0">
        <Button type="button" variant="ghost" size="icon" className="size-9 text-muted-foreground sm:size-7" disabled={pending || index === 0} onClick={() => onMove("up")} aria-label="上移" title="上移"><ChevronUp className="h-4 w-4" /></Button>
        <Button type="button" variant="ghost" size="icon" className="size-9 text-muted-foreground sm:size-7" disabled={pending || index === count - 1} onClick={() => onMove("down")} aria-label="下移" title="下移"><ChevronDown className="h-4 w-4" /></Button>
        <span className="mx-1 h-6 w-px bg-border" />
        <Button type="button" variant="ghost" size="icon" className={cn("size-9 sm:size-7", item.enabled ? "text-emerald-600" : "text-muted-foreground")} disabled={pending} onClick={onToggle} aria-label={item.enabled ? "禁用规则" : "启用规则"} title={item.enabled ? "禁用规则" : "启用规则"}>{item.enabled ? <Bell className="h-4 w-4" /> : <BellOff className="h-4 w-4" />}</Button>
        <Button type="button" variant="ghost" size="icon" className="size-9 text-muted-foreground sm:size-7" disabled={pending} onClick={onEdit} aria-label="编辑规则" title="编辑规则"><PencilLine className="h-4 w-4" /></Button>
        <Button type="button" variant="ghost" size="icon" className="size-9 text-muted-foreground sm:size-7" disabled={pending} onClick={onApply} aria-label="应用到现有邮件" title="应用到现有邮件"><PlayCircle className="h-4 w-4" /></Button>
        <Button variant="ghost" size="icon" className="size-9 shrink-0 text-destructive hover:bg-destructive/10 hover:text-destructive sm:size-7" disabled={pending} onClick={() => setConfirmOpen(true)} aria-label="删除规则" title="删除规则"><Trash2 className="h-4 w-4" /></Button>
      </div>
      <ConfirmDialog open={confirmOpen} title="删除收件规则？" description={`规则“${item.name}”将不再处理后续邮件。`} confirmText="删除规则" destructive onOpenChange={setConfirmOpen} onConfirm={() => { onDelete(item.id); setConfirmOpen(false) }} />
    </div>
  )
}

function normalizeDraftAction(action: MailRuleAction, labels: MailLabel[]): MailRuleAction {
  if (action.type === "label") {
    const value = action.value || labels[0]?.name || ""
    return { type: "label", value, labelId: labels.find((label) => label.name === value)?.id || action.labelId || "" }
  }
  if (action.type === "move") return { type: "move", value: action.value || "Archive" }
  if (action.type === "forward") return { type: "forward", value: (action.value || "").trim() }
  return { type: action.type }
}

function conditionOperatorsForField(field?: MailRuleCondition["field"]) {
  if (field === "size") return sizeConditionOperators
  if (field === "date") return dateConditionOperators
  return textConditionOperators
}

function defaultConditionOperator(field?: MailRuleCondition["field"]): RuleConditionOperator {
  if (field === "size") return "gte"
  if (field === "date") return "on"
  return "contains"
}

function conditionPlaceholder(field?: MailRuleCondition["field"]) {
  if (field === "size") return "例如 10mb"
  if (field === "date") return "选择日期"
  if (field === "attachment") return "输入附件名或扩展名"
  if (field === "to" || field === "from" || field === "cc") return "输入邮箱或关键词"
  if (field === "subject") return "输入主题关键词"
  return "输入值"
}

function ruleConditionSummary(conditions: MailRuleCondition[] = [], fromContains = "", subjectContains = "") {
  const items = conditions.length > 0 ? conditions : [fromContains ? { field: "from", operator: "contains", value: fromContains } as MailRuleCondition : undefined, subjectContains ? { field: "subject", operator: "contains", value: subjectContains } as MailRuleCondition : undefined].filter(Boolean) as MailRuleCondition[]
  return items.map(ruleConditionItemSummary).join("；") || "无条件"
}

function ruleConditionItemSummary(item: MailRuleCondition): string {
  if (item.conditions?.length) {
    const mode = item.matchMode === "any" ? "任一" : "全部"
    return `${mode}(${item.conditions.map(ruleConditionItemSummary).join("；")})`
  }
  const field = item.field || "from"
  const operator = item.operator || defaultConditionOperator(field)
  const value = item.value ? `"${item.value}"` : ""
  return `${conditionFieldShortLabels[field]}${conditionOperatorLabels[operator]}${value}`
}

function ruleActionSummary(action: MailRuleAction) {
  if (action.type === "label") return `${ruleActionLabels[action.type]}${action.value ? `："${action.value}"` : ""}`
  if (action.type === "move") return `${ruleActionLabels[action.type]}"${folderLabel(action.value || "Archive")}"`
  if (action.type === "forward") return `${ruleActionLabels[action.type]}${action.value ? `：${action.value}` : ""}`
  return ruleActionLabels[action.type]
}

function BlockedSection({ items, mailboxes, mailboxId, spamCount, onMailboxChange, onCreate, onDelete, pending }: { items: any[]; mailboxes: Mailbox[]; mailboxId: string; spamCount: number; onMailboxChange: (value: string) => void; onCreate: (form: FormData) => void; onDelete: (id: string) => void; pending: boolean }) {
  const [pendingConfirm, setPendingConfirm] = React.useState<PendingConfirm | null>(null)
  const [dialogOpen, setDialogOpen] = React.useState(false)
  function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    onCreate(new FormData(event.currentTarget))
    event.currentTarget.reset()
    setDialogOpen(false)
  }
  return (
    <div>
      <SettingsCard
        title="拦截规则"
        subtitle={`共 ${items.length} 条发件人规则 · 垃圾邮件 ${spamCount} 封`}
        action={<Button type="button" onClick={() => setDialogOpen(true)}>新增拦截</Button>}
        contentClassName="space-y-2"
      >
        {items.map((item) => (
          <div key={item.id} className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-3 rounded-lg border bg-background p-3 transition-colors hover:bg-muted/40">
            <div className="min-w-0">
              <div className="break-all text-sm font-semibold leading-5 text-foreground">{item.email}</div>
              <div className="mt-1 break-words text-xs leading-5 text-muted-foreground">{item.mailboxId ? mailboxes.find((m) => m.id === item.mailboxId)?.address : "全部邮箱"}{item.reason ? ` · ${item.reason}` : ""}</div>
            </div>
            <Button variant="ghost" size="icon" className="size-8 shrink-0 text-destructive hover:bg-destructive/10 hover:text-destructive" aria-label={`移除拦截规则 ${item.email}`} title="移除拦截规则" onClick={() => setPendingConfirm({ title: "移除拦截规则？", description: `${item.email} 之后将不再被此规则拦截。`, confirmText: "移除规则", onConfirm: () => { onDelete(item.id); setPendingConfirm(null) } })}><Trash2 className="h-4 w-4" /></Button>
          </div>
        ))}
        {items.length === 0 && <EmptyState icon={<ShieldCheck />} text="没有被拦截的邮件" description="当前没有发件人拦截规则，所有邮件都会按正常规则投递。" />}
      </SettingsCard>
      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="w-[min(92vw,30rem)] max-w-none">
          <DialogHeader><DialogTitle>新增拦截发件人</DialogTitle></DialogHeader>
          <form className="space-y-4" onSubmit={submit}>
            <Field label="适用邮箱"><MailboxSelect value={mailboxId} mailboxes={mailboxes} onChange={onMailboxChange} /></Field>
            <Field label="发件人邮箱"><Input name="email" type="email" required placeholder="sender@example.com" /></Field>
            <Field label="原因"><Input name="reason" placeholder="可选，例如：广告、骚扰邮件" /></Field>
            <DialogFooter className="gap-2 [&>button]:w-full sm:[&>button]:w-auto">
              <Button type="button" variant="outline" onClick={() => setDialogOpen(false)}>取消</Button>
              <Button disabled={pending}>{pending ? "保存中..." : "加入拦截"}</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
      <ConfirmDialog open={!!pendingConfirm} title={pendingConfirm?.title || ""} description={pendingConfirm?.description} confirmText={pendingConfirm?.confirmText || "移除"} destructive onOpenChange={(open) => { if (!open) setPendingConfirm(null) }} onConfirm={() => pendingConfirm?.onConfirm()} />
    </div>
  )
}

function StatsSection({ stats }: { stats?: MailStats }) {
  const quotaLabel = stats?.quotaBytes ? `${formatBytes(stats.storageBytes || 0)} / ${formatBytes(stats.quotaBytes)}` : formatBytes(stats?.storageBytes || 0)
  const quotaPct = Math.min(stats?.quotaUsedPct || 0, 100)
  const primaryCards = [
    { label: "总收件", value: stats?.totalIncoming || 0, icon: <Mail className="h-4 w-4" />, tone: "bg-muted text-foreground" },
    { label: "总发件", value: stats?.totalOutgoing || 0, icon: <SendHorizontal className="h-4 w-4" />, tone: "bg-emerald-50 text-emerald-600" },
    { label: "未读邮件", value: stats?.unreadMessages || 0, icon: <MailCheck className="h-4 w-4" />, tone: "bg-amber-50 text-amber-600" },
    { label: "存储用量", value: formatBytes(stats?.storageBytes || 0), subvalue: stats?.quotaBytes ? `/ ${formatBytes(stats.quotaBytes)} (${quotaPct.toFixed(0)}%)` : "不限", icon: <HardDrive className="h-4 w-4" />, tone: "bg-violet-50 text-violet-600" },
  ]
  const secondaryStats = [
    { label: "今日发件", value: stats?.todayOutgoing || 0 },
    { label: "草稿", value: stats?.draftMessages || 0 },
    { label: "发送失败", value: stats?.failedSends || 0 },
    { label: "平均邮件大小", value: formatBytes(stats?.averageMessageBytes || 0) },
  ]
  return (
    <div className="space-y-6">
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {primaryCards.map((card) => (
          <div key={card.label} className="grid h-[106px] grid-cols-[2.5rem_minmax(0,1fr)] items-center gap-3 rounded-lg border bg-card px-5">
            <div className={cn("flex size-10 shrink-0 items-center justify-center rounded-lg", card.tone)}>{card.icon}</div>
            <div className="min-w-0">
              <div className="truncate text-sm text-muted-foreground">{card.label}</div>
              <div className="truncate text-2xl font-semibold leading-8 text-foreground">{card.value}</div>
              {"subvalue" in card && <div className="truncate text-xs leading-4 text-muted-foreground">{card.subvalue}</div>}
            </div>
          </div>
        ))}
      </div>
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {secondaryStats.map((item) => (
          <div key={item.label} className="flex h-[54px] items-center justify-between gap-3 rounded-lg border bg-background px-4 text-sm">
            <div className="truncate text-sm text-muted-foreground">{item.label}</div>
            <div className="shrink-0 font-semibold text-foreground">{item.value}</div>
          </div>
        ))}
      </div>
      <div className="grid gap-6 lg:grid-cols-[2fr_1fr]">
        <StatsPanel title="收发趋势" className="h-[321px]">
          <StatsTrendChart points={stats?.trend || []} />
        </StatsPanel>
        <StatsPanel title="邮件分布" className="h-[321px]">
          <StatsDistribution items={stats?.distribution || []} />
        </StatsPanel>
      </div>
      <div className="grid gap-6 lg:grid-cols-2">
        <StatsPanel title="存储用量" className="h-[338px]">
          <StatsStorage quotaLabel={quotaLabel} quotaPct={quotaPct} hasQuota={!!stats?.quotaBytes} />
        </StatsPanel>
        <StatsPanel title="常用联系人" className="h-[338px]">
          <StatsContacts contacts={stats?.topContacts || []} />
        </StatsPanel>
      </div>
    </div>
  )
}

function StatsPanel({ title, children, className }: { title: string; children: React.ReactNode; className?: string }) {
  return (
    <section className={cn("overflow-hidden rounded-lg border bg-card", className)}>
      <div className="px-5 pb-2 pt-4">
        <h2 className="text-base font-semibold leading-6 text-foreground">{title}</h2>
      </div>
      <div className="px-5 pb-5">{children}</div>
    </section>
  )
}

function StatsTrendChart({ points }: { points: MailStats["trend"] }) {
  const data = points.length ? points : [{ date: "", incoming: 0, outgoing: 0 }]
  const maxValue = Math.max(...data.flatMap((item) => [item.incoming, item.outgoing]), 1)
  const width = 520
  const height = 148
  const padding = { top: 12, right: 10, bottom: 22, left: 32 }
  const plotWidth = width - padding.left - padding.right
  const plotHeight = height - padding.top - padding.bottom
  const xFor = (index: number) => padding.left + (data.length === 1 ? plotWidth / 2 : (index / (data.length - 1)) * plotWidth)
  const yFor = (value: number) => padding.top + plotHeight - (value / maxValue) * plotHeight
  const pathFor = (key: "incoming" | "outgoing") => data.map((item, index) => `${index === 0 ? "M" : "L"} ${xFor(index).toFixed(1)} ${yFor(item[key]).toFixed(1)}`).join(" ")
  const ticks = trendTicks(data)
  return (
    <div className="h-[250px] rounded-md bg-background">
      <div className="flex items-center justify-end gap-4 text-xs text-muted-foreground">
        <span className="inline-flex items-center gap-1.5"><span className="size-2 rounded-full bg-foreground" />收件</span>
        <span className="inline-flex items-center gap-1.5"><span className="size-2 rounded-full bg-emerald-500" />发件</span>
      </div>
      <svg viewBox={`0 0 ${width} ${height}`} className="h-[226px] w-full overflow-visible" role="img" aria-label="邮件收发趋势">
        {[0, 0.25, 0.5, 0.75, 1].map((step) => {
          const y = padding.top + plotHeight * step
          return <line key={step} x1={padding.left} x2={width - padding.right} y1={y} y2={y} className="stroke-border" strokeDasharray={step === 1 ? undefined : "3 5"} />
        })}
        <path d={pathFor("incoming")} fill="none" className="stroke-foreground" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" />
        <path d={pathFor("outgoing")} fill="none" className="stroke-emerald-500" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" />
        {data.map((item, index) => (
          <g key={`${item.date}-${index}`}>
            <circle cx={xFor(index)} cy={yFor(item.incoming)} r="2.8" className="fill-foreground" />
            <circle cx={xFor(index)} cy={yFor(item.outgoing)} r="2.8" className="fill-emerald-500" />
          </g>
        ))}
        <text x="0" y={padding.top + 4} className="fill-muted-foreground text-[11px]">{maxValue}</text>
        <text x="0" y={padding.top + plotHeight + 4} className="fill-muted-foreground text-[11px]">0</text>
        {ticks.map((tick) => (
          <text key={`${tick.index}-${tick.label}`} x={xFor(tick.index)} y={height - 6} textAnchor="middle" className="fill-muted-foreground text-[11px]">{tick.label}</text>
        ))}
      </svg>
    </div>
  )
}

function trendTicks(data: MailStats["trend"]) {
  if (data.length === 0) return []
  const count = Math.min(5, data.length)
  const seen = new Set<number>()
  const ticks: { index: number; label: string }[] = []
  for (let i = 0; i < count; i++) {
    const index = count === 1 ? 0 : Math.round((i / (count - 1)) * (data.length - 1))
    if (seen.has(index)) continue
    seen.add(index)
    ticks.push({ index, label: formatStatsDate(data[index]?.date || "") })
  }
  return ticks
}

function formatStatsDate(value: string) {
  if (!value) return ""
  const [, month, day] = value.split("-")
  return month && day ? `${month}-${day}` : value
}

function StatsDistribution({ items }: { items: MailStats["distribution"] }) {
  const rows = items.length ? items : [
    { key: "inbox", label: "收件箱", count: 0 },
    { key: "archive", label: "已归档", count: 0 },
    { key: "spam", label: "垃圾邮件", count: 0 },
    { key: "trash", label: "已删除", count: 0 },
    { key: "attachments", label: "有附件", count: 0 },
    { key: "starred", label: "已加旗标", count: 0 },
  ]
  const maxCount = Math.max(...rows.map((row) => row.count), 1)
  return (
    <div className="space-y-3 py-3">
      {rows.map((row) => (
        <div key={row.key} className="grid grid-cols-[4.5rem_minmax(0,1fr)_2rem] items-center gap-2 text-xs">
          <div className="truncate font-medium text-muted-foreground">{row.label}</div>
          <div className="h-2.5 overflow-hidden rounded-full bg-muted">
            <div className={cn("h-full rounded-full", distributionBarTone(row.key))} style={{ width: `${Math.max(row.count > 0 ? 6 : 0, Math.round((row.count / maxCount) * 100))}%` }} />
          </div>
          <div className="text-right font-semibold text-foreground">{row.count}</div>
        </div>
      ))}
    </div>
  )
}

function distributionBarTone(key: string) {
  if (key === "archive") return "bg-sky-500"
  if (key === "spam") return "bg-amber-400"
  if (key === "trash") return "bg-red-500"
  if (key === "attachments") return "bg-violet-500"
  if (key === "starred") return "bg-slate-600"
  return "bg-slate-900"
}

function StatsStorage({ quotaLabel, quotaPct, hasQuota }: { quotaLabel: string; quotaPct: number; hasQuota: boolean }) {
  return (
    <div className="h-[260px] pt-3">
      <div className="mb-2 flex items-end justify-between gap-3">
        <div className="text-sm font-semibold text-foreground">{quotaLabel}</div>
        <div className="text-xs font-semibold text-foreground">{hasQuota ? `${quotaPct.toFixed(0)}%` : "不限"}</div>
      </div>
      <div className="h-2 overflow-hidden rounded-full bg-muted">
        <div className="h-full rounded-full bg-slate-900 transition-all" style={{ width: `${hasQuota ? quotaPct : 12}%` }} />
      </div>
      <p className="mt-3 text-xs text-muted-foreground">{quotaPct >= 90 ? "存储容量接近上限，请及时清理。" : "存储容量使用正常。"}</p>
    </div>
  )
}

function StatsContacts({ contacts }: { contacts: MailStats["topContacts"] }) {
  if (contacts.length === 0) return <EmptyState icon={<Users />} text="暂无常用联系人" description="有邮件往来后会显示联系人排行" className="h-[260px] min-h-0 py-4" />
  return (
    <div className="h-[260px] space-y-1 overflow-hidden pt-1">
      {contacts.slice(0, 10).map((item, index) => (
        <div key={item.email} className="grid grid-cols-[1.25rem_minmax(0,1fr)_3.25rem] items-center gap-2 text-sm leading-6">
          <div className="text-center font-semibold text-muted-foreground">{index + 1}</div>
          <div className="min-w-0 truncate font-medium text-foreground">{item.email}</div>
          <div className="text-right font-semibold text-muted-foreground">{item.count} 封</div>
        </div>
      ))}
    </div>
  )
}

function StatsSummary({ stats }: { stats?: MailStats }) {
  const quotaLabel = stats?.quotaBytes ? `${formatBytes(stats.storageBytes || 0)} / ${formatBytes(stats.quotaBytes)}` : formatBytes(stats?.storageBytes || 0)
  const cards = [
    { label: "总邮件", value: stats?.totalMessages || 0, icon: <Mail className="h-4 w-4" />, tone: "bg-zinc-100 text-zinc-700" },
    { label: "未读", value: stats?.unreadMessages || 0, icon: <MailCheck className="h-4 w-4" />, tone: "bg-emerald-50 text-emerald-600" },
    { label: "星标", value: stats?.starredMessages || 0, icon: <ShieldCheck className="h-4 w-4" />, tone: "bg-amber-50 text-amber-600" },
    { label: "附件", value: `${stats?.attachmentCount || 0} / ${formatBytes(stats?.attachmentBytes || 0)}`, icon: <Image className="h-4 w-4" />, tone: "bg-violet-50 text-violet-600" },
    { label: stats?.quotaBytes ? `容量 ${Math.min(stats.quotaUsedPct || 0, 999).toFixed(1)}%` : "容量", value: quotaLabel, icon: <BarChart3 className="h-4 w-4" />, tone: "bg-slate-100 text-slate-700" },
  ]
  return (
    <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5">
      {cards.map((card) => (
        <Card key={card.label} className="min-w-0 overflow-hidden shadow-[0_1px_2px_rgba(15,23,42,0.04)]">
          <CardContent className="flex min-w-0 items-center gap-3 p-4">
            <div className={cn("flex size-9 shrink-0 items-center justify-center rounded-lg", card.tone)}>{card.icon}</div>
            <div className="min-w-0">
              <div className="break-words text-lg font-semibold leading-6 text-foreground [overflow-wrap:anywhere]">{card.value}</div>
              <div className="mt-0.5 text-xs text-muted-foreground">{card.label}</div>
            </div>
          </CardContent>
        </Card>
      ))}
    </div>
  )
}

function CleanupButton({ icon, title, disabled, onClick }: { icon: React.ReactNode; title: string; disabled: boolean; onClick: () => void }) { return <Button variant="outline" className="min-h-[72px] w-full min-w-0 justify-start whitespace-normal px-4 py-3 text-left" disabled={disabled} onClick={onClick}><div className="mr-3 shrink-0 rounded-md bg-muted p-2">{icon}</div><div className="min-w-0 break-words font-medium leading-5">{title}</div></Button> }
function MailboxSelect({ value, mailboxes, onChange }: { value: string; mailboxes: Mailbox[]; onChange: (value: string) => void }) { return <Select value={value} onValueChange={onChange}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent><SelectItem value="all">全部邮箱</SelectItem>{mailboxes.map((m) => <SelectItem key={m.id} value={m.id}>{m.address}</SelectItem>)}</SelectContent></Select> }
function Field({ label, children }: { label: string; children: React.ReactNode }) { return <div className="space-y-2"><Label>{label}</Label>{children}</div> }
function EmptyState({ text, description, icon, action, className }: { text: string; description?: string; icon?: React.ReactNode; action?: React.ReactNode; className?: string }) {
  return (
    <div className={cn("grid min-h-[132px] place-items-center rounded-lg border border-dashed bg-background/60 px-6 py-8 text-center", className)}>
      <div className="flex max-w-sm flex-col items-center">
        {icon && <div className="mb-3 text-muted-foreground/70 [&>svg]:h-9 [&>svg]:w-9 [&>svg]:stroke-[1.5]">{icon}</div>}
        <div className="text-sm font-medium text-muted-foreground">{text}</div>
        {description && <div className="mt-1 text-xs leading-5 text-muted-foreground">{description}</div>}
        {action && <div className="mt-4">{action}</div>}
      </div>
    </div>
  )
}
function folderLabel(folder: string) { return ({ Inbox: "收件箱", Sent: "已发送", Drafts: "草稿箱", Archive: "归档", Spam: "垃圾邮件", Trash: "回收站" } as Record<string, string>)[folder] || folder }
function clientServerHost(hostname?: string, address?: string) { const value = (hostname || "").trim(); if (value) return value; const domain = (address || "").split("@")[1]; return domain ? `mail.${domain}` : "mail.example.com" }
function AccountHeader({ name, email, darkMode, onToggleTheme, onBack }: { name: string; email?: string; darkMode: boolean; onToggleTheme: () => void; onBack: () => void }) {
  const displayName = cleanAccountName(name, email)
  return (
    <div className="flex h-full items-center justify-between gap-3 px-4">
      <div className="flex min-w-0 items-center gap-3">
        <Avatar className="size-[32px] rounded-full">
          <AvatarFallback className="bg-primary text-xs font-semibold text-primary-foreground">{accountInitial(displayName, email)}</AvatarFallback>
        </Avatar>
        <div className="min-w-0 truncate text-sm font-semibold leading-5">{displayName}</div>
      </div>
      <div className="flex shrink-0 items-center gap-2">
        <Button type="button" variant="ghost" size="icon" className="size-[28px] rounded-md text-muted-foreground" aria-label={darkMode ? "切换浅色模式" : "切换深色模式"} title={darkMode ? "浅色模式" : "深色模式"} onClick={onToggleTheme}>
          {darkMode ? <Sun className="h-4 w-4 text-amber-500" /> : <Moon className="h-4 w-4" />}
        </Button>
        <Button type="button" variant="ghost" size="icon" className="size-[28px] rounded-md text-muted-foreground" aria-label="返回邮箱" title="返回邮箱" onClick={onBack}>
          <ArrowLeft className="h-4 w-4" />
        </Button>
      </div>
    </div>
  )
}
function cleanAccountName(name: string, email?: string) { const value = name.trim(); if (!value || (email && value.toLowerCase() === email.toLowerCase())) return email?.split("@")[0] || "用户"; return value }
function accountInitial(name: string, email?: string) { const source = cleanAccountName(name, email); const first = Array.from(source.trim())[0]; return (first || "蓝").toUpperCase() }
