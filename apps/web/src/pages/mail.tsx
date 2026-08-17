import * as React from "react"
import DOMPurify from "dompurify"
import { type InfiniteData, useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Node, mergeAttributes, type Editor } from "@tiptap/core"
import { DOMParser as ProseMirrorDOMParser } from "@tiptap/pm/model"
import { EditorContent, useEditor } from "@tiptap/react"
import StarterKit from "@tiptap/starter-kit"
import LinkExtension from "@tiptap/extension-link"
import ImageExtension from "@tiptap/extension-image"
import TextAlign from "@tiptap/extension-text-align"
import Placeholder from "@tiptap/extension-placeholder"
import { BackgroundColor, Color, FontFamily, FontSize, TextStyle } from "@tiptap/extension-text-style"
import { useNavigate } from "react-router-dom"
import { AlignCenter, AlignLeft, AlignRight, Archive, ArrowLeft, Ban, Bell, Bold, Bot, Briefcase, Calendar, Check, ChevronDown, Clock3, Code2, Copy, Download, Ellipsis, Eraser, Eye, FileText, Folder, Forward, GraduationCap, Heart, Highlighter, History, Image, Inbox, IndentDecrease, IndentIncrease, Italic, Link, List, ListOrdered, Mail, Mailbox as MailboxIcon, MailCheck, MailQuestion, Moon, PanelLeftOpen, Paperclip, PencilLine, Plane, Plus, Quote, Receipt, Redo2, RefreshCcw, Reply, RotateCcw, Search, Send, Settings, ShieldCheck, ShoppingBag, Signature, SlidersHorizontal, Smile, Sparkles, Star, Strikethrough, Sun, Tag, Trash2, Type, Underline, Undo2, Upload, Users, X } from "lucide-react"
import { api, ExternalImapAccount, ListResponse, Mailbox, MailFolder, MailLabel, MailMessage, MailSearchParams, SendPayload, DraftPayload, ScheduledSend, SendQueueItem, SendQueueAuditEvent, SendQueueStatus, PermissionLimits } from "@/lib/api"
import { cn, decodeMimeHeader, formatBytes, formatDate, formatDateTime, generateLabelColor } from "@/lib/utils"
import { applyTheme, getInitialTheme } from "@/lib/theme"
import { useDisplayMode } from "@/lib/display-mode"
import { Language, languageOptions, useLanguage } from "@/lib/language"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Sheet, SheetContent, SheetTitle, SheetTrigger } from "@/components/ui/sheet"
import { Label } from "@/components/ui/label"
import { DropdownMenu, DropdownMenuCheckboxItem, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Separator } from "@/components/ui/separator"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { ConfirmDialog } from "@/components/confirm-dialog"
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
} from "@/components/ui/sidebar"
import { useMe } from "@/hooks/use-me"
import { useIsMobile } from "@/hooks/use-mobile"
import { useToast } from "@/hooks/use-toast"
import { hasPermission } from "@/lib/permissions"

const folderIcons: Record<string, React.ReactNode> = { inbox: <Inbox className="h-4 w-4" />, sent: <Send className="h-4 w-4" />, drafts: <FileText className="h-4 w-4" />, archive: <Archive className="h-4 w-4" />, spam: <Ban className="h-4 w-4" />, trash: <Trash2 className="h-4 w-4" /> }
function NetflixFolderIcon({ className }: { className?: string }) {
  return <span aria-hidden="true" className={cn("inline-flex items-center justify-center font-black text-red-600", className)}>N</span>
}
const customFolderIconOptions = [
  { key: "folder", label: "文件夹", icon: Folder },
  { key: "mail", label: "邮件", icon: Mail },
  { key: "briefcase", label: "工作", icon: Briefcase },
  { key: "users", label: "联系人", icon: Users },
  { key: "receipt", label: "账单", icon: Receipt },
  { key: "shopping", label: "购物", icon: ShoppingBag },
  { key: "plane", label: "旅行", icon: Plane },
  { key: "graduation", label: "学习", icon: GraduationCap },
  { key: "heart", label: "收藏", icon: Heart },
  { key: "star", label: "重要", icon: Star },
  { key: "bell", label: "提醒", icon: Bell },
  { key: "shield", label: "安全", icon: ShieldCheck },
  { key: "tag", label: "分类", icon: Tag },
  { key: "netflix", label: "Netflix", icon: NetflixFolderIcon },
  { key: "chatgpt", label: "ChatGPT", icon: Bot },
] as const
function suggestedFolderIcon(name: string) {
  const value = name.trim().toLocaleLowerCase()
  if (/netflix|奈飞|网飞/.test(value)) return "netflix"
  if (/chatgpt|openai|\bgpt\b/.test(value)) return "chatgpt"
  if (/账单|发票|收据|bill|invoice|receipt/.test(value)) return "receipt"
  if (/购物|订单|快递|shop|order|delivery/.test(value)) return "shopping"
  if (/旅行|旅游|机票|酒店|travel|trip|flight|hotel/.test(value)) return "plane"
  if (/学习|教育|课程|学校|study|school|course/.test(value)) return "graduation"
  if (/联系人|团队|用户|contact|team|people/.test(value)) return "users"
  if (/工作|项目|客户|work|project|business|client/.test(value)) return "briefcase"
  if (/收藏|喜欢|favorite|favourite/.test(value)) return "heart"
  if (/重要|紧急|important|urgent/.test(value)) return "star"
  if (/安全|验证|密码|登录|security|verify|password|login/.test(value)) return "shield"
  if (/提醒|通知|remind|notification/.test(value)) return "bell"
  if (/邮件|邮箱|mail|email/.test(value)) return "mail"
  return "folder"
}

async function prepareFolderIcon(file: File) {
  if (!/^image\/(png|jpe?g|webp)$/i.test(file.type)) throw new Error("仅支持 PNG、JPG 或 WebP 图片")
  if (file.size > 2 * 1024 * 1024) throw new Error("原图不能超过 2 MB")
  const bitmap = await createImageBitmap(file)
  try {
    const canvas = document.createElement("canvas")
    canvas.width = 64
    canvas.height = 64
    const context = canvas.getContext("2d")
    if (!context) throw new Error("无法处理该图片")
    const scale = Math.min(64 / bitmap.width, 64 / bitmap.height)
    const width = Math.max(1, Math.round(bitmap.width * scale))
    const height = Math.max(1, Math.round(bitmap.height * scale))
    context.drawImage(bitmap, Math.round((64 - width) / 2), Math.round((64 - height) / 2), width, height)
    const result = canvas.toDataURL("image/png")
    if (result.length > 44_000) throw new Error("处理后的图标过大")
    return result
  } finally {
    bitmap.close()
  }
}
const folderLabels: Record<string, string> = {
  Inbox: "收件箱",
  Sent: "已发送",
  Drafts: "草稿箱",
  Archive: "已归档",
  Spam: "垃圾邮件",
  Trash: "已删除",
}

type ComposeDraft = { key: string; id?: string; mailboxId?: string; to?: string; cc?: string; bcc?: string; subject?: string; text?: string; html?: string; files?: File[]; isDraft?: boolean }
type MailFilter = "all" | "unread" | "starred" | "attachments" | "recent7"
type MailView = "folder" | "starred" | "label" | "scheduled" | "sendQueue" | "external" | "unknown"
type MailListResponse = { items?: MailMessage[]; nextCursor?: string }
type PendingConfirm = { title: string; description?: string; confirmText: string; onConfirm: () => void }
type MailNotificationState = { latestId: string; latestReceivedAt: string }
type ComposeSendIntent = { title: string; description: string; confirmText: string; onConfirm: () => void }
type MessageContextMenuState = { message: MailMessage; x: number; y: number }
type SidebarContextMenuState = { item: MailMenuItem; x: number; y: number }
type FolderDropTarget = { key: string; edge: "before" | "after" | "end" }
type AdvancedMailSearch = { from: string; to: string; subject: string; startDate: string; endDate: string; hasAttachments: boolean; unread: boolean; starred: boolean }
type AdvancedMailSearchDraft = AdvancedMailSearch
type AdvancedSearchChip = { key: keyof AdvancedMailSearch; label: string }
type MailMenuItem =
  | { type: "starred"; key: string; label: string; icon: React.ReactNode; count: number; order: number }
  | { type: "scheduled"; key: string; label: string; icon: React.ReactNode; count: number; order: number }
  | { type: "sendQueue"; key: string; label: string; icon: React.ReactNode; count: number; order: number }
  | { type: "unknown"; key: string; label: string; icon: React.ReactNode; count: number; order: number }
  | { type: "folder"; key: string; folderId: string; folderName: string; label: string; icon: React.ReactNode; count: number; custom: boolean; order: number }

const filterLabels: Record<MailFilter, string> = {
  all: "全部",
  unread: "未读",
  starred: "已加旗标",
  attachments: "有附件",
  recent7: "最近 7 天",
}

const emptyAdvancedSearch: AdvancedMailSearch = { from: "", to: "", subject: "", startDate: "", endDate: "", hasAttachments: false, unread: false, starred: false }
const emptyAdvancedSearchDraft: AdvancedMailSearchDraft = { ...emptyAdvancedSearch }
const mailImportBatchBytes = 32 * 1024 * 1024
const mailImportBatchFiles = 20
const mailCompactBreakpoint = 768
const mailDetailBreakpoint = 768

function buildMailImportBatches(files: File[]) {
  const batches: File[][] = []
  let batch: File[] = []
  let batchBytes = 0
  for (const file of files) {
    if (batch.length > 0 && (batch.length >= mailImportBatchFiles || batchBytes + file.size > mailImportBatchBytes)) {
      batches.push(batch)
      batch = []
      batchBytes = 0
    }
    batch.push(file)
    batchBytes += file.size
  }
  if (batch.length > 0) batches.push(batch)
  return batches
}

function useMaxViewportWidth(maxWidth: number) {
  const [matches, setMatches] = React.useState(false)
  React.useEffect(() => {
    const media = window.matchMedia(`(max-width: ${maxWidth - 1}px)`)
    const sync = () => setMatches(media.matches)
    media.addEventListener("change", sync)
    sync()
    return () => media.removeEventListener("change", sync)
  }, [maxWidth])
  return matches
}

export function MailPage() {
  const qc = useQueryClient()
  const { toast } = useToast()
  const navigate = useNavigate()
  const me = useMe()
  const [folder, setFolder] = React.useState("Inbox")
  const [mailView, setMailView] = React.useState<MailView>("folder")
  const [selectedLabelId, setSelectedLabelId] = React.useState("")
  const [query, setQuery] = React.useState("")
  const [advancedSearchOpen, setAdvancedSearchOpen] = React.useState(false)
  const [advancedSearch, setAdvancedSearch] = React.useState<AdvancedMailSearch>(emptyAdvancedSearch)
  const [advancedSearchDraft, setAdvancedSearchDraft] = React.useState<AdvancedMailSearchDraft>(emptyAdvancedSearchDraft)
  const [selectedId, setSelectedId] = React.useState<string | null>(null)
  const [compactSelectedIds, setCompactSelectedIds] = React.useState<string[]>([])
  const [composeOpen, setComposeOpen] = React.useState(false)
  const [composeDraft, setComposeDraft] = React.useState<ComposeDraft | undefined>()
  const sidebarCollapsed = false
  const [mailFilter, setMailFilter] = React.useState<MailFilter>("all")
  const [selectedMailboxId, setSelectedMailboxId] = React.useState("all")
  const [selectedExternalAccountId, setSelectedExternalAccountId] = React.useState("")
  const [expandedExternalAccountIds, setExpandedExternalAccountIds] = React.useState<string[]>([])
  const [foldersExpanded, setFoldersExpanded] = React.useState(true)
  const [labelsExpanded, setLabelsExpanded] = React.useState(true)
  const [externalFolder, setExternalFolder] = React.useState("INBOX")
  const [darkMode, setDarkMode] = React.useState(getInitialTheme)
  const [language, setLanguage] = useLanguage()
  const [displayMode] = useDisplayMode()
  const isMobile = useIsMobile()
  const isNarrowMailViewport = useMaxViewportWidth(mailCompactBreakpoint)
  const isTwoPaneMailViewport = useMaxViewportWidth(mailDetailBreakpoint)
  const compactMailLayout = isMobile || isNarrowMailViewport || displayMode === "compact"
  const [refreshing, setRefreshing] = React.useState(false)
  const [autoRefreshing, setAutoRefreshing] = React.useState(false)
  const [exportingMail, setExportingMail] = React.useState(false)
  const [importingMail, setImportingMail] = React.useState(false)
  const [bulkPending, setBulkPending] = React.useState(false)
  const [pendingConfirm, setPendingConfirm] = React.useState<PendingConfirm | null>(null)
  const [cancelingScheduledId, setCancelingScheduledId] = React.useState("")
  const [sendQueueStatus, setSendQueueStatus] = React.useState<SendQueueStatus | "all">("all")
  const [sendQueueMessageId, setSendQueueMessageId] = React.useState("")
  const [sendQueueRecipient, setSendQueueRecipient] = React.useState("")
  const [sendQueueFrom, setSendQueueFrom] = React.useState("")
  const [sendQueueTo, setSendQueueTo] = React.useState("")
  const [sendQueueAuditId, setSendQueueAuditId] = React.useState("")
  const [sendQueuePendingId, setSendQueuePendingId] = React.useState("")
  const [mobileSidebarOpen, setMobileSidebarOpen] = React.useState(false)
  const [labelEditMode, setLabelEditMode] = React.useState(false)
  const [newLabelEditing, setNewLabelEditing] = React.useState(false)
  const [messageContextMenu, setMessageContextMenu] = React.useState<MessageContextMenuState | null>(null)
  const [sidebarContextMenu, setSidebarContextMenu] = React.useState<SidebarContextMenuState | null>(null)
  const [folderDialogOpen, setFolderDialogOpen] = React.useState(false)
  const [draggingFolderId, setDraggingFolderId] = React.useState("")
  const [folderDropTarget, setFolderDropTarget] = React.useState<FolderDropTarget | null>(null)
  const themeMountedRef = React.useRef(false)
  const mailNotifyStateRef = React.useRef<Record<string, MailNotificationState>>({})
  const mailAudioContextRef = React.useRef<AudioContext | null>(null)
  const mailImportInputRef = React.useRef<HTMLInputElement | null>(null)
  const user = me.data?.user
  const canAccessMail = hasPermission(user, "mail.access")
  const canReadMail = hasPermission(user, "mail.messages.read")
  const canSendMail = hasPermission(user, "mail.messages.send")
  const canManageDrafts = hasPermission(user, "mail.messages.drafts")
  const canScheduleMail = hasPermission(user, "mail.messages.schedule")
  const canOrganizeMail = hasPermission(user, "mail.messages.organize")
  const canManageLabels = hasPermission(user, "mail.labels.manage")
  const canDownloadAttachments = hasPermission(user, "mail.attachments.download")
  const canManageSignatures = hasPermission(user, "mail.signatures.manage")
  const canViewUnknownMail = user?.role === "admin"
  const publicSettings = useQuery({ queryKey: ["public-settings"], queryFn: api.publicSettings })
  const externalImapEnabled = publicSettings.data?.externalImapEnabled ?? false

  React.useEffect(() => {
    if (!user) return
    void import("@/pages/profile")
  }, [user])

  const mailboxList = useQuery({ queryKey: ["mailboxes", "mine"], queryFn: api.myMailboxes, enabled: canAccessMail })
  const externalMailAccounts = useQuery({ queryKey: ["mail-external-accounts"], queryFn: api.externalMailAccounts, enabled: canAccessMail && canReadMail && externalImapEnabled })
  const selectedExternalAccount = React.useMemo(() => externalImapEnabled ? externalMailAccounts.data?.items.find((item) => item.id === selectedExternalAccountId) : undefined, [externalImapEnabled, externalMailAccounts.data?.items, selectedExternalAccountId])
  const externalFolders = useQuery({ queryKey: ["mail-external-folders", selectedExternalAccountId], queryFn: () => api.externalFolders(selectedExternalAccountId), enabled: !!selectedExternalAccountId && canReadMail && externalImapEnabled })
  const selectedMailbox = React.useMemo(() => {
    const items = mailboxList.data?.items || []
    if (selectedMailboxId === "all") return undefined
    return items.find((item) => item.id === selectedMailboxId) || items[0]
  }, [mailboxList.data?.items, selectedMailboxId])
  const isAllMailboxSelected = selectedMailboxId === "all"
  const activeMailboxId = selectedMailboxId === "all" ? "all" : selectedMailbox?.id || ""
  const composeMailboxes = React.useMemo(() => (mailboxList.data?.items || []).filter((item) => item.status === "active"), [mailboxList.data?.items])
  const accountMailbox = React.useMemo(() => {
    const loginEmail = user?.email.trim().toLowerCase()
    if (!loginEmail) return undefined
    return composeMailboxes.find((item) => item.address.trim().toLowerCase() === loginEmail)
  }, [composeMailboxes, user?.email])
  const selectedComposeMailbox = selectedMailbox?.status === "active" ? selectedMailbox : (isAllMailboxSelected ? accountMailbox || composeMailboxes[0] : undefined)
  const hasMailboxes = (mailboxList.data?.items.length || 0) > 0
  const canManageFolders = canOrganizeMail && hasMailboxes
  const showMailboxCopy = !!selectedMailbox && !isAllMailboxSelected
  const folders = useQuery({ queryKey: ["folders", activeMailboxId], queryFn: () => api.folders(activeMailboxId), enabled: !!activeMailboxId && canReadMail })
  const labels = useQuery({ queryKey: ["labels", activeMailboxId], queryFn: () => api.labels(activeMailboxId), enabled: !!activeMailboxId && (canReadMail || canManageLabels) })
  const mailStats = useQuery({ queryKey: ["mail-stats", activeMailboxId], queryFn: () => api.mailStats(activeMailboxId), enabled: !!activeMailboxId && hasPermission(user, "mail.stats.view") })
  const scheduledSends = useQuery({ queryKey: ["scheduled-sends", activeMailboxId], queryFn: () => api.scheduledSends(activeMailboxId), enabled: !!activeMailboxId && canScheduleMail, refetchInterval: 30000 })
  const canViewSendQueue = canReadMail
  const sendQueue = useQuery({
    queryKey: ["send-queue", activeMailboxId, sendQueueStatus, sendQueueMessageId, sendQueueRecipient, sendQueueFrom, sendQueueTo],
    queryFn: () => api.sendQueue({ mailboxId: activeMailboxId, status: sendQueueStatus, messageId: sendQueueMessageId.trim(), recipient: sendQueueRecipient.trim(), from: datetimeLocalToISO(sendQueueFrom), to: datetimeLocalToISO(sendQueueTo) }),
    enabled: !!activeMailboxId && canViewSendQueue,
    refetchInterval: 15000,
  })
  const sendQueueAudit = useQuery({ queryKey: ["send-queue-audit", sendQueueAuditId], queryFn: () => api.sendQueueAudit(sendQueueAuditId), enabled: !!sendQueueAuditId && canViewSendQueue })
  const mailRefreshInterval = publicSettings.data?.mailAutoRefresh ? Math.max(publicSettings.data.mailRefreshMs || 30000, 5000) : false
  const advancedSearchActive = hasAdvancedMailSearch(advancedSearch)
  const advancedSearchChips = React.useMemo(() => buildAdvancedSearchChips(advancedSearch), [advancedSearch])
  const mailSearchParams = React.useMemo<MailSearchParams>(() => ({
    q: query.trim(),
    ...advancedSearch,
  }), [advancedSearch, query])
  React.useEffect(() => {
    if (externalImapEnabled) return
    setSelectedExternalAccountId("")
    setExpandedExternalAccountIds([])
    if (mailView === "external") setMailView("folder")
  }, [externalImapEnabled, mailView])
  React.useEffect(() => {
    const ids = new Set((externalMailAccounts.data?.items || []).map((item) => item.id))
    setExpandedExternalAccountIds((current) => {
      const next = current.filter((id) => ids.has(id))
      return next.length === current.length ? current : next
    })
  }, [externalMailAccounts.data?.items])
  const inboxProbe = useQuery({
    queryKey: ["mail-notifications", activeMailboxId],
    queryFn: () => api.messages("Inbox", "", "", activeMailboxId),
    enabled: !!activeMailboxId && canReadMail,
    refetchInterval: mailRefreshInterval,
    refetchIntervalInBackground: true,
  })
  const messages = useInfiniteQuery({
    queryKey: ["messages", activeMailboxId, mailView, folder, selectedLabelId, mailSearchParams],
    queryFn: ({ pageParam }) => {
      const cursor = typeof pageParam === "string" ? pageParam : ""
      if (mailView === "starred") return api.starredMessages(mailSearchParams, cursor, activeMailboxId)
      if (mailView === "label") return api.labelMessages(selectedLabelId, mailSearchParams, cursor, activeMailboxId)
      return api.messages(folder, mailSearchParams, cursor, activeMailboxId)
    },
    initialPageParam: "",
    getNextPageParam: (lastPage) => lastPage.nextCursor || undefined,
    enabled: !!activeMailboxId && canReadMail && mailView !== "scheduled" && mailView !== "sendQueue" && mailView !== "unknown" && (mailView !== "label" || !!selectedLabelId),
  })
  const unknownMessages = useInfiniteQuery({
    queryKey: ["admin", "unknown-messages", query],
    queryFn: ({ pageParam }) => api.adminMessages({
      mailboxId: "unregistered",
      q: query.trim(),
      cursor: typeof pageParam === "string" ? pageParam : "",
    }),
    initialPageParam: "",
    getNextPageParam: (lastPage) => lastPage.nextCursor || undefined,
    enabled: canViewUnknownMail && mailView === "unknown",
  })
  const externalMessages = useInfiniteQuery({
    queryKey: ["external-messages", selectedExternalAccountId, externalFolder, query],
    queryFn: ({ pageParam }) => api.externalMessages(selectedExternalAccountId, externalFolder, typeof pageParam === "string" ? pageParam : "", query),
    initialPageParam: "",
    getNextPageParam: (lastPage) => lastPage.nextCursor || undefined,
    enabled: !!selectedExternalAccountId && canReadMail && mailView === "external" && externalImapEnabled,
  })
  const detail = useQuery({
    queryKey: ["message", selectedId, mailView, selectedExternalAccountId],
    queryFn: () => mailView === "external" ? api.externalMessage(selectedExternalAccountId, selectedId!) : mailView === "unknown" ? api.adminMessage(selectedId!) : api.message(selectedId!, { markRead: false }),
    enabled: !!selectedId && canReadMail && (mailView !== "external" || (!!selectedExternalAccountId && externalImapEnabled)) && (mailView !== "unknown" || canViewUnknownMail),
  })
  function updateCachedMessage(id: string, patch: Partial<MailMessage>) {
    qc.setQueryData(["message", id], (current: MailMessage | undefined) => current ? { ...current, ...patch } : current)
    qc.setQueriesData({ queryKey: ["messages"] }, (current: InfiniteData<MailListResponse> | undefined) => {
      if (!current?.pages) return current
      return {
        ...current,
        pages: current.pages.map((page) => ({
          ...page,
          items: (page.items || []).map((message) => message.id === id ? { ...message, ...patch } : message),
        })),
      }
    })
    qc.setQueriesData({ queryKey: ["external-messages"] }, (current: InfiniteData<MailListResponse> | undefined) => {
      if (!current?.pages) return current
      return {
        ...current,
        pages: current.pages.map((page) => ({
          ...page,
          items: (page.items || []).map((message) => message.id === id ? { ...message, ...patch } : message),
        })),
      }
    })
  }
  const star = useMutation({
    mutationFn: ({ id, starred }: { id: string; starred: boolean }) => api.star(id, starred),
    onMutate: ({ id, starred }) => updateCachedMessage(id, { isStarred: starred }),
    onSuccess: async (_, variables) => {
      await qc.invalidateQueries({ queryKey: ["messages"] })
      await qc.invalidateQueries({ queryKey: ["message", variables.id] })
      await qc.invalidateQueries({ queryKey: ["mail-stats"] })
      await qc.invalidateQueries({ queryKey: ["labels"] })
    },
    onError: (error) => toast({ title: "操作失败", description: error.message }),
  })
  const markRead = useMutation({
    mutationFn: ({ id, read }: { id: string; read: boolean }) => api.markRead(id, read),
    onMutate: ({ id, read }) => updateCachedMessage(id, { isRead: read }),
    onSuccess: async (_, variables) => {
      await qc.invalidateQueries({ queryKey: ["messages"] })
      await qc.invalidateQueries({ queryKey: ["message", variables.id] })
      await qc.invalidateQueries({ queryKey: ["folders"] })
      await qc.invalidateQueries({ queryKey: ["mailboxes"] })
      await qc.invalidateQueries({ queryKey: ["mail-stats"] })
    },
    onError: (error) => toast({ title: "操作失败", description: error.message }),
  })
  const markExternalRead = useMutation({
    mutationFn: ({ id, remoteId, read }: { id: string; remoteId: string; read: boolean }) => api.markExternalRead(id, remoteId, read),
    onMutate: ({ remoteId, read }) => updateCachedMessage(remoteId, { isRead: read }),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["external-messages"] })
      await qc.invalidateQueries({ queryKey: ["message"] })
      await qc.invalidateQueries({ queryKey: ["mail-external-folders"] })
    },
    onError: (error) => toast({ title: "操作失败", description: error.message }),
  })
  const addLabel = useMutation({
    mutationFn: ({ id, label }: { id: string; label: MailLabel }) => api.addLabel(id, { name: label.name, color: label.color }),
    onMutate: async ({ id, label }) => {
      await qc.cancelQueries({ queryKey: ["messages"] })
      if (selectedId) await qc.cancelQueries({ queryKey: ["message", selectedId] })
      const prevMessage = selectedId ? qc.getQueryData<MailMessage>(["message", selectedId]) : undefined
      if (selectedId) qc.setQueryData<MailMessage>(["message", selectedId], (current) => current ? { ...current, labels: [...(current.labels || []), label] } : current)
      qc.setQueriesData<InfiniteData<MailListResponse>>({ queryKey: ["messages"] }, (current) => current ? { ...current, pages: current.pages.map((page) => ({ ...page, items: (page.items || []).map((m) => m.id === id ? { ...m, labels: [...(m.labels || []), label] } : m) })) } : current)
      return { prevMessage }
    },
    onError: (_error, _vars, context) => {
      if (selectedId && context?.prevMessage) qc.setQueryData(["message", selectedId], context.prevMessage)
      qc.invalidateQueries({ queryKey: ["messages"] })
      toast({ title: "添加标签失败" })
    },
    onSettled: () => { qc.invalidateQueries({ queryKey: ["messages"] }); qc.invalidateQueries({ queryKey: ["labels"] }) },
  })
  const removeLabel = useMutation({
    mutationFn: ({ id, labelId }: { id: string; labelId: string }) => api.removeLabel(id, labelId),
    onMutate: async ({ id, labelId }) => {
      await qc.cancelQueries({ queryKey: ["messages"] })
      if (selectedId) await qc.cancelQueries({ queryKey: ["message", selectedId] })
      const prevMessage = selectedId ? qc.getQueryData<MailMessage>(["message", selectedId]) : undefined
      if (selectedId) qc.setQueryData<MailMessage>(["message", selectedId], (current) => current ? { ...current, labels: (current.labels || []).filter((l) => l.id !== labelId) } : current)
      qc.setQueriesData<InfiniteData<MailListResponse>>({ queryKey: ["messages"] }, (current) => current ? { ...current, pages: current.pages.map((page) => ({ ...page, items: (page.items || []).map((m) => m.id === id ? { ...m, labels: (m.labels || []).filter((l) => l.id !== labelId) } : m) })) } : current)
      return { prevMessage }
    },
    onError: (_error, _vars, context) => {
      if (selectedId && context?.prevMessage) qc.setQueryData(["message", selectedId], context.prevMessage)
      qc.invalidateQueries({ queryKey: ["messages"] })
      toast({ title: "移除标签失败" })
    },
    onSettled: () => { qc.invalidateQueries({ queryKey: ["messages"] }); qc.invalidateQueries({ queryKey: ["labels"] }) },
  })
  const createLabel = useMutation({
    mutationFn: (name: string) => api.createLabel({ mailboxId: activeMailboxId, name }),
    onMutate: async (name) => {
      await qc.cancelQueries({ queryKey: ["labels"] })
      const prevLabels = qc.getQueryData<ListResponse<MailLabel>>(["labels", activeMailboxId])
      const tempLabel: MailLabel = { id: `temp-${Date.now()}`, name, color: "" }
      qc.setQueryData<ListResponse<MailLabel>>(["labels", activeMailboxId], (current) => current ? { ...current, items: [...(current.items || []), tempLabel] } : { items: [tempLabel] })
      return { prevLabels }
    },
    onError: (_error, _name, context) => {
      if (context?.prevLabels) qc.setQueryData(["labels", activeMailboxId], context.prevLabels)
      toast({ title: "创建标签失败" })
    },
    onSettled: () => qc.invalidateQueries({ queryKey: ["labels"] }),
  })
  const deleteLabel = useMutation({
    mutationFn: (id: string) => api.deleteLabel(id, activeMailboxId),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: ["labels"] })
      await qc.cancelQueries({ queryKey: ["messages"] })
      if (selectedId) await qc.cancelQueries({ queryKey: ["message", selectedId] })
      const prevLabels = qc.getQueryData<ListResponse<MailLabel>>(["labels", activeMailboxId])
      const prevMessage = selectedId ? qc.getQueryData<MailMessage>(["message", selectedId]) : undefined
      qc.setQueryData<ListResponse<MailLabel>>(["labels", activeMailboxId], (current) => current ? { ...current, items: (current.items || []).filter((l) => l.id !== id) } : current)
      if (selectedId) qc.setQueryData<MailMessage>(["message", selectedId], (current) => current ? { ...current, labels: (current.labels || []).filter((l) => l.id !== id) } : current)
      qc.setQueriesData<InfiniteData<MailListResponse>>({ queryKey: ["messages"] }, (current) => current ? { ...current, pages: current.pages.map((page) => ({ ...page, items: (page.items || []).map((m) => ({ ...m, labels: (m.labels || []).filter((l) => l.id !== id) })) })) } : current)
      return { prevLabels, prevMessage }
    },
    onSuccess: (_data, id) => {
      if (mailView === "label" && selectedLabelId === id) {
        setMailView("folder")
        setFolder("Inbox")
        setSelectedLabelId("")
        setSelectedId(null)
      }
    },
    onError: (_error, _id, context) => {
      if (context?.prevLabels) qc.setQueryData(["labels", activeMailboxId], context.prevLabels)
      if (selectedId && context?.prevMessage) qc.setQueryData(["message", selectedId], context.prevMessage)
      qc.invalidateQueries({ queryKey: ["messages"] })
      toast({ title: "删除标签失败" })
    },
    onSettled: () => { qc.invalidateQueries({ queryKey: ["labels"] }); qc.invalidateQueries({ queryKey: ["messages"] }) },
  })
  const del = useMutation({
    mutationFn: ({ id }: { id: string; permanent: boolean }) => api.delete(id),
    onSuccess: async (_, { permanent }) => {
      setSelectedId(null)
      setPendingConfirm(null)
      await refreshMailData()
      toast({ title: permanent ? "邮件已永久删除" : "邮件已移入已删除" })
    },
    onError: (error) => toast({ title: "删除失败", description: error.message }),
  })
  const move = useMutation({ mutationFn: ({ id, folder }: { id: string; folder: string }) => api.move(id, folder), onSuccess: async () => { setSelectedId(null); await qc.invalidateQueries({ queryKey: ["messages"] }); await qc.invalidateQueries({ queryKey: ["folders"] }); await qc.invalidateQueries({ queryKey: ["mailboxes"] }); await qc.invalidateQueries({ queryKey: ["mail-stats"] }); await qc.invalidateQueries({ queryKey: ["labels"] }); toast({ title: "已移动" }) }, onError: (error) => toast({ title: "移动失败", description: error.message }) })
  const cancelScheduledSend = useMutation({
    mutationFn: (item: ScheduledSend) => api.cancelScheduledSend(item.id),
    onMutate: (item) => setCancelingScheduledId(item.id),
    onSuccess: async (_, item) => {
      await qc.invalidateQueries({ queryKey: ["scheduled-sends"] })
      toast({ title: item.status === "failed" ? "已移除失败记录" : "已取消定时发送" })
    },
    onError: (error) => toast({ title: "操作失败", description: error instanceof Error ? error.message : "请稍后重试" }),
    onSettled: () => setCancelingScheduledId(""),
  })
  const createFolder = useMutation({
    mutationFn: ({ name, icon }: { name: string; icon: string }) => api.createFolder({ mailboxId: activeMailboxId, name, icon }),
    onSuccess: (created) => {
      qc.invalidateQueries({ queryKey: ["folders"] })
      setFolderDialogOpen(false)
      openFolder(created.name)
      toast({ title: "文件夹已创建" })
    },
    onError: (error) => toast({ title: "创建文件夹失败", description: error instanceof Error ? error.message : "请稍后重试" }),
  })
  const reorderFolders = useMutation({
    mutationFn: (items: { id: string; sortOrder: number }[]) => api.reorderFolders({ mailboxId: activeMailboxId, folderIds: items.map((item) => item.id), folders: items }),
    onMutate: async (items) => {
      await qc.cancelQueries({ queryKey: ["folders", activeMailboxId] })
      const previous = qc.getQueryData<ListResponse<MailFolder>>(["folders", activeMailboxId])
      qc.setQueryData<ListResponse<MailFolder>>(["folders", activeMailboxId], (current) => {
        if (!current?.items) return current
        const order = new Map(items.map((item) => [item.id, item.sortOrder]))
        return {
          ...current,
          items: current.items
            .map((item) => order.has(item.id) ? { ...item, sortOrder: order.get(item.id) || item.sortOrder } : item)
            .sort(compareMailFolders),
        }
      })
      return { previous }
    },
    onError: (error, _items, context) => {
      if (context?.previous) qc.setQueryData(["folders", activeMailboxId], context.previous)
      toast({ title: "文件夹排序失败", description: error instanceof Error ? error.message : "请稍后重试" })
    },
    onSettled: () => qc.invalidateQueries({ queryKey: ["folders", activeMailboxId] }),
  })
  const deleteFolder = useMutation({
    mutationFn: (item: Extract<MailMenuItem, { type: "folder" }>) => api.deleteFolder(item.folderId, activeMailboxId, item.folderName),
    onSuccess: async (result, item) => {
      setPendingConfirm(null)
      if (mailView === "folder" && folder === item.folderName) {
        setFolder("Inbox")
        setSelectedId(null)
      }
      await refreshMailData()
      toast({ title: "文件夹已删除", description: result.moved > 0 ? `已将 ${result.moved} 封邮件移回收件箱` : undefined })
    },
    onError: (error) => toast({ title: "删除文件夹失败", description: error instanceof Error ? error.message : "请稍后重试" }),
  })
  const retrySendQueue = useMutation({
    mutationFn: (item: SendQueueItem) => api.retrySendQueue(item.id),
    onMutate: (item) => setSendQueuePendingId(item.id),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["send-queue"] })
      await qc.invalidateQueries({ queryKey: ["send-queue-audit"] })
      toast({ title: "已重新加入发送队列" })
    },
    onError: (error) => toast({ title: "重试失败", description: error instanceof Error ? error.message : "请稍后重试" }),
    onSettled: () => setSendQueuePendingId(""),
  })
  const cancelSendQueue = useMutation({
    mutationFn: (item: SendQueueItem) => api.cancelSendQueue(item.id),
    onMutate: (item) => setSendQueuePendingId(item.id),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["send-queue"] })
      await qc.invalidateQueries({ queryKey: ["send-queue-audit"] })
      toast({ title: "已取消发送任务" })
    },
    onError: (error) => toast({ title: "取消失败", description: error instanceof Error ? error.message : "请稍后重试" }),
    onSettled: () => setSendQueuePendingId(""),
  })
  React.useEffect(() => {
    if (!mailboxList.isSuccess) return
    const items = mailboxList.data?.items || []
    if (items.length === 0) {
      if (selectedMailboxId) {
        setSelectedMailboxId("")
        setSelectedId(null)
      }
      localStorage.removeItem("lanqin:selected-mailbox")
      return
    }
    if (!selectedMailboxId || (selectedMailboxId !== "all" && !items.some((item) => item.id === selectedMailboxId))) {
      setSelectedMailboxId("all")
      setSelectedExternalAccountId("")
      setFolder("Inbox")
      setMailView("folder")
      setSelectedLabelId("")
      setSelectedId(null)
      setMailFilter("all")
    }
  }, [mailboxList.isSuccess, mailboxList.data?.items, selectedMailboxId])

  React.useEffect(() => {
    localStorage.removeItem("lanqin:selected-mailbox")
    localStorage.removeItem("lanqin:selected-mailbox-version")
  }, [])

  React.useEffect(() => {
    setSelectedId(null)
    setMailFilter("all")
  }, [mailView])

  React.useEffect(() => {
    setCompactSelectedIds([])
  }, [selectedMailboxId, mailView, folder, selectedLabelId, query, advancedSearch, displayMode])

  React.useEffect(() => {
    applyTheme(darkMode, themeMountedRef.current)
    themeMountedRef.current = true
  }, [darkMode])

  React.useEffect(() => {
    const unlock = () => {
      const AudioContextCtor = window.AudioContext || (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext
      if (AudioContextCtor && !mailAudioContextRef.current) {
        const ctx = new AudioContextCtor()
        mailAudioContextRef.current = ctx
        if (ctx.state === "suspended") void ctx.resume()
      }
      if ("Notification" in window && Notification.permission === "default") {
        void Notification.requestPermission()
      }
    }
    window.addEventListener("pointerdown", unlock, { once: true })
    window.addEventListener("keydown", unlock, { once: true })
    return () => {
      window.removeEventListener("pointerdown", unlock)
      window.removeEventListener("keydown", unlock)
    }
  }, [])

  React.useEffect(() => {
    if (!activeMailboxId || !inboxProbe.data?.items) return
    const items = inboxProbe.data.items
    const latest = items[0]
    const nextState = { latestId: latest?.id || "", latestReceivedAt: latest?.receivedAt || "" }
    const prevState = mailNotifyStateRef.current[activeMailboxId]
    if (!prevState) {
      mailNotifyStateRef.current[activeMailboxId] = nextState
      return
    }
    const newMessages = items.filter((item) => item.receivedAt > prevState.latestReceivedAt && item.id !== prevState.latestId)
    mailNotifyStateRef.current[activeMailboxId] = nextState
    if (newMessages.length === 0) return

    const first = newMessages[0]
    const firstSender = senderDisplayName(first)
    const title = newMessages.length > 1 ? `收到 ${newMessages.length} 封新邮件` : `新邮件：${messageSubject(first)}`
    const description = newMessages.length > 1 ? `${firstSender} 等发来新邮件` : firstSender
    const openFirstMessage = () => {
      setMailView("folder")
      setFolder("Inbox")
      setSelectedId(first.id)
    }
    toast({
      title,
      description,
      onClick: openFirstMessage,
      onKeyDown: (event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault()
          openFirstMessage()
        }
      },
      role: "button",
      tabIndex: 0,
    })
    playIncomingMailSound(mailAudioContextRef)
    if ("Notification" in window && Notification.permission === "granted") {
      const notification = new Notification(title, {
        body: description,
        tag: `lanqin-mail-${activeMailboxId}`,
      })
      notification.onclick = () => {
        window.focus()
        openFirstMessage()
        notification.close()
      }
    }
  }, [activeMailboxId, inboxProbe.data?.items, toast])

  React.useEffect(() => {
    const events = new EventSource("/api/events", { withCredentials: true })
    events.addEventListener("sync", () => {
      qc.invalidateQueries({ queryKey: ["messages"] })
      qc.invalidateQueries({ queryKey: ["admin", "unknown-messages"] })
      qc.invalidateQueries({ queryKey: ["folders"] })
      qc.invalidateQueries({ queryKey: ["mail-stats"] })
      qc.invalidateQueries({ queryKey: ["labels"] })
      qc.invalidateQueries({ queryKey: ["mail-notifications"] })
    })
    return () => events.close()
  }, [qc])

  React.useEffect(() => {
    if (!publicSettings.data?.mailAutoRefresh) return
    const timer = window.setInterval(() => {
      setAutoRefreshing(true)
      Promise.all([
        qc.invalidateQueries({ queryKey: ["messages"] }),
        qc.invalidateQueries({ queryKey: ["admin", "unknown-messages"] }),
        qc.invalidateQueries({ queryKey: ["external-messages"] }),
        qc.invalidateQueries({ queryKey: ["mail-external-folders"] }),
        qc.invalidateQueries({ queryKey: ["folders"] }),
        qc.invalidateQueries({ queryKey: ["mail-stats"] }),
        qc.invalidateQueries({ queryKey: ["labels"] }),
        qc.invalidateQueries({ queryKey: ["scheduled-sends"] }),
        qc.invalidateQueries({ queryKey: ["send-queue"] }),
        qc.invalidateQueries({ queryKey: ["mail-notifications"] }),
      ]).finally(() => {
        window.setTimeout(() => setAutoRefreshing(false), 600)
      })
    }, mailRefreshInterval || 30000)
    return () => window.clearInterval(timer)
  }, [mailRefreshInterval, publicSettings.data?.mailAutoRefresh, qc])

  const selected = detail.data
  const allMessages = (mailView === "external" ? externalMessages.data?.pages : mailView === "unknown" ? unknownMessages.data?.pages : messages.data?.pages)?.flatMap((page) => page.items || []) || []
  const visibleMessages = allMessages.filter((message) => {
    if (!messageMatchesAdvancedSearch(message, advancedSearch)) return false
    if (mailFilter === "unread") return !message.isRead
    if (mailFilter === "starred") return message.isStarred
    if (mailFilter === "attachments") return message.hasAttachments
    if (mailFilter === "recent7") {
      const receivedAt = new Date(message.receivedAt).getTime()
      return Number.isFinite(receivedAt) && receivedAt >= Date.now() - 7 * 24 * 60 * 60 * 1000
    }
    return true
  })
  const unreadCount = allMessages.filter((message) => !message.isRead).length
  const mailboxUnreadCount = (folders.data?.items || []).find((item) => item.name === "Inbox")?.unreadCount ?? unreadCount
  const starredCount = mailStats.data?.starredMessages ?? (mailView === "starred" ? allMessages.length : 0)
  const scheduledItems = scheduledSends.data?.items || []
  const scheduledDraftIds = new Set(scheduledItems.map((item) => item.draftId).filter((draftId): draftId is string => Boolean(draftId)))
  const scheduledCount = scheduledItems.length
  const scheduledQuery = query.trim().toLowerCase()
  const visibleScheduledItems = scheduledQuery
    ? scheduledItems.filter((item) => [item.subject, item.snippet, ...(item.to || [])].join(" ").toLowerCase().includes(scheduledQuery))
    : scheduledItems
  const sendQueueItems = sendQueue.data?.items || []
  const sendQueueCount = sendQueueItems.filter((item) => item.status === "failed" || item.status === "queued" || item.status === "sending").length
  const visibleSendQueueItems = sendQueueItems
  const mailMenuItems = buildMailMenuItems(folders.data?.items || [], starredCount, canScheduleMail ? scheduledCount : 0, canScheduleMail, canViewSendQueue ? sendQueueCount : 0, canViewSendQueue, canViewUnknownMail)
  const primaryMailMenuItems = mailMenuItems.filter((item) => !isCustomMenuFolder(item))
  const customMailMenuItems = mailMenuItems.filter(isCustomMenuFolder)
  const customFolderUnreadCount = customMailMenuItems.reduce((total, item) => total + Math.max(0, item.count || 0), 0)
  const canOrganizeCurrentMailbox = canOrganizeMail && !isAllMailboxSelected
  const canManageCurrentMailboxLabels = canManageLabels && !isAllMailboxSelected
  const externalAccountItems = externalImapEnabled ? externalMailAccounts.data?.items || [] : []
  const externalFolderItems = externalImapEnabled ? externalFolders.data?.items || [] : []
  const labelItems = labels.data?.items || []
  const labelUnreadCount = labelItems.reduce((total, label) => total + Math.max(0, label.unreadCount || 0), 0)
  const selectedLabel = labelItems.find((item) => item.id === selectedLabelId)
  const viewTitle = mailView === "external" ? `${selectedExternalAccount?.name || "外部邮箱"} · ${folderLabels[externalFolder] || externalFolder}` : mailView === "unknown" ? "未知收件" : mailView === "sendQueue" ? "发送队列" : mailView === "scheduled" ? "待发送" : mailView === "starred" ? "星标邮件" : mailView === "label" ? selectedLabel?.name || "标签" : folderLabels[folder] || folder
  const isTransferView = mailView === "folder" || mailView === "starred" || mailView === "label" || mailView === "unknown"
  const canExportCurrentView = canReadMail && isTransferView
  const canImportCurrentView = canOrganizeMail && isTransferView && mailView !== "unknown" && !!selectedMailbox
  const emptyMessage = getEmptyMessage(mailView, mailView === "external" ? externalFolder : folder, allMessages.length)
  const visibleMessageIds = visibleMessages.map((message) => message.id)
  const selectedCountOnPage = compactSelectedIds.filter((id) => visibleMessageIds.includes(id)).length
  const selectedMessagesOnPage = visibleMessages.filter((message) => compactSelectedIds.includes(message.id))
  const bulkReadAction: BulkAction = selectedMessagesOnPage.some((message) => !message.isRead) ? "read" : "unread"
  const compactAllSelected = visibleMessageIds.length > 0 && selectedCountOnPage === visibleMessageIds.length
  const compactSomeSelected = selectedCountOnPage > 0 && !compactAllSelected
  const mailMessagesLoading = mailView === "external" ? externalMessages.isLoading : mailView === "unknown" ? unknownMessages.isLoading : messages.isLoading
  const mailMessagesError = mailView === "external" ? externalMessages.error : mailView === "unknown" ? unknownMessages.error : messages.error
  const mailMessagesLoadingMore = mailView === "external" ? externalMessages.isFetchingNextPage : mailView === "unknown" ? unknownMessages.isFetchingNextPage : messages.isFetchingNextPage
  const hasMoreMessages = mailView === "external" ? !!externalMessages.hasNextPage : mailView === "unknown" ? !!unknownMessages.hasNextPage : !!messages.hasNextPage
  const canLoadMore = hasMoreMessages && !mailMessagesLoadingMore
  const loadMoreMessages = () => mailView === "external" ? externalMessages.fetchNextPage() : mailView === "unknown" ? unknownMessages.fetchNextPage() : messages.fetchNextPage()
  const refetchMessages = () => mailView === "external" ? externalMessages.refetch() : mailView === "unknown" ? unknownMessages.refetch() : messages.refetch()
  function toggleCompactSelectAll(checked: boolean) {
    setCompactSelectedIds(checked ? visibleMessageIds : [])
  }
  function toggleCompactSelect(messageId: string, checked: boolean) {
    setCompactSelectedIds((ids) => checked ? Array.from(new Set([...ids, messageId])) : ids.filter((id) => id !== messageId))
  }
  async function refreshMailData() {
    await Promise.all([
      qc.invalidateQueries({ queryKey: ["messages"] }),
      qc.invalidateQueries({ queryKey: ["admin", "unknown-messages"] }),
      qc.invalidateQueries({ queryKey: ["external-messages"] }),
      qc.invalidateQueries({ queryKey: ["mail-external-folders"] }),
      qc.invalidateQueries({ queryKey: ["mail-external-accounts"] }),
      qc.invalidateQueries({ queryKey: ["folders"] }),
      qc.invalidateQueries({ queryKey: ["mailboxes"] }),
      qc.invalidateQueries({ queryKey: ["mail-stats"] }),
      qc.invalidateQueries({ queryKey: ["labels"] }),
      qc.invalidateQueries({ queryKey: ["scheduled-sends"] }),
      qc.invalidateQueries({ queryKey: ["send-queue"] }),
    ])
  }
  async function runBulkAction(action: BulkAction) {
    if (!canOrganizeMail) return
    const ids = compactSelectedIds.filter((id) => visibleMessageIds.includes(id))
    if (ids.length === 0) return
    if (action === "delete") {
      setPendingConfirm({
        title: "删除所选邮件？",
        description: `将删除当前选中的 ${ids.length} 封邮件，此操作无法从邮件列表中恢复。`,
        confirmText: "删除邮件",
        onConfirm: () => runConfirmedBulkAction("delete", ids),
      })
      return
    }
    await runConfirmedBulkAction(action, ids)
  }
  async function runConfirmedBulkAction(action: BulkAction, ids: string[]) {
    setBulkPending(true)
    try {
      let completionTitle = `已处理 ${ids.length} 封邮件`
      let completionDescription: string | undefined
      if (action === "read" || action === "unread") {
        const read = action === "read"
        await Promise.all(ids.map((id) => api.markRead(id, read)))
      } else if (action === "star" || action === "unstar") {
        const starred = action === "star"
        await Promise.all(ids.map((id) => api.star(id, starred)))
      } else if (action === "delete") {
        await Promise.all(ids.map((id) => api.delete(id)))
        completionTitle = `已永久删除 ${ids.length} 封邮件`
      } else {
        const target = action === "archive" ? "Archive" : action === "inbox" ? "Inbox" : action === "trash" ? "Trash" : "Spam"
        const result = await api.bulkMove(ids, target)
        completionTitle = result.ok
          ? target === "Trash"
            ? `已将 ${result.moved} 封邮件移入已删除`
            : target === "Spam"
              ? `已将 ${result.moved} 封邮件移入垃圾邮件`
              : target === "Inbox"
                ? `已将 ${result.moved} 封邮件移回收件箱`
                : `已归档 ${result.moved} 封邮件`
          : "批量移动部分失败"
        completionDescription = result.ok ? undefined : result.message
      }
      if (selectedId && ids.includes(selectedId)) setSelectedId(null)
      setCompactSelectedIds([])
      setPendingConfirm(null)
      await refreshMailData()
      toast({ title: completionTitle, description: completionDescription })
    } catch (error) {
      toast({ title: "批量操作失败", description: error instanceof Error ? error.message : "请稍后重试" })
    } finally {
      setBulkPending(false)
    }
  }
  async function runBulkMoveToFolder(folderName: string) {
    if (!canOrganizeMail) return
    const ids = compactSelectedIds.filter((id) => visibleMessageIds.includes(id))
    if (ids.length === 0) return
    setBulkPending(true)
    try {
      const result = await api.bulkMove(ids, folderName)
      if (selectedId && ids.includes(selectedId)) setSelectedId(null)
      setCompactSelectedIds([])
      await refreshMailData()
      toast({ title: result.ok ? (folderName === "Inbox" ? `已将 ${result.moved} 封邮件移回收件箱` : `已移动 ${result.moved} 封邮件`) : "批量移动部分失败", description: result.ok ? undefined : result.message })
    } catch (error) {
      toast({ title: "批量移动失败", description: error instanceof Error ? error.message : "请稍后重试" })
    } finally {
      setBulkPending(false)
    }
  }
  function confirmDeleteMessage(message: MailMessage) {
    const permanent = message.folder === "Trash"
    const subject = messageSubject(message)
    setPendingConfirm({
      title: permanent ? "永久删除这封邮件？" : "将这封邮件移入已删除？",
      description: permanent
        ? `邮件“${subject}”将被永久删除，且无法恢复。`
        : `邮件“${subject}”将移入已删除。`,
      confirmText: permanent ? "永久删除" : "移入已删除",
      onConfirm: () => del.mutate({ id: message.id, permanent }),
    })
  }
  function openCompose(draft?: ComposeDraft) {
    if (draft?.isDraft && !canManageDrafts) return
    if (!draft?.isDraft && !canSendMail) return
    setComposeDraft(draft || { key: `new-${Date.now()}` })
    setComposeOpen(true)
  }
  function openReply(message: MailMessage) {
    if (!canSendMail) return
    openCompose({ key: `reply-${message.id}-${Date.now()}`, mailboxId: message.mailboxId, to: message.from, subject: withPrefix(messageSubject(message), "Re:"), text: quoteMessage(message) })
  }
  function openForward(message: MailMessage) {
    if (!canSendMail) return
    openCompose({ key: `forward-${message.id}-${Date.now()}`, mailboxId: message.mailboxId, subject: withPrefix(messageSubject(message), "Fwd:"), text: quoteMessage(message) })
  }
  async function openDraft(message: MailMessage) {
    if (!canManageDrafts) return
    if (scheduledDraftIds.has(message.id)) {
      toast({ title: "这封草稿已在待发送队列中", description: "请先取消定时发送，再继续编辑。" })
      openScheduled()
      return
    }
    try {
      const detail = await api.message(message.id, { markRead: false })
      openCompose({
        key: `draft-${detail.id}-${Date.now()}`,
        id: detail.id,
        mailboxId: detail.mailboxId,
        to: detail.to.join(", "),
        cc: detail.cc.join(", "),
        bcc: (detail.bcc || []).join(", "),
        subject: detail.subject === "(无主题)" ? "" : detail.subject,
        text: detail.bodyText || "",
        html: detail.bodyHtml || "",
        files: await attachmentFilesFromMessage(detail),
        isDraft: true,
      })
      setSelectedId(null)
    } catch (error) {
      toast({ title: "打开草稿失败", description: error instanceof Error ? error.message : "请稍后重试" })
    }
  }
  function switchMailbox(mailboxId: string) {
    setSelectedMailboxId(mailboxId)
    setSelectedExternalAccountId("")
    setFolder("Inbox")
    setMailView("folder")
    setSelectedLabelId("")
    setSelectedId(null)
    setMailFilter("all")
    if (mailboxId === "all") {
      setLabelEditMode(false)
      setNewLabelEditing(false)
    }
    setMobileSidebarOpen(false)
  }
  function openFolder(nextFolder: string) {
    setSelectedExternalAccountId("")
    setFolder(nextFolder)
    setMailView("folder")
    setSelectedLabelId("")
    setSelectedId(null)
    setMailFilter("all")
    setMobileSidebarOpen(false)
  }
  function openStarred() {
    setSelectedExternalAccountId("")
    setMailView("starred")
    setSelectedLabelId("")
    setSelectedId(null)
    setMailFilter("all")
    setMobileSidebarOpen(false)
  }
  function openScheduled() {
    setSelectedExternalAccountId("")
    setMailView("scheduled")
    setSelectedLabelId("")
    setSelectedId(null)
    setMailFilter("all")
    setMobileSidebarOpen(false)
  }
  function openUnknownMail() {
    if (!canViewUnknownMail) return
    setSelectedExternalAccountId("")
    setMailView("unknown")
    setSelectedLabelId("")
    setSelectedId(null)
    setMailFilter("all")
    setMobileSidebarOpen(false)
  }
  function openMessageContextMenu(event: React.MouseEvent, message: MailMessage) {
    if (mailView === "external" || mailView === "unknown") return
    event.preventDefault()
    event.stopPropagation()
    if (message.folder !== "Drafts") setSelectedId(message.id)
    setMessageContextMenu({ message, x: event.clientX, y: event.clientY })
  }
  function closeMessageContextMenu() {
    setMessageContextMenu(null)
  }
  function openSidebarContextMenu(event: React.MouseEvent, item: MailMenuItem) {
    event.preventDefault()
    event.stopPropagation()
    setSidebarContextMenu({ item, x: event.clientX, y: event.clientY })
  }
  function closeSidebarContextMenu() {
    setSidebarContextMenu(null)
  }
  function runAfterClosingMobileSidebar(action: () => void) {
    if (!isMobile) {
      action()
      return
    }
    if (mobileSidebarOpen) setMobileSidebarOpen(false)
    window.setTimeout(action, 250)
  }
  function activateSidebarItem(item: MailMenuItem) {
    if (item.type === "starred") openStarred()
    else if (item.type === "scheduled") openScheduled()
    else if (item.type === "sendQueue") openSendQueue()
    else if (item.type === "unknown") openUnknownMail()
    else openFolder(item.folderName)
  }
  function openExternalFolder(account: ExternalImapAccount, folderName = "INBOX") {
    setSelectedExternalAccountId(account.id)
    setExpandedExternalAccountIds([account.id])
    setExternalFolder(folderName)
    setMailView("external")
    setSelectedLabelId("")
    setSelectedId(null)
    setMailFilter("all")
    setMobileSidebarOpen(false)
  }
  function toggleExternalAccount(account: ExternalImapAccount) {
    if (sidebarCollapsed) {
      openExternalFolder(account, "INBOX")
      return
    }
    const expanded = expandedExternalAccountIds.includes(account.id)
    if (expanded) {
      setExpandedExternalAccountIds((items) => items.filter((id) => id !== account.id))
      return
    }
    setExpandedExternalAccountIds([account.id])
    if (selectedExternalAccountId !== account.id || mailView !== "external") {
      openExternalFolder(account, "INBOX")
    }
  }
  function reorderCustomFolder(draggedId: string, target: FolderDropTarget) {
    if (!canOrganizeMail || reorderFolders.isPending) return
    const foldersByID = new Map((folders.data?.items || []).map((item) => [item.id, item]))
    const dragged = foldersByID.get(draggedId)
    if (!dragged || !isCustomMailFolder(dragged)) return
    const menuWithoutDragged = mailMenuItems.filter((item) => !(item.type === "folder" && item.folderId === draggedId))
    let insertIndex = menuWithoutDragged.length
    if (target.edge !== "end") {
      const targetIndex = menuWithoutDragged.findIndex((item) => item.key === target.key)
      if (targetIndex < 0) return
      insertIndex = target.edge === "before" ? targetIndex : targetIndex + 1
    }
    const draggedMenuItem: MailMenuItem = {
      type: "folder",
      key: dragged.id,
      folderId: dragged.id,
      folderName: dragged.name,
      label: folderLabels[dragged.name] || dragged.name,
      icon: folderIcons[dragged.role] || <Inbox className="h-4 w-4" />,
      count: dragged.name === "Drafts" ? dragged.totalCount : dragged.unreadCount,
      custom: true,
      order: dragged.sortOrder,
    }
    const nextMenu = [...menuWithoutDragged]
    nextMenu.splice(insertIndex, 0, draggedMenuItem)
    const nextFolders = assignCustomFolderOrders(nextMenu)
    reorderFolders.mutate(nextFolders)
  }
  function moveSidebarFolder(item: MailMenuItem, action: "top" | "up" | "down" | "bottom") {
    if (item.type !== "folder" || !item.custom) return
    const currentIndex = mailMenuItems.findIndex((entry) => entry.key === item.key)
    if (currentIndex < 0) return
    if (action === "top") {
      reorderCustomFolder(item.folderId, { key: mailMenuItems[0]?.key || "__end__", edge: "before" })
      return
    }
    if (action === "bottom") {
      reorderCustomFolder(item.folderId, { key: "__end__", edge: "end" })
      return
    }
    const targetIndex = action === "up" ? currentIndex - 1 : currentIndex + 1
    const target = mailMenuItems[targetIndex]
    if (!target) return
    reorderCustomFolder(item.folderId, { key: target.key, edge: action === "up" ? "before" : "after" })
  }
  function confirmDeleteFolder(item: MailMenuItem) {
    if (item.type !== "folder" || !item.custom) return
    setPendingConfirm({
      title: `删除文件夹“${item.label}”？`,
      description: isAllMailboxSelected
        ? "所有邮箱中的同名文件夹都会删除，文件夹内邮件会移回各自的收件箱。"
        : "文件夹内的邮件会移回收件箱，不会被删除。",
      confirmText: "删除文件夹",
      onConfirm: () => deleteFolder.mutate(item),
    })
  }
  function handleFolderDragStart(event: React.DragEvent, item: MailMenuItem) {
    if (item.type !== "folder" || !item.custom || sidebarCollapsed || !canOrganizeMail) return
    event.dataTransfer.effectAllowed = "move"
    event.dataTransfer.setData("text/plain", item.folderId)
    setDraggingFolderId(item.folderId)
  }
  function handleFolderDragOver(event: React.DragEvent, item: MailMenuItem) {
    if (!draggingFolderId || item.key === draggingFolderId) return
    event.preventDefault()
    event.dataTransfer.dropEffect = "move"
    const rect = event.currentTarget.getBoundingClientRect()
    setFolderDropTarget({ key: item.key, edge: event.clientY < rect.top + rect.height / 2 ? "before" : "after" })
  }
  function handleFolderDrop(event: React.DragEvent, item: MailMenuItem) {
    if (!draggingFolderId) return
    event.preventDefault()
    const draggedId = event.dataTransfer.getData("text/plain") || draggingFolderId
    const rect = event.currentTarget.getBoundingClientRect()
    const edge = event.clientY < rect.top + rect.height / 2 ? "before" : "after"
    setDraggingFolderId("")
    setFolderDropTarget(null)
    reorderCustomFolder(draggedId, { key: item.key, edge })
  }
  function handleFolderDropEnd(event: React.DragEvent) {
    if (!draggingFolderId) return
    event.preventDefault()
    const draggedId = event.dataTransfer.getData("text/plain") || draggingFolderId
    setDraggingFolderId("")
    setFolderDropTarget(null)
    reorderCustomFolder(draggedId, { key: "__end__", edge: "end" })
  }
  function clearFolderDragState() {
    setDraggingFolderId("")
    setFolderDropTarget(null)
  }
  function runMessageContextAction(action: "open" | "reply" | "forward" | "read" | "star" | "archive" | "trash" | "spam" | "delete", message: MailMessage) {
    closeMessageContextMenu()
    if (action === "open") {
      openMessage(message.id)
      return
    }
    if (action === "reply") {
      openReply(message)
      return
    }
    if (action === "forward") {
      openForward(message)
      return
    }
    if (action === "read") {
      markRead.mutate({ id: message.id, read: !message.isRead })
      return
    }
    if (action === "star") {
      star.mutate({ id: message.id, starred: !message.isStarred })
      return
    }
    if (action === "archive") {
      move.mutate({ id: message.id, folder: message.folder === "Archive" ? "Inbox" : "Archive" })
      return
    }
    if (action === "trash") {
      move.mutate({ id: message.id, folder: "Trash" })
      return
    }
    if (action === "spam") {
      move.mutate({ id: message.id, folder: "Spam" })
      return
    }
    confirmDeleteMessage(message)
  }
  function openSendQueue() {
    setSelectedExternalAccountId("")
    setMailView("sendQueue")
    setSelectedLabelId("")
    setSelectedId(null)
    setMailFilter("all")
    setMobileSidebarOpen(false)
  }
  function openMessageSendTimeline(message: MailMessage) {
    if (!message.sendQueueId) return
    setSendQueueAuditId(message.sendQueueId)
  }
  function openLabel(labelId: string) {
    setSelectedExternalAccountId("")
    setSelectedLabelId(labelId)
    setMailView("label")
    setSelectedId(null)
    setMailFilter("all")
    setMobileSidebarOpen(false)
  }
  function openMessage(messageId: string | null) {
    if (!messageId) {
      setSelectedId(null)
      return
    }
    const message = allMessages.find((item) => item.id === messageId)
    if (mailView !== "external" && message?.folder === "Drafts") {
      void openDraft(message)
      return
    }
    setSelectedId(messageId)
    if (message && !message.isRead && canOrganizeMail && mailView !== "unknown") {
      if (mailView === "external" && selectedExternalAccountId) markExternalRead.mutate({ id: selectedExternalAccountId, remoteId: message.id, read: true })
      else markRead.mutate({ id: message.id, read: true })
    }
  }
  async function refreshMail() {
    if (refreshing || autoRefreshing) return
    setRefreshing(true)
    try {
      await refreshMailData()
      const refreshedAt = new Date()
      toast({ title: "邮件已刷新", description: `更新于 ${refreshedAt.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" })}` })
    } catch (error) {
      toast({ title: "刷新失败", description: error instanceof Error ? error.message : "请稍后重试" })
    } finally {
      setRefreshing(false)
    }
  }
  async function exportCurrentMail() {
    if (!canExportCurrentView || exportingMail) return
    setExportingMail(true)
    const exportView = mailView === "unknown" ? "unknown" : mailView === "starred" ? "starred" : mailView === "label" ? "label" : "folder"
    const selectedMessageIds = compactSelectedIds.filter((id) => visibleMessageIds.includes(id))
    const anchor = document.createElement("a")
    anchor.href = api.exportMailUrl({
      view: exportView,
      mailboxId: mailView === "unknown" ? undefined : activeMailboxId,
      folder: exportView === "folder" ? folder : undefined,
      labelId: exportView === "label" ? selectedLabelId : undefined,
      messageIds: selectedMessageIds.length > 0 ? selectedMessageIds : undefined,
    })
    anchor.download = `${viewTitle.replace(/[\\/:*?"<>|]+/g, "-") || "邮件"}-${new Date().toISOString().slice(0, 10)}.zip`
    document.body.appendChild(anchor)
    anchor.click()
    anchor.remove()
    toast({ title: "已开始下载", description: selectedMessageIds.length > 0 ? `正在打包选中的 ${selectedMessageIds.length} 封邮件。` : "邮件将打包为 ZIP，压缩包内为标准 EML 文件；邮件较多时请查看浏览器下载进度。" })
    window.setTimeout(() => setExportingMail(false), 1000)
  }
  function chooseMailImport() {
    if (!canImportCurrentView || importingMail) {
      if (isAllMailboxSelected) toast({ title: "请先选择一个邮箱", description: "导入邮件需要明确目标邮箱。" })
      return
    }
    mailImportInputRef.current?.click()
  }
  async function importSelectedMailFiles(event: React.ChangeEvent<HTMLInputElement>) {
    const files = Array.from(event.target.files || [])
    event.target.value = ""
    if (files.length === 0 || !selectedMailbox) return
    setImportingMail(true)
    try {
      const batches = buildMailImportBatches(files)
      const target = { mailboxId: selectedMailbox.id, folder: mailView === "folder" ? folder : "Inbox" }
      const result = { imported: 0, skipped: 0, errors: [] as string[] }
      for (const batch of batches) {
        try {
          const current = await api.importMail(batch, target)
          result.imported += current.imported
          result.skipped += current.skipped
          result.errors.push(...current.errors)
        } catch (error) {
          result.skipped += batch.length
          result.errors.push(error instanceof Error ? error.message : "导入请求失败")
        }
      }
      await refreshMailData()
      if (result.imported === 0 && result.errors.length > 0) throw new Error(result.errors[0])
      toast({
        title: `已导入 ${result.imported} 封邮件`,
        description: result.skipped > 0 ? `${result.skipped} 封未能导入${result.errors[0] ? `：${result.errors[0]}` : ""}` : `已保存到 ${mailView === "folder" ? viewTitle : "收件箱"}`,
      })
    } catch (error) {
      toast({ title: "导入失败", description: error instanceof Error ? error.message : "请检查 EML/MBOX 文件" })
    } finally {
      setImportingMail(false)
    }
  }
  async function copyCurrentMailbox() {
    if (!selectedMailbox?.address) return
    await navigator.clipboard.writeText(selectedMailbox.address)
    toast({ title: "邮箱地址已复制" })
  }
  function openSettings() {
    void import("@/pages/profile").then(() => navigate("/profile"))
  }
  function toggleAdvancedSearch() {
    setAdvancedSearchDraft(advancedSearch)
    setAdvancedSearchOpen((open) => !open)
  }
  function updateAdvancedSearch(next: AdvancedMailSearch) {
    setAdvancedSearch(next)
    setAdvancedSearchDraft(next)
    setSelectedId(null)
    setCompactSelectedIds([])
  }
  function applyAdvancedSearch(draft: AdvancedMailSearchDraft) {
    updateAdvancedSearch({
      from: draft.from.trim(),
      to: draft.to.trim(),
      subject: draft.subject.trim(),
      startDate: draft.startDate,
      endDate: draft.endDate,
      hasAttachments: draft.hasAttachments,
      unread: draft.unread,
      starred: draft.starred,
    })
    setAdvancedSearchOpen(false)
  }
  function clearAdvancedSearch() {
    updateAdvancedSearch(emptyAdvancedSearch)
  }
  function removeAdvancedSearchFilter(key: keyof AdvancedMailSearch) {
    updateAdvancedSearch({ ...advancedSearch, [key]: emptyAdvancedSearch[key] })
  }
  function handleSearchKeyDown(event: React.KeyboardEvent<HTMLInputElement>) {
    if (event.key !== "Enter") return
    const parsed = parseSmartSearchQuery(query)
    if (!parsed.active) return
    event.preventDefault()
    setQuery(parsed.query)
    updateAdvancedSearch({ ...advancedSearch, ...parsed.search })
    setAdvancedSearchOpen(false)
  }
  const sidebarContent = (
    <Sidebar collapsible="none" className="h-full min-h-0 w-full overflow-hidden border-r border-border bg-sidebar text-sidebar-foreground">
      <SidebarHeader className={cn("shrink-0 pb-2 pt-3", sidebarCollapsed ? "px-2" : "px-3")}>
        <AccountHeader
          collapsed={sidebarCollapsed}
          name={me.data?.user.displayName || selectedMailbox?.address || "NewSzxcn"}
          email={me.data?.user.email || selectedMailbox?.address}
          darkMode={darkMode}
          onToggleTheme={() => setDarkMode((value) => !value)}
          language={language}
          onLanguageChange={setLanguage}
          onSettings={openSettings}
        />
        <div className={cn("relative mt-2", sidebarCollapsed && "flex justify-center")}>
          <MailboxSwitcher
            collapsed={sidebarCollapsed}
            mailboxes={mailboxList.data?.items || []}
            loading={mailboxList.isLoading}
            selectedMailboxId={selectedMailboxId}
            selectedMailbox={selectedMailbox}
            unreadCount={mailboxUnreadCount}
            hasCopyAction={showMailboxCopy}
            onSelect={switchMailbox}
          />
          {!sidebarCollapsed && showMailboxCopy && (
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="absolute right-1 top-1 h-6 w-6 rounded-sm text-muted-foreground shadow-none hover:bg-accent hover:text-foreground"
              onClick={copyCurrentMailbox}
              disabled={!selectedMailbox}
              aria-label="复制邮箱地址"
              title="复制邮箱地址"
            >
              <Copy className="h-3.5 w-3.5" />
            </Button>
          )}
        </div>
        {canSendMail && (
          <Button className={cn("mt-4 h-9 w-full rounded-md text-[13px] font-semibold shadow-none", sidebarCollapsed && "px-0")} size={sidebarCollapsed ? "icon" : "default"} onClick={() => openCompose()} disabled={!selectedComposeMailbox}>
            <PencilLine className="h-4 w-4" />
            {!sidebarCollapsed && <span>写邮件</span>}
          </Button>
        )}
      </SidebarHeader>
      <SidebarContent className="min-h-0 overflow-x-hidden overflow-y-auto overscroll-contain px-1 pb-4">
        {!sidebarCollapsed && publicSettings.isError && <SidebarQueryFailure label="邮箱设置读取失败" onRetry={() => { void publicSettings.refetch() }} />}
        <SidebarGroup>
          {!sidebarCollapsed && (
            <div className="flex items-center justify-between px-2 py-1">
              <SidebarGroupLabel className="m-0 h-auto p-0 text-xs font-semibold text-muted-foreground">邮件</SidebarGroupLabel>
            </div>
          )}
          <SidebarGroupContent>
            <SidebarMenu>
              {primaryMailMenuItems.map((item) => (
                <SidebarMenuItem
                  key={item.key}
                  draggable={item.type === "folder" && item.custom && canOrganizeCurrentMailbox && !sidebarCollapsed}
                  onDragStart={(event) => handleFolderDragStart(event, item)}
                  onDragOver={(event) => handleFolderDragOver(event, item)}
                  onDragLeave={() => { if (folderDropTarget?.key === item.key) setFolderDropTarget(null) }}
                  onDrop={(event) => handleFolderDrop(event, item)}
                  onDragEnd={clearFolderDragState}
                  onContextMenu={(event) => openSidebarContextMenu(event, item)}
                >
                  <SidebarMenuButton
                    isActive={item.type === "starred" ? mailView === "starred" : item.type === "scheduled" ? mailView === "scheduled" : item.type === "sendQueue" ? mailView === "sendQueue" : item.type === "unknown" ? mailView === "unknown" : mailView === "folder" && folder === item.folderName}
                    className={cn(
                      "h-8 rounded-md px-2 text-[13px] font-normal",
                      sidebarCollapsed && "justify-center px-0",
                      item.type === "folder" && item.custom && canOrganizeCurrentMailbox && !sidebarCollapsed && "cursor-grab active:cursor-grabbing",
                      item.type === "folder" && item.custom && draggingFolderId === item.folderId && "opacity-50",
                      folderDropTarget?.key === item.key && "bg-accent/60",
                      folderDropTarget?.key === item.key && folderDropTarget.edge === "before" && "border-t-2 border-t-primary",
                      folderDropTarget?.key === item.key && folderDropTarget.edge === "after" && "border-b-2 border-b-primary"
                    )}
                    onClick={() => activateSidebarItem(item)}
                    >
                      {item.icon}
                      {!sidebarCollapsed && <span className="min-w-0 flex-1 truncate">{item.label}</span>}
                      {!sidebarCollapsed && <UnreadBadge count={item.count} />}
                    </SidebarMenuButton>
                  </SidebarMenuItem>
              ))}
              {!sidebarCollapsed && canOrganizeCurrentMailbox && customMailMenuItems.length === 0 && (
                <div
                  className={cn("mx-2 h-4 rounded-sm border border-dashed border-transparent", folderDropTarget?.edge === "end" && "border-primary bg-accent/60")}
                  onDragOver={(event) => {
                    if (!draggingFolderId) return
                    event.preventDefault()
                    event.dataTransfer.dropEffect = "move"
                    setFolderDropTarget({ key: "__end__", edge: "end" })
                  }}
                  onDragLeave={() => { if (folderDropTarget?.edge === "end") setFolderDropTarget(null) }}
                  onDrop={handleFolderDropEnd}
                />
              )}
            </SidebarMenu>
            {folders.isLoading && <FolderSkeleton />}
            {!sidebarCollapsed && folders.isError && <SidebarQueryFailure label="文件夹读取失败" onRetry={() => { void folders.refetch() }} />}
          </SidebarGroupContent>
        </SidebarGroup>
        {!sidebarCollapsed && externalImapEnabled && externalMailAccounts.isError && <SidebarQueryFailure label="外部邮箱读取失败" onRetry={() => { void externalMailAccounts.refetch() }} />}
        {externalAccountItems.length > 0 && <SidebarGroup>
          {!sidebarCollapsed && <SidebarGroupLabel>外部邮箱</SidebarGroupLabel>}
          <SidebarGroupContent>
            <SidebarMenu>
              {externalAccountItems.map((account) => {
                const expanded = expandedExternalAccountIds.includes(account.id)
                return (
                <React.Fragment key={account.id}>
                  <SidebarMenuItem>
                    <SidebarMenuButton
                      isActive={mailView === "external" && selectedExternalAccountId === account.id}
                      size={sidebarCollapsed ? "default" : "lg"}
                      className={cn(sidebarCollapsed && "justify-center px-0")}
                      title={`${externalAccountLabel(account)}${account.name && account.name !== externalAccountLabel(account) ? ` · ${account.name}` : ""}`}
                      onClick={() => toggleExternalAccount(account)}
                    >
                      <Mail className="h-4 w-4" />
                      {!sidebarCollapsed && (
                        <>
                          <span className="min-w-0 flex-1">
                            <span className="block truncate">{externalAccountLabel(account)}</span>
                            <span className="block truncate text-xs font-normal text-muted-foreground">{externalAccountSubtitle(account)}</span>
                          </span>
                          <ChevronDown className={cn("h-4 w-4 text-muted-foreground transition-transform", expanded && "rotate-180")} />
                        </>
                      )}
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                  {!sidebarCollapsed && expanded && externalFolderItems.map((item) => (
                    <SidebarMenuItem key={`${account.id}-${item.name}`}>
                      <SidebarMenuButton
                        isActive={externalFolder === item.name}
                        className="h-8 pl-8 text-[13px]"
                        onClick={() => openExternalFolder(account, item.name)}
                      >
                        {folderIcons[item.role.toLowerCase()] || <Inbox className="h-4 w-4" />}
                        <span>{folderLabels[item.role] || folderLabels[item.name] || item.name}</span>
                        {item.unreadCount > 0 && <Badge variant="secondary" className="ml-auto">{item.unreadCount}</Badge>}
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                  ))}
                  {!sidebarCollapsed && expanded && externalFolders.isLoading && <FolderSkeleton />}
                  {!sidebarCollapsed && expanded && account.id === selectedExternalAccountId && externalFolders.isError && <SidebarQueryFailure label="远端文件夹读取失败" onRetry={() => { void externalFolders.refetch() }} />}
                </React.Fragment>
              )})}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>}
        {(customMailMenuItems.length > 0 || canManageFolders) && <SidebarGroup>
          {!sidebarCollapsed && (
            <div className="flex items-center justify-between px-2 py-1">
              <Button type="button" variant="ghost" className="h-auto min-w-0 justify-start gap-1 p-0 text-xs font-semibold text-muted-foreground hover:bg-transparent hover:text-foreground" aria-expanded={foldersExpanded} aria-controls="mail-sidebar-folders" onClick={() => setFoldersExpanded((value) => !value)}>
                <ChevronDown className={cn("h-3 w-3 shrink-0 transition-transform", !foldersExpanded && "-rotate-90")} />
                <span>文件夹</span>
                <UnreadSectionStatus hasUnread={customFolderUnreadCount > 0} label="文件夹有未读邮件" />
              </Button>
              {canManageFolders && (
                <Button type="button" variant="ghost" size="icon" className="h-5 w-5 text-muted-foreground hover:bg-transparent hover:text-foreground" aria-label="新建文件夹" title="新建文件夹" onClick={() => runAfterClosingMobileSidebar(() => setFolderDialogOpen(true))}>
                  <Plus className="h-3.5 w-3.5" />
                </Button>
              )}
            </div>
          )}
          {foldersExpanded && <SidebarGroupContent id="mail-sidebar-folders">
            <SidebarMenu>
              {customMailMenuItems.map((item) => (
                <SidebarMenuItem
                  key={item.key}
                  draggable={item.type === "folder" && item.custom && canOrganizeCurrentMailbox && !sidebarCollapsed}
                  onDragStart={(event) => handleFolderDragStart(event, item)}
                  onDragOver={(event) => handleFolderDragOver(event, item)}
                  onDragLeave={() => { if (folderDropTarget?.key === item.key) setFolderDropTarget(null) }}
                  onDrop={(event) => handleFolderDrop(event, item)}
                  onDragEnd={clearFolderDragState}
                  onContextMenu={(event) => openSidebarContextMenu(event, item)}
                >
                  <SidebarMenuButton
                    isActive={mailView === "folder" && folder === item.folderName}
                    className={cn(
                      "h-8 rounded-md px-2 text-[13px] font-normal",
                      sidebarCollapsed && "justify-center px-0",
                      item.type === "folder" && item.custom && canOrganizeCurrentMailbox && !sidebarCollapsed && "cursor-grab active:cursor-grabbing",
                      draggingFolderId === item.folderId && "opacity-50",
                      folderDropTarget?.key === item.key && "bg-accent/60",
                      folderDropTarget?.key === item.key && folderDropTarget.edge === "before" && "border-t-2 border-t-primary",
                      folderDropTarget?.key === item.key && folderDropTarget.edge === "after" && "border-b-2 border-b-primary"
                    )}
                    onClick={() => activateSidebarItem(item)}
                  >
                    {item.icon}
                    {!sidebarCollapsed && <span className="min-w-0 flex-1 truncate">{item.label}</span>}
                    {!sidebarCollapsed && <UnreadBadge count={item.count} />}
                  </SidebarMenuButton>
                </SidebarMenuItem>
              ))}
              {!sidebarCollapsed && canOrganizeCurrentMailbox && (
                <div
                  className={cn("mx-2 h-4 rounded-sm border border-dashed border-transparent", folderDropTarget?.edge === "end" && "border-primary bg-accent/60")}
                  onDragOver={(event) => {
                    if (!draggingFolderId) return
                    event.preventDefault()
                    event.dataTransfer.dropEffect = "move"
                    setFolderDropTarget({ key: "__end__", edge: "end" })
                  }}
                  onDragLeave={() => { if (folderDropTarget?.edge === "end") setFolderDropTarget(null) }}
                  onDrop={handleFolderDropEnd}
                />
              )}
            </SidebarMenu>
          </SidebarGroupContent>}
        </SidebarGroup>}
        {(canReadMail || canManageLabels) && <SidebarGroup>
          {!sidebarCollapsed && (
            <div className="flex items-center justify-between px-2 py-1">
              <Button type="button" variant="ghost" className="h-auto min-w-0 justify-start gap-1 p-0 text-xs font-semibold text-muted-foreground hover:bg-transparent hover:text-foreground" aria-expanded={labelsExpanded} aria-controls="mail-sidebar-labels" onClick={() => setLabelsExpanded((value) => !value)}>
                <ChevronDown className={cn("h-3 w-3 shrink-0 transition-transform", !labelsExpanded && "-rotate-90")} />
                <span>标签</span>
                <UnreadSectionStatus hasUnread={labelUnreadCount > 0} label="标签有未读邮件" />
              </Button>
              {canManageCurrentMailboxLabels && (
                <div className="flex items-center gap-0.5">
                <Button type="button" variant="ghost" size="icon" className="h-5 w-5 text-muted-foreground hover:bg-transparent hover:text-foreground" aria-label="新建标签" title="新建标签" onClick={() => { setNewLabelEditing(true); setLabelEditMode(true) }}>
                  <Plus className="h-3.5 w-3.5" />
                </Button>
                {labelEditMode && (
                  <Button type="button" variant="ghost" size="icon" className="h-5 w-5 text-muted-foreground hover:bg-transparent hover:text-foreground" aria-label="完成标签编辑" title="完成编辑" onClick={() => setLabelEditMode((v) => !v)}>
                    <Check className="h-3 w-3" />
                  </Button>
                )}
                </div>
              )}
            </div>
          )}
          {labelsExpanded && <SidebarGroupContent id="mail-sidebar-labels">
            <SidebarMenu>
              {canReadMail && labelItems.map((label) => {
                const dotColor = labelDotColor(label)
                return (
                  <SidebarMenuItem key={label.id}>
                    <SidebarMenuButton
                      isActive={!labelEditMode && mailView === "label" && selectedLabelId === label.id}
                      className={cn("h-8 rounded-md px-2 text-[13px] font-normal", sidebarCollapsed && "justify-center px-0")}
                      onClick={() => { if (!labelEditMode) openLabel(label.id) }}
                    >
                      {sidebarCollapsed ? (
                        <span className="h-2 w-2 rounded-full" style={{ backgroundColor: dotColor }} />
                      ) : (
                        <span className="flex min-w-0 items-center gap-3">
                          <span className="h-2 w-2 shrink-0 rounded-full" style={{ backgroundColor: dotColor }} />
                          <span className="truncate">{label.name}</span>
                        </span>
                      )}
                      {!sidebarCollapsed && !labelEditMode && <UnreadBadge count={label.unreadCount} />}
                      {!sidebarCollapsed && labelEditMode && canManageCurrentMailboxLabels && (
                        <button
                          type="button"
                          className="ml-auto flex h-5 w-5 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive"
                          onClick={(e) => { e.stopPropagation(); deleteLabel.mutate(label.id) }}
                          disabled={deleteLabel.isPending}
                          aria-label={`删除标签 ${label.name}`}
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </button>
                      )}
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                )
              })}
              {canReadMail && !sidebarCollapsed && !labels.isLoading && !labels.isError && labelItems.length === 0 && <div className="px-2 py-1 text-xs text-muted-foreground">暂无标签</div>}
              {canManageCurrentMailboxLabels && labelEditMode && newLabelEditing && (
                <SidebarMenuItem>
                  <NewLabelButton collapsed={sidebarCollapsed} pending={createLabel.isPending} onCreate={(name) => { createLabel.mutate(name); setNewLabelEditing(false) }} editing={newLabelEditing} onEditingChange={setNewLabelEditing} />
                </SidebarMenuItem>
              )}
            </SidebarMenu>
            {labels.isLoading && <FolderSkeleton />}
            {!sidebarCollapsed && labels.isError && <SidebarQueryFailure label="标签读取失败" onRetry={() => { void labels.refetch() }} />}
          </SidebarGroupContent>}
        </SidebarGroup>}
      </SidebarContent>
      <SidebarContextMenu
        state={sidebarContextMenu}
        canCreate={canManageFolders}
        canReorder={canOrganizeCurrentMailbox}
        canDelete={canManageFolders}
        pending={reorderFolders.isPending || deleteFolder.isPending}
        onClose={closeSidebarContextMenu}
        onOpen={(item) => {
          closeSidebarContextMenu()
          activateSidebarItem(item)
        }}
        onRefresh={() => {
          closeSidebarContextMenu()
          void refreshMailData()
        }}
        onCreateFolder={() => {
          closeSidebarContextMenu()
          runAfterClosingMobileSidebar(() => setFolderDialogOpen(true))
        }}
        onMove={(item, action) => {
          closeSidebarContextMenu()
          moveSidebarFolder(item, action)
        }}
        onDelete={(item) => {
          closeSidebarContextMenu()
          runAfterClosingMobileSidebar(() => confirmDeleteFolder(item))
        }}
      />
    </Sidebar>
  )

  const mailTransferTools = isTransferView ? (
    <div className="flex shrink-0 items-center gap-0.5">
      <Button type="button" size="icon" variant="ghost" onClick={() => void exportCurrentMail()} disabled={!canExportCurrentView || exportingMail} className="h-8 w-8 text-muted-foreground hover:text-foreground" title={selectedCountOnPage > 0 ? `下载选中的 ${selectedCountOnPage} 封邮件` : "导出当前邮箱邮件为 ZIP"} aria-label={selectedCountOnPage > 0 ? `下载选中的 ${selectedCountOnPage} 封邮件` : "导出当前邮箱邮件为 ZIP"}>
        <Download className={cn("h-4 w-4", exportingMail && "animate-pulse")} />
      </Button>
      {mailView !== "unknown" && (
        <Button type="button" size="icon" variant="ghost" onClick={chooseMailImport} disabled={importingMail} className="h-8 w-8 text-muted-foreground hover:text-foreground disabled:pointer-events-auto" title={isAllMailboxSelected ? "请先选择一个邮箱后导入" : "导入邮件（EML/MBOX）"} aria-label="导入邮件（EML/MBOX）">
          <Upload className={cn("h-4 w-4", importingMail && "animate-pulse")} />
        </Button>
      )}
      <Button type="button" size="icon" variant="ghost" onClick={() => void refreshMail()} disabled={refreshing || autoRefreshing} className={cn("h-8 w-8 text-muted-foreground hover:text-foreground", (refreshing || autoRefreshing) && "text-primary")} title={autoRefreshing ? "自动刷新中" : "刷新当前视图"} aria-label="刷新当前视图">
        <RefreshCcw className={cn("h-4 w-4", (refreshing || autoRefreshing) && "animate-spin")} />
      </Button>
    </div>
  ) : null

  const contentView = !canAccessMail ? (
    <PermissionEmptyState title="无邮箱前台权限" description="当前账号未开启邮箱前台访问权限。" onOpenSettings={openSettings} />
  ) : !canReadMail ? (
    <PermissionEmptyState title="无邮件查看权限" description="当前账号可以访问邮箱前台，但未开启邮件查看权限。" onOpenSettings={openSettings} />
  ) : mailboxList.isError ? (
    <MailQueryFailure error={mailboxList.error} onRetry={() => { void mailboxList.refetch() }} />
  ) : !mailboxList.isLoading && !hasMailboxes && mailView !== "unknown" ? (
    <NoMailboxState onManageMailboxes={() => navigate("/profile?tab=mailboxes")} />
  ) : mailView === "scheduled" && scheduledSends.isError ? (
    <MailQueryFailure error={scheduledSends.error} onRetry={() => { void scheduledSends.refetch() }} />
  ) : mailView === "scheduled" && canScheduleMail ? (
    <ScheduledSendView
      compact={compactMailLayout}
      items={visibleScheduledItems}
      total={scheduledItems.length}
      loading={scheduledSends.isLoading}
      query={query}
      cancelingId={cancelingScheduledId}
      onCancel={(item) => cancelScheduledSend.mutate(item)}
    />
  ) : mailView === "scheduled" ? (
    <PermissionEmptyState title="无定时发送权限" description="当前账号不能查看或管理定时发送任务。" onOpenSettings={openSettings} />
  ) : mailView === "sendQueue" && sendQueue.isError ? (
    <MailQueryFailure error={sendQueue.error} onRetry={() => { void sendQueue.refetch() }} />
  ) : mailView === "sendQueue" && canViewSendQueue ? (
    <SendQueueView
      compact={compactMailLayout}
      items={visibleSendQueueItems}
      total={sendQueueItems.length}
      loading={sendQueue.isLoading}
      status={sendQueueStatus}
      messageId={sendQueueMessageId}
      recipient={sendQueueRecipient}
      from={sendQueueFrom}
      to={sendQueueTo}
      pendingId={sendQueuePendingId}
      onStatusChange={setSendQueueStatus}
      onMessageIdChange={setSendQueueMessageId}
      onRecipientChange={setSendQueueRecipient}
      onFromChange={setSendQueueFrom}
      onToChange={setSendQueueTo}
      onClearFilters={() => { setSendQueueMessageId(""); setSendQueueRecipient(""); setSendQueueFrom(""); setSendQueueTo(""); setSendQueueStatus("all") }}
      onRetry={(item) => retrySendQueue.mutate(item)}
      onCancel={(item) => cancelSendQueue.mutate(item)}
      onAudit={(item) => setSendQueueAuditId(item.id)}
      canMutate={canSendMail}
    />
  ) : mailView === "sendQueue" ? (
    <PermissionEmptyState title="无发送队列权限" description="当前账号不能查看发送队列。" onOpenSettings={openSettings} />
  ) : mailMessagesError ? (
    <MailQueryFailure error={mailMessagesError} onRetry={() => { void refetchMessages() }} />
  ) : compactMailLayout ? (
    <CompactMailView
      key={`${mailView}:${mailView === "folder" ? folder : mailView === "label" ? selectedLabelId : selectedExternalAccountId}`}
      title={viewTitle}
      currentFolder={mailView === "folder" ? folder : undefined}
      icon={mailView === "label" && selectedLabel ? <Badge variant="outline" className="gap-1.5 rounded-md font-normal"><span className="h-2 w-2 shrink-0 rounded-full" style={{ backgroundColor: labelDotColor(selectedLabel) }} />{selectedLabel.name}</Badge> : undefined}
      messages={visibleMessages}
      total={allMessages.length}
      selectedIds={compactSelectedIds}
      allSelected={compactAllSelected}
      someSelected={compactSomeSelected}
      loading={mailMessagesLoading}
      hasMore={hasMoreMessages}
      loadingMore={mailMessagesLoadingMore}
      onLoadMore={loadMoreMessages}
      emptyMessage={emptyMessage}
      selectedId={selectedId}
      selected={selected}
      detailLoading={detail.isLoading}
      detailError={detail.error}
      onRetryDetail={() => { void detail.refetch() }}
      labels={labelItems}
      labelPending={addLabel.isPending || removeLabel.isPending}
      onSelect={openMessage}
      onSelectAll={toggleCompactSelectAll}
      onToggleSelected={toggleCompactSelect}
      scheduledDraftIds={scheduledDraftIds}
      onCloseReader={() => setSelectedId(null)}
      onStar={(message) => { if (mailView !== "external") star.mutate({ id: message.id, starred: !message.isStarred }) }}
      onReply={openReply}
      onForward={openForward}
      onSendTimeline={openMessageSendTimeline}
      onArchive={(message) => { if (mailView !== "external") move.mutate({ id: message.id, folder: message.folder === "Archive" ? "Inbox" : "Archive" }) }}
      onDelete={(message) => { if (mailView !== "external") confirmDeleteMessage(message) }}
      onToggleRead={(message) => mailView === "external" && selectedExternalAccountId ? markExternalRead.mutate({ id: selectedExternalAccountId, remoteId: message.id, read: !message.isRead }) : markRead.mutate({ id: message.id, read: !message.isRead })}
      onAddLabel={(message, label) => addLabel.mutate({ id: message.id, label })}
      onRemoveLabel={(message, labelId) => removeLabel.mutate({ id: message.id, labelId })}
      bulkPending={bulkPending}
        onBulkAction={runBulkAction}
        onContextMenu={openMessageContextMenu}
        canSend={canSendMail}
      canOrganize={canOrganizeMail && mailView !== "external" && mailView !== "unknown"}
      canManageLabels={canManageLabels && mailView !== "external" && mailView !== "unknown"}
      canDownloadAttachments={canDownloadAttachments}
      language={language}
      tools={!isMobile ? mailTransferTools : undefined}
    />
  ) : (
    <div className={cn("mail-content-grid min-h-0 flex-1 bg-background", selectedId && "is-reading")}>
      <div className={cn("mail-list-pane min-w-0", selectedId && "max-[767px]:hidden")}>
        <div className="flex h-full min-h-0 flex-col bg-background">
          <div className="shrink-0 border-b px-3 pb-2.5 pt-2.5">
            <div className="relative">
              <Search className="absolute left-2.5 top-2.5 h-3.5 w-3.5 text-muted-foreground" />
              <Input value={query} onChange={(e) => setQuery(e.target.value)} onKeyDown={handleSearchKeyDown} placeholder="搜索发件人、主题、内容..." className="h-9 rounded-md bg-background pl-8 text-[13px] shadow-none" />
            </div>
            <div className="relative mt-2">
              <Button type="button" variant={advancedSearchActive || advancedSearchOpen ? "secondary" : "outline"} size="sm" className="h-7 rounded-md px-2 text-xs font-normal shadow-none" onClick={toggleAdvancedSearch}>
                <SlidersHorizontal className="h-3.5 w-3.5" />
                高级搜索
              </Button>
              {advancedSearchOpen && (
                <AdvancedSearchPanel
                  draft={advancedSearchDraft}
                  onDraftChange={setAdvancedSearchDraft}
                  onSubmit={applyAdvancedSearch}
                  onClear={clearAdvancedSearch}
                />
              )}
            </div>
            {advancedSearchChips.length > 0 && (
              <div className="mt-2 flex flex-wrap items-center gap-1.5">
                {advancedSearchChips.map((chip) => (
                  <SearchFilterChip key={chip.key} label={chip.label} onRemove={() => removeAdvancedSearchFilter(chip.key)} />
                ))}
                <Button type="button" variant="ghost" className="h-6 rounded-full px-2 text-xs font-normal text-muted-foreground hover:bg-transparent hover:text-foreground" onClick={clearAdvancedSearch}>清空</Button>
              </div>
            )}
            <div className="mt-2.5 flex min-w-0 items-center gap-1.5 overflow-hidden text-xs text-muted-foreground">
              <span className="shrink-0">快捷筛选</span>
              {(["attachments", "starred", "recent7"] as MailFilter[]).map((value) => (
                <Button
                  key={value}
                  type="button"
                  variant={mailFilter === value ? "secondary" : "outline"}
                  size="sm"
                  className={cn("h-7 shrink-0 rounded-full px-2.5 text-xs font-normal shadow-none", mailFilter === value && "bg-accent text-accent-foreground")}
                  onClick={() => setMailFilter(mailFilter === value ? "all" : value)}
                >
                  {filterLabels[value]}
                </Button>
              ))}
            </div>
          </div>
          <div className="flex min-h-14 shrink-0 items-center justify-between gap-2 border-b px-3 py-2">
            <div className="flex min-w-0 items-center gap-2">
              <div className="flex items-center gap-1">
                <Checkbox aria-label="选择当前页邮件" checked={compactAllSelected ? true : compactSomeSelected ? "indeterminate" : false} onCheckedChange={(value) => toggleCompactSelectAll(value === true)} />
                <ChevronDown className="h-3.5 w-3.5 text-muted-foreground" />
              </div>
              <div className="flex min-w-0 items-center gap-2">
                <h1 key={viewTitle} className="min-w-0 truncate text-xl font-bold leading-none text-foreground">{mailView === "label" && selectedLabel ? selectedLabel.name : viewTitle}</h1>
                {mailTransferTools}
              </div>
            </div>
            <div className="flex min-w-0 shrink-0 items-center gap-1.5">
              <Button type="button" variant={mailFilter === "all" ? "secondary" : "outline"} size="sm" className="h-8 rounded-md px-3 text-[13px] font-normal shadow-none" onClick={() => setMailFilter("all")}>全部</Button>
              <Button type="button" variant={mailFilter === "unread" ? "secondary" : "outline"} size="sm" className="h-8 rounded-md px-3 text-[13px] font-normal shadow-none" onClick={() => setMailFilter("unread")}>未读</Button>
            </div>
          </div>
          {selectedCountOnPage > 0 && canOrganizeMail && mailView !== "unknown" && (
            <div className="flex min-h-10 shrink-0 items-center gap-3 border-b px-3 py-1.5">
              <span className="shrink-0 text-[13px] text-muted-foreground">已选 {selectedCountOnPage} 封</span>
              <BulkActionToolbar pending={bulkPending} currentFolder={mailView === "folder" ? folder : undefined} folders={folders.data?.items || []} readAction={bulkReadAction} onAction={runBulkAction} onMoveToFolder={runBulkMoveToFolder} />
            </div>
          )}
          <ScrollArea className="min-h-0 flex-1">
            {mailMessagesLoading && <MessageSkeleton />}
            {visibleMessages.map((m) => <MessageRow key={m.id} message={m} active={selectedId === m.id} checked={compactSelectedIds.includes(m.id)} scheduled={scheduledDraftIds.has(m.id)} onCheckedChange={(checked) => toggleCompactSelect(m.id, checked)} onClick={() => openMessage(m.id)} onContextMenu={(event) => openMessageContextMenu(event, m)} onStar={() => star.mutate({ id: m.id, starred: !m.isStarred })} onArchive={() => move.mutate({ id: m.id, folder: m.folder === "Archive" ? "Inbox" : "Archive" })} onTrash={() => m.folder === "Trash" ? confirmDeleteMessage(m) : move.mutate({ id: m.id, folder: "Trash" })} onToggleRead={() => markRead.mutate({ id: m.id, read: !m.isRead })} canOrganize={canOrganizeMail && mailView !== "unknown"} />)}
            {!mailMessagesLoading && visibleMessages.length === 0 && <div className="grid min-h-[170px] place-items-center px-8 py-12 text-center text-base text-muted-foreground">{emptyMessage}</div>}
            {!mailMessagesLoading && hasMoreMessages && (
              <div className="border-b p-4 text-center">
                <Button variant="outline" size="sm" disabled={!canLoadMore} onClick={loadMoreMessages}>
                  {mailMessagesLoadingMore ? "加载中..." : "加载更多"}
                </Button>
              </div>
            )}
          </ScrollArea>
        </div>
      </div>

      <section className={cn("mail-detail-pane min-w-0", !selectedId && "max-[767px]:hidden")}>
        <div className="h-full min-h-0 bg-background">
          {!selectedId && (
            <div className="grid h-full place-items-center text-muted-foreground">
              <div className="flex flex-col items-center gap-4 text-center">
                <Mail className="h-12 w-12 stroke-[1.6] text-muted-foreground/60" />
                <div className="text-base">选择一封邮件以查看详情</div>
              </div>
            </div>
          )}
          {detail.isLoading && <div className="space-y-4 p-6"><Skeleton className="h-8 w-2/3" /><Skeleton className="h-4 w-1/3" /><Separator /><Skeleton className="h-40 w-full" /></div>}
          {detail.isError && <MailDetailFailure error={detail.error} onRetry={() => { void detail.refetch() }} />}
          {!detail.isError && selected && <div className="flex h-full min-h-0 flex-col">
            <div className="border-b p-5">
              {isTwoPaneMailViewport && (
                <Button variant="ghost" size="sm" className="-ml-2 mb-3 h-8 px-2 text-[13px] font-normal" onClick={() => setSelectedId(null)}>
                  <ArrowLeft className="h-4 w-4" />
                  返回列表
                </Button>
              )}
              <div className="mb-4 flex items-center justify-between gap-3">
                <h2 className="text-xl font-semibold">{selected.subject}</h2>
                <div className="flex flex-wrap justify-end gap-2">
                  {canSendMail && <Button variant="outline" size="sm" onClick={() => openReply(selected)}><Reply className="h-4 w-4" />回复</Button>}
                  {canSendMail && <Button variant="outline" size="sm" onClick={() => openForward(selected)}><Forward className="h-4 w-4" />转发</Button>}
                  {mailView !== "external" && mailView !== "unknown" && selected.sendQueueId && <Button variant="outline" size="sm" onClick={() => openMessageSendTimeline(selected)}><History className="h-4 w-4" />投递时间线</Button>}
                  {mailView !== "external" && mailView !== "unknown" && canOrganizeMail && (selected.folder === "Archive" ? (
                    <Button variant="outline" size="sm" onClick={() => move.mutate({ id: selected.id, folder: "Inbox" })}>取消归档</Button>
                  ) : (
                    <Button variant="outline" size="sm" onClick={() => move.mutate({ id: selected.id, folder: "Archive" })}>归档</Button>
                  ))}
                  {mailView !== "external" && mailView !== "unknown" && canOrganizeMail && <Button variant="destructive" size="sm" onClick={() => confirmDeleteMessage(selected)}>{selected.folder === "Trash" ? "永久删除" : "移入已删除"}</Button>}
                </div>
              </div>
              <MessageMetaPanel
                message={selected}
                {...(canManageLabels && mailView !== "unknown" ? { availableLabels: labelItems, onAddLabel: (label: MailLabel) => addLabel.mutate({ id: selected.id, label }), onRemoveLabel: (labelId: string) => removeLabel.mutate({ id: selected.id, labelId }), labelPending: addLabel.isPending || removeLabel.isPending } : {})}
              />
            </div>
            <ScrollArea className="min-h-0 flex-1">
              <div className="p-6">
                <TranslatableMailBody message={selected} language={language} />
                {selected.attachments && selected.attachments.length > 0 && <div className="mt-8 rounded-lg border p-4"><div className="mb-3 font-medium">附件</div><div className="space-y-2">{selected.attachments.map((a) => canDownloadAttachments ? <a className="flex items-center justify-between rounded-md border p-3 text-sm hover:bg-accent" href={attachmentHref(selected, a.id)} key={a.id}><span className="flex items-center gap-2"><Paperclip className="h-4 w-4" />{a.filename}</span><span className="text-muted-foreground">{formatBytes(a.sizeBytes)}</span></a> : <div className="flex items-center justify-between rounded-md border p-3 text-sm text-muted-foreground" key={a.id}><span className="flex items-center gap-2"><Paperclip className="h-4 w-4" />{a.filename}</span><span>{formatBytes(a.sizeBytes)}</span></div>)}</div></div>}
              </div>
            </ScrollArea>
          </div>}
        </div>
      </section>
    </div>
  )

  return (
    <div className="h-svh overflow-hidden bg-background">
      <SidebarProvider className="h-full min-h-0 w-full min-w-0 flex-col">
        {isMobile ? (
          <div className="flex h-full min-h-0 flex-col">
            {!selectedId && (
              <header className="flex min-h-14 shrink-0 flex-wrap items-center gap-2 border-b px-3 py-2">
                <Sheet open={mobileSidebarOpen} onOpenChange={setMobileSidebarOpen}>
                  <SheetTrigger asChild>
                    <Button size="icon" variant="ghost" aria-label="打开导航"><PanelLeftOpen className="h-4 w-4" /></Button>
                  </SheetTrigger>
                  <SheetContent side="left" className="w-[86vw] max-w-80 p-0 [&>button]:hidden" aria-describedby={undefined}>
                    <SheetTitle className="sr-only">邮箱导航</SheetTitle>
                    <div className="h-svh min-h-0 overflow-hidden">{sidebarContent}</div>
                  </SheetContent>
                </Sheet>
                <div className="min-w-0 flex-1 text-sm font-semibold">{mailView === "label" && selectedLabel ? <Badge variant="outline" className="gap-1.5 rounded-md font-normal"><span className="h-2 w-2 shrink-0 rounded-full" style={{ backgroundColor: labelDotColor(selectedLabel) }} />{selectedLabel.name}</Badge> : viewTitle}</div>
                {mailTransferTools}
                  {canSendMail && <Button type="button" size="icon" onClick={() => openCompose()} disabled={!selectedComposeMailbox} aria-label="写邮件"><PencilLine className="h-4 w-4" /></Button>}
                <div className="relative basis-full">
                  <Search className="absolute left-3 top-2.5 h-4 w-4 text-muted-foreground" />
                  <Input value={query} onChange={(e) => setQuery(e.target.value)} placeholder={mailView === "external" ? "搜索远端邮件" : mailView === "sendQueue" ? "搜索发送队列" : mailView === "scheduled" ? "搜索待发送" : "搜索邮件"} className="h-10 pl-9" />
                </div>
              </header>
            )}
            <section className="flex min-h-0 flex-1 flex-col">{contentView}</section>
          </div>
        ) : (
          <div className="mail-shell-grid h-full min-h-0 w-full min-w-0 overflow-hidden">
            <div className="h-full min-h-0 min-w-0 overflow-hidden">
              {sidebarContent}
            </div>
            <section className="flex h-full min-h-0 min-w-0 flex-col">
              {contentView}
            </section>
          </div>
        )}
      </SidebarProvider>

      <ComposeDialog mailboxes={composeMailboxes} mailbox={selectedComposeMailbox} open={composeOpen} draft={composeDraft} limits={user?.limits} canSend={canSendMail} canManageDrafts={canManageDrafts} canSchedule={canScheduleMail} canManageSignatures={canManageSignatures} onOpenChange={(open) => { setComposeOpen(open); if (!open) setComposeDraft(undefined) }} onSent={() => { setComposeOpen(false); setComposeDraft(undefined); qc.invalidateQueries({ queryKey: ["messages"] }); qc.invalidateQueries({ queryKey: ["folders"] }); qc.invalidateQueries({ queryKey: ["mail-stats"] }); qc.invalidateQueries({ queryKey: ["labels"] }); qc.invalidateQueries({ queryKey: ["scheduled-sends"] }); qc.invalidateQueries({ queryKey: ["send-queue"] }) }} />
      <Input ref={mailImportInputRef} type="file" accept=".eml,.mbox,message/rfc822,application/mbox" multiple className="hidden" onChange={importSelectedMailFiles} />
      <SendQueueAuditDialog
        open={!!sendQueueAuditId}
        loading={sendQueueAudit.isLoading}
        error={sendQueueAudit.error}
        events={sendQueueAudit.data?.items || []}
        onRetry={() => { void sendQueueAudit.refetch() }}
        onOpenChange={(open) => { if (!open) setSendQueueAuditId("") }}
      />
      <MessageContextMenu
        state={messageContextMenu}
        labels={labelItems}
        folders={folders.data?.items || []}
        canSend={canSendMail}
        canOrganize={canOrganizeMail}
        canManageLabels={canManageLabels}
        labelPending={addLabel.isPending || removeLabel.isPending}
        onClose={closeMessageContextMenu}
        onAction={runMessageContextAction}
        onMoveToFolder={(message, folderName) => {
          closeMessageContextMenu()
          move.mutate({ id: message.id, folder: folderName })
        }}
        onToggleLabel={(message, label) => {
          const active = (message.labels || []).some((item) => item.id === label.id)
          active ? removeLabel.mutate({ id: message.id, labelId: label.id }) : addLabel.mutate({ id: message.id, label })
        }}
      />
      <CreateFolderDialog
        open={folderDialogOpen}
        pending={createFolder.isPending}
        scope={isAllMailboxSelected ? "全部邮箱" : selectedMailbox?.address || "当前邮箱"}
        onOpenChange={setFolderDialogOpen}
        onCreate={(payload) => createFolder.mutate(payload)}
      />
      <ConfirmDialog
        open={!!pendingConfirm}
        title={pendingConfirm?.title || ""}
        description={pendingConfirm?.description}
        confirmText={pendingConfirm?.confirmText || "确认"}
        destructive
        pending={del.isPending || bulkPending || deleteFolder.isPending}
        onOpenChange={(open) => { if (!open) setPendingConfirm(null) }}
        onConfirm={() => pendingConfirm?.onConfirm()}
      />
    </div>
  )
}

function buildMailMenuItems(folders: MailFolder[], starredCount: number, scheduledCount: number, includeScheduled: boolean, sendQueueCount: number, includeSendQueue: boolean, includeUnknown: boolean): MailMenuItem[] {
  const byName = new Map(folders.map((item) => [item.name, item]))
  const normalizedFolders = ["Inbox", "Drafts", "Sent", "Archive", "Spam", "Trash"].map((name) => byName.get(name) || { id: `virtual-${name}`, name, role: name.toLowerCase(), icon: "folder", sortOrder: 0, unreadCount: 0, totalCount: 0, uidValidity: 0, uidNext: 1, highestModseq: 1 })
  for (const item of folders) {
    if (!normalizedFolders.some((folder) => folder.name === item.name)) normalizedFolders.push(item)
  }
  const folderItems: MailMenuItem[] = normalizedFolders.map((item) => ({
    type: "folder",
    key: item.id,
    folderId: item.id,
    folderName: item.name,
    label: folderLabels[item.name] || item.name,
    icon: isCustomMailFolder(item) ? customFolderIcon(item.icon) : folderIcons[item.role] || <Inbox className="h-4 w-4" />,
    count: item.name === "Drafts" ? item.totalCount : item.unreadCount,
    custom: isCustomMailFolder(item),
    order: isCustomMailFolder(item) ? item.sortOrder || 100000 : menuAnchorOrder(item.name),
  }))
  const starredItem: MailMenuItem = { type: "starred", key: "starred", label: "星标邮件", icon: <Star className="h-4 w-4" />, count: starredCount, order: 6000 }
  const scheduledItem: MailMenuItem = { type: "scheduled", key: "scheduled", label: "稍后提醒", icon: <Clock3 className="h-4 w-4" />, count: scheduledCount, order: 7000 }
  const unknownItem: MailMenuItem = { type: "unknown", key: "unknown", label: "未知收件", icon: <MailQuestion className="h-4 w-4" />, count: 0, order: 8000 }
  const sendQueueItem: MailMenuItem = { type: "sendQueue", key: "send-queue", label: "发送队列", icon: <History className="h-4 w-4" />, count: sendQueueCount, order: 10000 }
  const specialItems: MailMenuItem[] = [starredItem]
  if (includeScheduled) specialItems.push(scheduledItem)
  if (includeUnknown) specialItems.push(unknownItem)
  if (includeSendQueue && sendQueueCount > 0) specialItems.push(sendQueueItem)
  return [...folderItems, ...specialItems].sort((a, b) => a.order - b.order || a.label.localeCompare(b.label))
}

function isCustomMailFolder(folder: Pick<MailFolder, "name" | "id">) {
  return !folder.id.startsWith("virtual-") && !["inbox", "sent", "drafts", "archive", "spam", "trash"].includes(folder.name.trim().toLowerCase())
}

function customFolderIcon(iconKey: string | undefined, className = "h-4 w-4") {
  if (iconKey?.startsWith("data:image/png;base64,")) return <img src={iconKey} alt="" className={cn("shrink-0 object-contain", className)} />
  const option = customFolderIconOptions.find((item) => item.key === iconKey) || customFolderIconOptions[0]
  const Icon = option.icon
  return <Icon className={className} />
}

function compareMailFolders(a: MailFolder, b: MailFolder) {
  return (isCustomMailFolder(a) ? a.sortOrder || 100000 : menuAnchorOrder(a.name)) - (isCustomMailFolder(b) ? b.sortOrder || 100000 : menuAnchorOrder(b.name)) || a.name.localeCompare(b.name)
}

function assignCustomFolderOrders(items: MailMenuItem[]) {
  const out: { id: string; sortOrder: number }[] = []
  for (let i = 0; i < items.length; i++) {
    const item = items[i]
    if (!isCustomMenuFolder(item)) continue
    let start = 0
    for (let j = i - 1; j >= 0; j--) {
      if (!isCustomMenuFolder(items[j])) {
        start = items[j].order
        break
      }
    }
    const groupStart = i
    let groupEnd = i
    while (groupEnd + 1 < items.length && isCustomMenuFolder(items[groupEnd + 1])) groupEnd++
    let end = start + (groupEnd - groupStart + 2) * 1000
    for (let j = groupEnd + 1; j < items.length; j++) {
      if (!isCustomMenuFolder(items[j])) {
        end = items[j].order
        break
      }
    }
    const step = Math.max(1, Math.floor((end - start) / (groupEnd - groupStart + 2)))
    for (let j = groupStart; j <= groupEnd; j++) {
      const folder = items[j] as Extract<MailMenuItem, { type: "folder" }>
      out.push({ id: folder.folderId, sortOrder: start + step * (j - groupStart + 1) })
    }
    i = groupEnd
  }
  return out
}

function isCustomMenuFolder(item: MailMenuItem): item is Extract<MailMenuItem, { type: "folder" }> {
  return item.type === "folder" && item.custom
}

function hasAdvancedMailSearch(search: AdvancedMailSearch) {
  return Boolean(
    search.from.trim() ||
    search.to.trim() ||
    search.subject.trim() ||
    search.startDate ||
    search.endDate ||
    search.hasAttachments ||
    search.unread ||
    search.starred
  )
}

function messageMatchesAdvancedSearch(message: MailMessage, search: AdvancedMailSearch) {
  if (!hasAdvancedMailSearch(search)) return true
  if (search.from.trim() && !searchTextMatches([message.from, message.fromName || ""], search.from)) return false
  if (search.to.trim() && !searchTextMatches([...(message.to || []), ...(message.cc || []), ...(message.bcc || []), message.recipientAddress || "", message.mailboxAddress || ""], search.to)) return false
  if (search.subject.trim() && !searchTextMatches([message.subject || ""], search.subject)) return false
  if (search.hasAttachments && !message.hasAttachments) return false
  if (search.unread && message.isRead) return false
  if (search.starred && !message.isStarred) return false
  const receivedAt = new Date(message.receivedAt).getTime()
  if (search.startDate) {
    const start = new Date(`${search.startDate}T00:00:00`).getTime()
    if (Number.isFinite(start) && (!Number.isFinite(receivedAt) || receivedAt < start)) return false
  }
  if (search.endDate) {
    const end = new Date(`${search.endDate}T23:59:59.999`).getTime()
    if (Number.isFinite(end) && (!Number.isFinite(receivedAt) || receivedAt > end)) return false
  }
  return true
}

function buildAdvancedSearchChips(search: AdvancedMailSearch): AdvancedSearchChip[] {
  const chips: AdvancedSearchChip[] = []
  if (search.from.trim()) chips.push({ key: "from", label: `发件人: ${search.from.trim()}` })
  if (search.to.trim()) chips.push({ key: "to", label: `收件人: ${search.to.trim()}` })
  if (search.subject.trim()) chips.push({ key: "subject", label: `主题: ${search.subject.trim()}` })
  if (search.startDate) chips.push({ key: "startDate", label: `开始: ${search.startDate}` })
  if (search.endDate) chips.push({ key: "endDate", label: `结束: ${search.endDate}` })
  if (search.hasAttachments) chips.push({ key: "hasAttachments", label: "有附件" })
  if (search.unread) chips.push({ key: "unread", label: "未读" })
  if (search.starred) chips.push({ key: "starred", label: "星标" })
  return chips
}

function parseSmartSearchQuery(raw: string): { active: boolean; query: string; search: Partial<AdvancedMailSearch> } {
  const search: Partial<AdvancedMailSearch> = {}
  let active = false
  const query = raw.replace(/\b(from|to|subject|after|before|has|is):(?:"([^"]*)"|'([^']*)'|(\S+))/gi, (match, key: string, quoted: string, singleQuoted: string, bare: string) => {
    const value = (quoted || singleQuoted || bare || "").trim()
    const normalizedKey = key.toLowerCase()
    const normalizedValue = value.toLowerCase()
    if (!value) return match
    if (normalizedKey === "from") search.from = value
    else if (normalizedKey === "to") search.to = value
    else if (normalizedKey === "subject") search.subject = value
    else if (normalizedKey === "after" && isDateInputValue(value)) search.startDate = value
    else if (normalizedKey === "before" && isDateInputValue(value)) search.endDate = value
    else if (normalizedKey === "has" && ["attachment", "attachments", "file", "files"].includes(normalizedValue)) search.hasAttachments = true
    else if (normalizedKey === "is" && ["unread", "starred"].includes(normalizedValue)) {
      if (normalizedValue === "unread") search.unread = true
      if (normalizedValue === "starred") search.starred = true
    } else {
      return match
    }
    active = true
    return " "
  }).replace(/\s+/g, " ").trim()
  return { active, query, search }
}

function isDateInputValue(value: string) {
  return /^\d{4}-\d{2}-\d{2}$/.test(value)
}

function searchTextMatches(values: string[], needle: string) {
  const normalized = needle.trim().toLowerCase()
  if (!normalized) return true
  return values.some((value) => value.toLowerCase().includes(normalized))
}

function menuAnchorOrder(name: string) {
  switch (name) {
    case "Inbox": return 1000
    case "Drafts": return 2000
    case "Sent": return 3000
    case "Archive": return 4000
    case "Trash": return 5000
    case "Spam": return 9000
    default: return 100000
  }
}

function AdvancedSearchPanel({ draft, onDraftChange, onSubmit, onClear }: { draft: AdvancedMailSearchDraft; onDraftChange: (draft: AdvancedMailSearchDraft) => void; onSubmit: (draft: AdvancedMailSearchDraft) => void; onClear: () => void }) {
  const update = <K extends keyof AdvancedMailSearchDraft>(key: K, value: AdvancedMailSearchDraft[K]) => onDraftChange({ ...draft, [key]: value })
  return (
    <form
      className="absolute left-0 top-full z-50 mt-2 w-full rounded-md border bg-popover p-3 text-xs text-popover-foreground shadow-md"
      onSubmit={(event) => {
        event.preventDefault()
        onSubmit(draft)
      }}
    >
      <div className="grid grid-cols-2 gap-x-2 gap-y-3">
        <AdvancedSearchField label="发件人">
          <Input value={draft.from} onChange={(event) => update("from", event.target.value)} placeholder="邮箱或名称" className="h-8 rounded-md bg-background px-2.5 text-[13px] shadow-none" />
        </AdvancedSearchField>
        <AdvancedSearchField label="收件人">
          <Input value={draft.to} onChange={(event) => update("to", event.target.value)} placeholder="邮箱地址" className="h-8 rounded-md bg-background px-2.5 text-[13px] shadow-none" />
        </AdvancedSearchField>
        <AdvancedSearchField label="主题" className="col-span-2">
          <Input value={draft.subject} onChange={(event) => update("subject", event.target.value)} placeholder="主题关键词" className="h-8 rounded-md bg-background px-2.5 text-[13px] shadow-none" />
        </AdvancedSearchField>
        <AdvancedSearchField label="开始日期">
          <Input type="date" value={draft.startDate} onChange={(event) => update("startDate", event.target.value)} className="h-8 rounded-md bg-background px-2.5 text-[13px] shadow-none" />
        </AdvancedSearchField>
        <AdvancedSearchField label="结束日期">
          <Input type="date" value={draft.endDate} onChange={(event) => update("endDate", event.target.value)} className="h-8 rounded-md bg-background px-2.5 text-[13px] shadow-none" />
        </AdvancedSearchField>
      </div>
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <AdvancedSearchToggle checked={draft.hasAttachments} onClick={() => update("hasAttachments", !draft.hasAttachments)}>有附件</AdvancedSearchToggle>
        <AdvancedSearchToggle checked={draft.unread} onClick={() => update("unread", !draft.unread)}>未读</AdvancedSearchToggle>
        <AdvancedSearchToggle checked={draft.starred} onClick={() => update("starred", !draft.starred)}>星标</AdvancedSearchToggle>
      </div>
      <div className="mt-3 flex items-center justify-between gap-2 border-t pt-3">
        <Button type="button" variant="ghost" className="h-8 px-2 text-xs font-normal text-muted-foreground hover:bg-transparent hover:text-foreground" onClick={onClear}>清空条件</Button>
        <div className="flex shrink-0 items-center gap-2">
          <Button type="submit" className="h-8 rounded-md px-3 text-xs font-medium shadow-none">应用</Button>
        </div>
      </div>
    </form>
  )
}

function AdvancedSearchField({ label, children, className }: { label: string; children: React.ReactNode; className?: string }) {
  return (
    <label className={cn("grid gap-1.5 text-xs font-normal text-muted-foreground", className)}>
      <span className="leading-none">{label}</span>
      {children}
    </label>
  )
}

function AdvancedSearchToggle({ checked, onClick, children }: { checked: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <Button type="button" variant={checked ? "secondary" : "outline"} className={cn("h-7 rounded-full px-3 text-xs font-normal shadow-none", checked && "bg-accent text-accent-foreground")} onClick={onClick}>
      {children}
    </Button>
  )
}

function SearchFilterChip({ label, onRemove }: { label: string; onRemove: () => void }) {
  return (
    <span className="inline-flex h-6 max-w-full items-center gap-1 rounded-full border bg-accent px-2 text-xs text-accent-foreground">
      <span className="truncate">{label}</span>
      <Button type="button" variant="ghost" size="icon" className="h-4 w-4 shrink-0 rounded-full p-0 text-muted-foreground shadow-none hover:bg-background hover:text-foreground" onClick={onRemove} aria-label={`移除筛选 ${label}`}>
        <X className="h-3 w-3" />
      </Button>
    </span>
  )
}

function FolderSkeleton() { return <div className="space-y-2 p-2"><Skeleton className="h-8 w-full" /><Skeleton className="h-8 w-4/5" /><Skeleton className="h-8 w-3/4" /></div> }
function MessageSkeleton() { return <div className="space-y-0">{Array.from({ length: 6 }).map((_, i) => <div className="space-y-2 border-b p-4" key={i}><Skeleton className="h-4 w-1/2" /><Skeleton className="h-4 w-4/5" /><Skeleton className="h-3 w-full" /></div>)}</div> }

function getEmptyMessage(mailView: MailView, folder: string, total: number) {
  if (mailView === "external") return total === 0 ? "远端文件夹没有邮件" : "当前筛选条件下没有远端邮件"
  if (mailView === "unknown") return total === 0 ? "暂无未知收件" : "当前筛选条件下没有邮件"
  if (mailView === "scheduled") return total === 0 ? "没有待发送邮件" : "当前搜索没有匹配的定时邮件"
  if (mailView === "sendQueue") return total === 0 ? "发送队列为空" : "当前搜索没有匹配的发送任务"
  if (total > 0) return "当前筛选条件下没有邮件"
  if (mailView === "starred") return "暂无星标邮件"
  if (mailView === "label") return "当前标签没有邮件"
  if (folder === "Inbox") return "暂无邮件"
  if (folder === "Drafts") return "还没有草稿"
  if (folder === "Sent") return "还没有已发送邮件"
  if (folder === "Trash") return "回收站是空的"
  if (folder === "Spam") return "暂无垃圾邮件"
  return "当前文件夹没有邮件"
}

function externalAccountLabel(account: ExternalImapAccount) {
  return account.oauthEmail || account.username || account.name || "外部邮箱"
}

function externalAccountSubtitle(account: ExternalImapAccount) {
  const label = externalAccountLabel(account)
  const mode = account.storageMode === "local" ? "同步" : "直连"
  const name = account.name && account.name !== label ? account.name : ""
  return [name, account.host, mode].filter(Boolean).join(" · ")
}

function NoMailboxState({ onManageMailboxes }: { onManageMailboxes: () => void }) {
  return (
    <div className="grid min-h-0 flex-1 place-items-center p-6">
      <div className="w-full max-w-md rounded-lg border border-dashed p-8 text-center">
        <div className="mx-auto mb-4 grid size-12 place-items-center rounded-full bg-muted">
          <Mail className="h-5 w-5 text-muted-foreground" />
        </div>
        <div className="text-lg font-semibold">还没有可用邮箱</div>
        <div className="mt-2 text-sm text-muted-foreground">请前往邮箱管理，创建、申请或联系管理员分配邮箱。</div>
        <Button className="mt-5" onClick={onManageMailboxes}>
          <MailboxIcon className="h-4 w-4" />前往邮箱管理
        </Button>
      </div>
    </div>
  )
}

function PermissionEmptyState({ title, description, onOpenSettings }: { title: string; description: string; onOpenSettings: () => void }) {
  return (
    <div className="grid min-h-0 flex-1 place-items-center p-6">
      <div className="w-full max-w-md rounded-lg border border-dashed p-8 text-center">
        <div className="mx-auto mb-4 grid size-12 place-items-center rounded-full bg-muted">
          <ShieldCheck className="h-5 w-5 text-muted-foreground" />
        </div>
        <div className="text-lg font-semibold">{title}</div>
        <div className="mt-2 text-sm text-muted-foreground">{description}</div>
        <Button className="mt-5" onClick={onOpenSettings}>
          <Settings className="h-4 w-4" />前往个人中心
        </Button>
      </div>
    </div>
  )
}

function MailQueryFailure({ error, onRetry }: { error: Error; onRetry: () => void }) {
  return (
    <div className="grid min-h-0 flex-1 place-items-center p-6" role="alert">
      <div className="w-full max-w-md rounded-lg border border-destructive/40 bg-destructive/5 p-8 text-center">
        <div className="text-lg font-semibold text-destructive">邮件数据读取失败</div>
        <div className="mt-2 break-words text-sm text-muted-foreground">{error.message || "请检查网络连接后重试"}</div>
        <Button type="button" variant="outline" className="mt-5" onClick={onRetry}>
          <RefreshCcw className="h-4 w-4" />重新读取
        </Button>
      </div>
    </div>
  )
}

function MailDetailFailure({ error, onRetry }: { error: Error; onRetry: () => void }) {
  return (
    <div className="grid h-full min-h-[240px] place-items-center p-6" role="alert">
      <div className="max-w-md text-center">
        <div className="font-semibold text-destructive">邮件详情读取失败</div>
        <div className="mt-2 break-words text-sm text-muted-foreground">{error.message || "请检查网络连接后重试"}</div>
        <Button type="button" variant="outline" size="sm" className="mt-4" onClick={onRetry}>
          <RefreshCcw className="h-4 w-4" />重新读取
        </Button>
      </div>
    </div>
  )
}

function SidebarQueryFailure({ label, onRetry }: { label: string; onRetry: () => void }) {
  return (
    <div className="mx-2 my-1 flex min-h-8 items-center gap-2 rounded-md border border-destructive/30 bg-destructive/5 px-2 text-xs text-destructive" role="alert">
      <span className="min-w-0 flex-1 truncate">{label}</span>
      <Button type="button" variant="ghost" size="icon" className="h-6 w-6 shrink-0 text-destructive hover:text-destructive" aria-label={`重试${label}`} title="重新读取" onClick={onRetry}>
        <RefreshCcw className="h-3.5 w-3.5" />
      </Button>
    </div>
  )
}

function ScheduledSendView({ compact, items, total, loading, query, cancelingId, onCancel }: { compact: boolean; items: ScheduledSend[]; total: number; loading: boolean; query: string; cancelingId: string; onCancel: (item: ScheduledSend) => void }) {
  const empty = query.trim() ? "当前搜索没有匹配的定时邮件" : "没有待发送邮件"
  return (
    <div className="flex min-h-0 flex-1 flex-col bg-background">
      <div className={cn("flex shrink-0 items-center justify-between gap-3 border-b", compact ? "h-12 px-4" : "h-14 px-5")}>
        <div className="min-w-0">
          <div className="flex items-center gap-2 text-sm font-semibold"><Clock3 className="h-4 w-4" />待发送</div>
          <div className="text-xs text-muted-foreground">{items.length} / {total} 封定时邮件</div>
        </div>
      </div>
      <ScrollArea className="min-h-0 flex-1">
        {loading && <ScheduledSendSkeleton />}
        {!loading && items.length === 0 && <div className="p-8 text-center text-sm text-muted-foreground">{empty}</div>}
        {!loading && items.map((item) => (
          <ScheduledSendRow key={item.id} item={item} compact={compact} pending={cancelingId === item.id} onCancel={() => onCancel(item)} />
        ))}
      </ScrollArea>
    </div>
  )
}

function ScheduledSendSkeleton() {
  return (
    <div className="space-y-0">
      {Array.from({ length: 4 }).map((_, index) => (
        <div className="space-y-3 border-b p-4" key={index}>
          <Skeleton className="h-4 w-1/2" />
          <Skeleton className="h-3 w-3/4" />
          <Skeleton className="h-8 w-32" />
        </div>
      ))}
    </div>
  )
}

function ScheduledSendRow({ item, compact, pending, onCancel }: { item: ScheduledSend; compact: boolean; pending: boolean; onCancel: () => void }) {
  const recipients = item.to?.length ? item.to.join(", ") : "未填写收件人"
  const failed = item.status === "failed"
  return (
    <div className={cn("border-b transition-colors hover:bg-accent/40", compact ? "p-4" : "px-5 py-4")}>
      <div className={cn("gap-4", compact ? "space-y-3" : "grid grid-cols-[minmax(0,1fr)_180px_116px] items-center")}>
        <div className="min-w-0">
          <div className="mb-1 flex min-w-0 items-center gap-2">
            <span className="truncate text-sm font-semibold">{item.subject || "(无主题)"}</span>
            <ScheduledStatusBadge status={item.status} />
          </div>
          <div className="truncate text-xs text-muted-foreground">发给 {recipients}</div>
          {item.snippet && <div className="mt-1 line-clamp-1 text-xs text-muted-foreground">{item.snippet}</div>}
          {failed && item.error && <div className="mt-2 text-xs text-destructive">{item.error}</div>}
        </div>
        <div className="text-sm">
          <div className="text-xs text-muted-foreground">发送时间</div>
          <div className="mt-1 font-medium">{formatDateTime(item.sendAt)}</div>
        </div>
        <div className={cn("flex", compact ? "justify-start" : "justify-end")}>
          <Button type="button" variant={failed ? "outline" : "destructive"} size="sm" disabled={pending || item.status === "sending"} onClick={onCancel}>
            {pending ? "处理中..." : failed ? "移除记录" : "取消发送"}
          </Button>
        </div>
      </div>
    </div>
  )
}

function ScheduledStatusBadge({ status }: { status: ScheduledSend["status"] }) {
  const label = status === "pending" ? "等待发送" : status === "sending" ? "发送中" : status === "failed" ? "发送失败" : status === "sent" ? "已发送" : "已取消"
  return (
    <Badge variant={status === "failed" ? "destructive" : status === "sending" ? "secondary" : "outline"} className="h-5 shrink-0 rounded-md px-1.5 text-[11px] font-normal">
      {label}
    </Badge>
  )
}

function attachmentHref(message: MailMessage, attachmentId: string) {
  if (message.externalAccountId) {
    return `/api/mail/external-accounts/${encodeURIComponent(message.externalAccountId)}/attachments/${encodeURIComponent(message.id)}/${encodeURIComponent(attachmentId)}`
  }
  if (!message.mailboxId) return `/api/admin/attachments/${encodeURIComponent(attachmentId)}`
  return `/api/mail/attachments/${attachmentId}`
}

const sendQueueStatusOptions: { value: SendQueueStatus | "all"; label: string }[] = [
  { value: "all", label: "全部状态" },
  { value: "queued", label: "排队中" },
  { value: "sending", label: "发送中" },
  { value: "failed", label: "发送失败" },
  { value: "delivered", label: "已投递" },
  { value: "canceled", label: "已取消" },
]

function SendQueueView({
  compact,
  items,
  total,
  loading,
  status,
  messageId,
  recipient,
  from,
  to,
  pendingId,
  onStatusChange,
  onMessageIdChange,
  onRecipientChange,
  onFromChange,
  onToChange,
  onClearFilters,
  onRetry,
  onCancel,
  onAudit,
  canMutate,
}: {
  compact: boolean
  items: SendQueueItem[]
  total: number
  loading: boolean
  status: SendQueueStatus | "all"
  messageId: string
  recipient: string
  from: string
  to: string
  pendingId: string
  onStatusChange: (status: SendQueueStatus | "all") => void
  onMessageIdChange: (value: string) => void
  onRecipientChange: (value: string) => void
  onFromChange: (value: string) => void
  onToChange: (value: string) => void
  onClearFilters: () => void
  onRetry: (item: SendQueueItem) => void
  onCancel: (item: SendQueueItem) => void
  onAudit: (item: SendQueueItem) => void
  canMutate: boolean
}) {
  const hasFilters = status !== "all" || messageId.trim() || recipient.trim() || from || to
  const empty = hasFilters ? "当前筛选没有匹配的发送任务" : "发送队列为空"
  return (
    <div className="flex min-h-0 flex-1 flex-col bg-background">
      <div className={cn("shrink-0 space-y-3 border-b", compact ? "px-4 py-3" : "px-5 py-4")}>
        <div className="flex items-center justify-between gap-3">
          <div className="min-w-0">
            <div className="flex items-center gap-2 text-sm font-semibold"><History className="h-4 w-4" />发送队列</div>
            <div className="text-xs text-muted-foreground">{items.length} / {total} 个发送任务</div>
          </div>
          <Select value={status} onValueChange={(value) => onStatusChange(value as SendQueueStatus | "all")}>
            <SelectTrigger className="h-9 w-[132px]">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {sendQueueStatusOptions.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>
        <div className={cn("grid gap-2", compact ? "grid-cols-1" : "grid-cols-[minmax(0,1.2fr)_minmax(0,1fr)_160px_160px_auto]")}>
          <Input value={messageId} onChange={(event) => onMessageIdChange(event.target.value)} placeholder="Message-ID" className="h-9" />
          <Input value={recipient} onChange={(event) => onRecipientChange(event.target.value)} placeholder="收件人" className="h-9" />
          <Input type="datetime-local" value={from} onChange={(event) => onFromChange(event.target.value)} className="h-9" />
          <Input type="datetime-local" value={to} onChange={(event) => onToChange(event.target.value)} className="h-9" />
          <Button type="button" variant="outline" size="sm" className="h-9" disabled={!hasFilters} onClick={onClearFilters}>清除</Button>
        </div>
      </div>
      <ScrollArea className="min-h-0 flex-1">
        {loading && <ScheduledSendSkeleton />}
        {!loading && items.length === 0 && <div className="p-8 text-center text-sm text-muted-foreground">{empty}</div>}
        {!loading && items.map((item) => (
          <SendQueueRow
            key={item.id}
            item={item}
            compact={compact}
            pending={pendingId === item.id}
            onRetry={() => onRetry(item)}
            onCancel={() => onCancel(item)}
            onAudit={() => onAudit(item)}
            canMutate={canMutate}
          />
        ))}
      </ScrollArea>
    </div>
  )
}

function SendQueueRow({ item, compact, pending, onRetry, onCancel, onAudit, canMutate }: { item: SendQueueItem; compact: boolean; pending: boolean; onRetry: () => void; onCancel: () => void; onAudit: () => void; canMutate: boolean }) {
  const recipients = item.recipients?.length ? item.recipients.join(", ") : "未记录收件人"
  const failure = item.lastError || item.error || item.failureReason || ""
  const canRetry = item.status === "failed"
  const canCancel = item.status === "queued" || item.status === "failed"
  return (
    <div className={cn("border-b transition-colors hover:bg-accent/40", compact ? "p-4" : "px-5 py-4")}>
      <div className={cn("gap-4", compact ? "space-y-3" : "grid grid-cols-[minmax(0,1fr)_210px_220px] items-center")}>
        <div className="min-w-0">
          <div className="mb-1 flex min-w-0 items-center gap-2">
            <span className="truncate text-sm font-semibold">{item.subject || "(无主题)"}</span>
            <SendQueueStatusBadge status={item.status} />
          </div>
          <div className="truncate text-xs text-muted-foreground">发给 {recipients}</div>
          <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
            <span>来源：{sendQueueSourceLabel(item.source)}</span>
            <span>尝试：{item.attemptCount}/{item.maxAttempts}</span>
            {item.nextAttemptAt && <span>下次：{formatDateTime(item.nextAttemptAt)}</span>}
          </div>
          {failure && <div className="mt-2 line-clamp-2 text-xs text-destructive">{failure}</div>}
        </div>
        <div className="space-y-1 text-sm">
          <div className="text-xs text-muted-foreground">更新时间</div>
          <div className="font-medium">{formatDateTime(item.updatedAt || item.createdAt)}</div>
          {item.deliveredAt && <div className="text-xs text-muted-foreground">投递于 {formatDateTime(item.deliveredAt)}</div>}
        </div>
        <div className={cn("flex flex-wrap gap-2", compact ? "justify-start" : "justify-end")}>
          <Button type="button" variant="outline" size="sm" onClick={onAudit}>
            <History className="h-4 w-4" />时间线
          </Button>
          {canMutate && canRetry && (
            <Button type="button" variant="outline" size="sm" disabled={pending} onClick={onRetry}>
              <RotateCcw className="h-4 w-4" />{pending ? "处理中..." : "重试"}
            </Button>
          )}
          {canMutate && canCancel && (
            <Button type="button" variant="destructive" size="sm" disabled={pending} onClick={onCancel}>
              {pending ? "处理中..." : "取消"}
            </Button>
          )}
        </div>
      </div>
    </div>
  )
}

function SendQueueStatusBadge({ status }: { status: SendQueueStatus }) {
  const label = status === "queued" ? "排队中" : status === "sending" ? "发送中" : status === "delivered" ? "已投递" : status === "failed" ? "发送失败" : "已取消"
  return (
    <Badge variant={status === "failed" ? "destructive" : status === "sending" || status === "queued" ? "secondary" : "outline"} className="h-5 shrink-0 rounded-md px-1.5 text-[11px] font-normal">
      {label}
    </Badge>
  )
}

function SendQueueAuditDialog({ open, loading, error, events, onRetry, onOpenChange }: { open: boolean; loading: boolean; error: Error | null; events: SendQueueAuditEvent[]; onRetry: () => void; onOpenChange: (open: boolean) => void }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[min(92vw,42rem)] max-w-none">
        <DialogHeader>
          <DialogTitle>投递时间线</DialogTitle>
        </DialogHeader>
        <div className="max-h-[60vh] overflow-auto pr-1">
          {loading && <div className="space-y-3">{Array.from({ length: 4 }).map((_, index) => <Skeleton key={index} className="h-14 w-full" />)}</div>}
          {!loading && error && <div className="rounded-lg border border-destructive/40 bg-destructive/5 p-6 text-center" role="alert"><div className="text-sm font-medium text-destructive">投递记录读取失败</div><div className="mt-1 break-words text-xs text-muted-foreground">{error.message}</div><Button type="button" variant="outline" size="sm" className="mt-4" onClick={onRetry}><RefreshCcw className="h-4 w-4" />重新读取</Button></div>}
          {!loading && !error && events.length === 0 && <div className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">暂无投递事件</div>}
          {!loading && !error && events.length > 0 && (
            <div className="space-y-3">
              {events.map((event) => (
                <div key={event.id} className="rounded-lg border p-3">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div className="flex items-center gap-2 text-sm font-medium">
                      {event.status && <SendQueueStatusBadge status={event.status} />}
                      <span>{event.message || event.event || event.eventType || "队列事件"}</span>
                    </div>
                    <span className="text-xs text-muted-foreground">{formatDateTime(event.createdAt)}</span>
                  </div>
                  <div className="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
                    {typeof event.attemptCount === "number" && <span>尝试次数：{event.attemptCount}</span>}
                    {event.error && <span className="text-destructive">{event.error}</span>}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>关闭</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function sendQueueSourceLabel(source: string) {
  const normalized = source.toLowerCase()
  if (normalized === "submission") return "SMTP Submission"
  if (normalized === "webmail") return "Webmail"
  if (normalized === "open_api") return "Open API"
  if (normalized === "scheduled") return "定时发送"
  return source || "未知"
}

function datetimeLocalToISO(value: string) {
  if (!value) return ""
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? "" : date.toISOString()
}

type BulkAction = "read" | "unread" | "star" | "unstar" | "archive" | "inbox" | "trash" | "spam" | "delete"

function BulkActionToolbar({ pending, currentFolder, folders = [], readAction = "read", onAction, onMoveToFolder }: { pending: boolean; currentFolder?: string; folders?: MailFolder[]; readAction?: "read" | "unread"; onAction: (action: BulkAction) => void; onMoveToFolder?: (folderName: string) => void }) {
  const buttonClass = "h-7 w-7 rounded-md text-muted-foreground hover:bg-accent hover:text-foreground"
  const permanentlyDelete = currentFolder === "Trash"
  const archiveAction: BulkAction = currentFolder === "Archive" ? "inbox" : "archive"
  const archiveLabel = currentFolder === "Archive" ? "移回收件箱" : "归档"
  const movableFolders = folders.filter((folder) => folder.name !== currentFolder && folder.name !== "Drafts")
  return (
    <div className="flex shrink-0 items-center gap-1">
      <Button type="button" variant="ghost" size="icon" className={buttonClass} disabled={pending} onClick={() => onAction(readAction)} title={readAction === "read" ? "标为已读" : "标为未读"} aria-label={readAction === "read" ? "标为已读" : "标为未读"}>
        {readAction === "read" ? <MailCheck className="h-3.5 w-3.5" /> : <Mail className="h-3.5 w-3.5" />}
      </Button>
      <Button type="button" variant="ghost" size="icon" className={buttonClass} disabled={pending} onClick={() => onAction(archiveAction)} title={archiveLabel} aria-label={archiveLabel}>
        <Archive className="h-3.5 w-3.5" />
      </Button>
      {onMoveToFolder && movableFolders.length > 0 && (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon" className={buttonClass} disabled={pending} title="移动到文件夹" aria-label="移动到文件夹">
              <Folder className="h-3.5 w-3.5" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-44">
            {movableFolders.map((folder) => (
              <DropdownMenuItem key={folder.id} onSelect={() => onMoveToFolder(folder.name)}>
                {isCustomMailFolder(folder) ? customFolderIcon(folder.icon) : folderIcons[folder.role] || <Folder className="h-4 w-4" />}
                <span className="min-w-0 flex-1 truncate">{folderLabels[folder.name] || folder.name}</span>
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      )}
      <Button type="button" variant="ghost" size="icon" className={cn(buttonClass, "text-destructive hover:bg-destructive/10 hover:text-destructive")} disabled={pending} onClick={() => onAction(permanentlyDelete ? "delete" : "trash")} title={permanentlyDelete ? "永久删除" : "移入已删除"} aria-label={permanentlyDelete ? "永久删除" : "移入已删除"}>
        <Trash2 className="h-3.5 w-3.5" />
      </Button>
    </div>
  )
}

function SidebarContextMenu({ state, canCreate, canReorder, canDelete, pending, onClose, onOpen, onRefresh, onCreateFolder, onMove, onDelete }: { state: SidebarContextMenuState | null; canCreate: boolean; canReorder: boolean; canDelete: boolean; pending: boolean; onClose: () => void; onOpen: (item: MailMenuItem) => void; onRefresh: () => void; onCreateFolder: () => void; onMove: (item: MailMenuItem, action: "top" | "up" | "down" | "bottom") => void; onDelete: (item: MailMenuItem) => void }) {
  React.useEffect(() => {
    if (!state) return
    const close = () => onClose()
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose()
    }
    window.addEventListener("pointerdown", close)
    window.addEventListener("resize", close)
    window.addEventListener("scroll", close, true)
    window.addEventListener("keydown", onKeyDown)
    return () => {
      window.removeEventListener("pointerdown", close)
      window.removeEventListener("resize", close)
      window.removeEventListener("scroll", close, true)
      window.removeEventListener("keydown", onKeyDown)
    }
  }, [state, onClose])

  if (!state) return null
  const item = state.item
  const customFolder = item.type === "folder" && item.custom
  const position = contextMenuPosition(state.x, state.y)
  const itemClass = "h-auto w-full justify-start rounded px-2 py-1.5 text-left font-normal"
  return (
    <div
      className="fixed z-[110] w-48 rounded-md border bg-popover p-1 text-sm text-popover-foreground shadow-md"
      style={{ left: position.x, top: position.y }}
      onPointerDown={(event) => event.stopPropagation()}
      onContextMenu={(event) => event.preventDefault()}
      role="menu"
    >
      <Button type="button" variant="ghost" className={itemClass} onClick={() => onOpen(item)}>
        <Inbox className="h-4 w-4" />打开
      </Button>
      <Button type="button" variant="ghost" className={itemClass} onClick={onRefresh}>
        <RefreshCcw className="h-4 w-4" />刷新
      </Button>
      {canCreate && (
        <Button type="button" variant="ghost" className={itemClass} onClick={onCreateFolder}>
          新建文件夹
        </Button>
      )}
      {customFolder && (canReorder || canDelete) && (
        <>
          <div className="my-1 h-px bg-border" />
          {canReorder && <>
            <Button type="button" variant="ghost" className={itemClass} disabled={pending} onClick={() => onMove(item, "top")}>
              <ArrowLeft className="h-4 w-4 rotate-90" />移到最上
            </Button>
            <Button type="button" variant="ghost" className={itemClass} disabled={pending} onClick={() => onMove(item, "up")}>
              <ChevronDown className="h-4 w-4 rotate-180" />上移一位
            </Button>
            <Button type="button" variant="ghost" className={itemClass} disabled={pending} onClick={() => onMove(item, "down")}>
              <ChevronDown className="h-4 w-4" />下移一位
            </Button>
            <Button type="button" variant="ghost" className={itemClass} disabled={pending} onClick={() => onMove(item, "bottom")}>
              <ArrowLeft className="h-4 w-4 -rotate-90" />移到最下
            </Button>
          </>}
          {canReorder && canDelete && <div className="my-1 h-px bg-border" />}
          {canDelete && <Button type="button" variant="ghost" className={cn(itemClass, "text-destructive hover:bg-destructive/10 hover:text-destructive")} disabled={pending} onClick={() => onDelete(item)}>
              <Trash2 className="h-4 w-4" />删除文件夹
          </Button>}
        </>
      )}
    </div>
  )
}

function MessageContextMenu({ state, labels, folders, canSend, canOrganize, canManageLabels, labelPending, onClose, onAction, onMoveToFolder, onToggleLabel }: { state: MessageContextMenuState | null; labels: MailLabel[]; folders: MailFolder[]; canSend: boolean; canOrganize: boolean; canManageLabels: boolean; labelPending: boolean; onClose: () => void; onAction: (action: "open" | "reply" | "forward" | "read" | "star" | "archive" | "trash" | "spam" | "delete", message: MailMessage) => void; onMoveToFolder: (message: MailMessage, folderName: string) => void; onToggleLabel: (message: MailMessage, label: MailLabel) => void }) {
  React.useEffect(() => {
    if (!state) return
    const close = () => onClose()
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose()
    }
    window.addEventListener("pointerdown", close)
    window.addEventListener("resize", close)
    window.addEventListener("scroll", close, true)
    window.addEventListener("keydown", onKeyDown)
    return () => {
      window.removeEventListener("pointerdown", close)
      window.removeEventListener("resize", close)
      window.removeEventListener("scroll", close, true)
      window.removeEventListener("keydown", onKeyDown)
    }
  }, [state, onClose])

  if (!state) return null
  const { message } = state
  const position = contextMenuPosition(state.x, state.y)
  const draft = message.folder === "Drafts"
  const itemClass = "h-auto w-full justify-start rounded-sm px-3 py-2 text-left font-normal"

  function item(label: string, icon: React.ReactNode, action: Parameters<typeof onAction>[0], destructive = false) {
    return (
      <Button type="button" variant="ghost" className={cn(itemClass, destructive && "text-destructive hover:text-destructive")} onClick={() => onAction(action, message)}>
        {icon}
        <span>{label}</span>
      </Button>
    )
  }
  function toggleLabel(label: MailLabel) {
    onToggleLabel(message, label)
    onClose()
  }
  function moveToFolder(folderName: string) {
    onMoveToFolder(message, folderName)
    onClose()
  }
  const movableFolders = folders.filter((folder) => folder.name !== message.folder && folder.name !== "Drafts")

  return (
    <div
      className="fixed z-[110] w-52 rounded-md border bg-popover p-1 text-popover-foreground shadow-md"
      style={{ left: position.x, top: position.y }}
      onClick={(event) => event.stopPropagation()}
      onContextMenu={(event) => event.preventDefault()}
      onPointerDown={(event) => event.stopPropagation()}
      role="menu"
    >
      {item(draft ? "编辑草稿" : "打开邮件", draft ? <PencilLine className="h-4 w-4" /> : <Mail className="h-4 w-4" />, "open")}
      {!draft && canSend && (
        <>
          {item("回复", <Reply className="h-4 w-4" />, "reply")}
          {item("转发", <Forward className="h-4 w-4" />, "forward")}
        </>
      )}
      {!draft && canOrganize && (
        <>
          <div className="-mx-1 my-1 h-px bg-border" />
          {item(message.isRead ? "标为未读" : "标为已读", <MailCheck className="h-4 w-4" />, "read")}
          {item(message.isStarred ? "取消星标" : "添加星标", <Star className={cn("h-4 w-4", message.isStarred && "fill-yellow-400 text-yellow-500")} />, "star")}
          {item(message.folder === "Archive" ? "取消归档" : "归档", <Archive className="h-4 w-4" />, "archive")}
          {message.folder !== "Trash" && item("移入回收站", <Trash2 className="h-4 w-4" />, "trash")}
          {message.folder !== "Spam" && item("移入垃圾邮件", <Trash2 className="h-4 w-4" />, "spam")}
        </>
      )}
      {!draft && canOrganize && movableFolders.length > 0 && (
        <>
          <div className="-mx-1 my-1 h-px bg-border" />
          <div className="px-3 py-1.5 text-xs font-medium text-muted-foreground">移动到</div>
          <div className="max-h-44 overflow-y-auto">
            {movableFolders.map((folder) => (
              <Button key={folder.id} type="button" variant="ghost" className={itemClass} onClick={() => moveToFolder(folder.name)}>
                {isCustomMailFolder(folder) ? customFolderIcon(folder.icon) : folderIcons[folder.role] || <Inbox className="h-4 w-4" />}
                <span className="min-w-0 flex-1 truncate text-left">{folderLabels[folder.name] || folder.name}</span>
                {folder.totalCount > 0 && <span className="text-xs text-muted-foreground">{folder.totalCount}</span>}
              </Button>
            ))}
          </div>
        </>
      )}
      {canManageLabels && labels.length > 0 && (
        <>
          <div className="-mx-1 my-1 h-px bg-border" />
          <div className="px-3 py-1.5 text-xs font-medium text-muted-foreground">标签</div>
          <div className="max-h-44 overflow-y-auto">
            {labels.map((label) => {
              const active = (message.labels || []).some((item) => item.id === label.id)
              return (
                <Button key={label.id} type="button" variant="ghost" className={itemClass} disabled={labelPending} onClick={() => toggleLabel(label)}>
                  <Check className={cn("h-4 w-4", active ? "opacity-100" : "opacity-0")} />
                  <span className="h-2 w-2 shrink-0 rounded-full" style={{ backgroundColor: labelDotColor(label) }} />
                  <span className="min-w-0 flex-1 truncate text-left">{active ? `移除 ${label.name}` : label.name}</span>
                </Button>
              )
            })}
          </div>
        </>
      )}
      {canOrganize && (
        <>
          <div className="-mx-1 my-1 h-px bg-border" />
          {item(message.folder === "Trash" ? "永久删除" : "移入已删除", <Trash2 className="h-4 w-4" />, "delete", true)}
        </>
      )}
    </div>
  )
}

function contextMenuPosition(x: number, y: number) {
  const width = 208
  const height = 312
  const padding = 8
  const maxX = Math.max(padding, window.innerWidth - width - padding)
  const maxY = Math.max(padding, window.innerHeight - height - padding)
  return { x: Math.min(Math.max(x, padding), maxX), y: Math.min(Math.max(y, padding), maxY) }
}

function CreateFolderDialog({ open, pending, scope, onOpenChange, onCreate }: { open: boolean; pending: boolean; scope: string; onOpenChange: (open: boolean) => void; onCreate: (payload: { name: string; icon: string }) => void }) {
  const [name, setName] = React.useState("")
  const [icon, setIcon] = React.useState("auto")
  const [uploadError, setUploadError] = React.useState("")
  const iconInputRef = React.useRef<HTMLInputElement>(null)
  React.useEffect(() => {
    if (open) {
      setName("")
      setIcon("auto")
      setUploadError("")
    }
  }, [open])
  const trimmed = name.trim()
  const suggestedIcon = suggestedFolderIcon(trimmed)
  const resolvedIcon = icon === "auto" ? suggestedIcon : icon
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[min(92vw,28rem)] max-w-none">
        <DialogHeader>
          <DialogTitle>新建文件夹</DialogTitle>
        </DialogHeader>
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault()
            if (trimmed) onCreate({ name: trimmed, icon: resolvedIcon })
          }}
        >
          <div className="space-y-2">
            <Label htmlFor="new-folder-name">文件夹名称</Label>
            <Input id="new-folder-name" autoFocus value={name} onChange={(event) => setName(event.target.value)} placeholder="例如：客户、账单、项目归档" />
            <p className="text-xs text-muted-foreground">创建位置：{scope}</p>
          </div>
          <fieldset className="space-y-2">
            <legend className="text-sm font-medium">图标</legend>
            <div className="grid grid-cols-7 gap-2">
              <Button type="button" variant="outline" size="icon" className={cn("relative size-10 shadow-none", icon === "auto" && "border-primary bg-primary/10 text-primary ring-1 ring-primary")} onClick={() => setIcon("auto")} aria-label={`自动匹配：${customFolderIconOptions.find((item) => item.key === suggestedIcon)?.label || "文件夹"}`} title={`自动匹配：${customFolderIconOptions.find((item) => item.key === suggestedIcon)?.label || "文件夹"}`} aria-pressed={icon === "auto"}>
                {customFolderIcon(suggestedIcon)}
                <Sparkles className="absolute -right-1 -top-1 h-3 w-3 rounded-full bg-background" />
              </Button>
              {customFolderIconOptions.map((option) => {
                const Icon = option.icon
                return (
                  <Button key={option.key} type="button" variant="outline" size="icon" className={cn("size-10 shadow-none", icon === option.key && "border-primary bg-primary/10 text-primary ring-1 ring-primary")} onClick={() => setIcon(option.key)} aria-label={option.label} title={option.label} aria-pressed={icon === option.key}>
                    <Icon className="h-4 w-4" />
                  </Button>
                )
              })}
              <Button type="button" variant="outline" size="icon" className={cn("size-10 shadow-none", icon.startsWith("data:image/png;base64,") && "border-primary bg-primary/10 text-primary ring-1 ring-primary")} onClick={() => iconInputRef.current?.click()} aria-label="上传自定义图标" title="上传自定义图标" aria-pressed={icon.startsWith("data:image/png;base64,")}>
                {icon.startsWith("data:image/png;base64,") ? customFolderIcon(icon) : <Upload className="h-4 w-4" />}
              </Button>
              <input
                ref={iconInputRef}
                type="file"
                accept="image/png,image/jpeg,image/webp"
                className="hidden"
                onChange={async (event) => {
                  const file = event.target.files?.[0]
                  event.target.value = ""
                  if (!file) return
                  try {
                    setUploadError("")
                    setIcon(await prepareFolderIcon(file))
                  } catch (error) {
                    setUploadError(error instanceof Error ? error.message : "无法处理该图片")
                  }
                }}
              />
            </div>
            {uploadError && <p role="alert" className="text-xs text-destructive">{uploadError}</p>}
          </fieldset>
          <DialogFooter className="gap-2">
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>取消</Button>
            <Button disabled={!trimmed || pending}>{pending ? "创建中..." : "创建"}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function CompactMailView({
  title,
  currentFolder,
  icon,
  messages,
  total,
  selectedIds,
  allSelected,
  someSelected,
  loading,
  hasMore,
  loadingMore,
  emptyMessage,
  selectedId,
  selected,
  detailLoading,
  detailError,
  labels,
  labelPending,
  onSelect,
  onSelectAll,
  onToggleSelected,
  scheduledDraftIds,
  onLoadMore,
  onRetryDetail,
  onCloseReader,
  onStar,
  onReply,
  onForward,
  onSendTimeline,
  onArchive,
  onDelete,
  onToggleRead,
  onAddLabel,
  onRemoveLabel,
  bulkPending,
  onBulkAction,
  onContextMenu,
  canSend,
  canOrganize,
  canManageLabels,
  canDownloadAttachments,
  language,
  tools,
}: {
  title: string
  currentFolder?: string
  icon?: React.ReactNode
  messages: MailMessage[]
  total: number
  selectedIds: string[]
  allSelected: boolean
  someSelected: boolean
  loading: boolean
  hasMore: boolean
  loadingMore: boolean
  emptyMessage: string
  selectedId: string | null
  selected?: MailMessage
  detailLoading: boolean
  detailError: Error | null
  labels: MailLabel[]
  labelPending: boolean
  onSelect: (id: string | null) => void
  onSelectAll: (checked: boolean) => void
  onToggleSelected: (id: string, checked: boolean) => void
  scheduledDraftIds: Set<string>
  onLoadMore: () => void
  onRetryDetail: () => void
  onCloseReader: () => void
  onStar: (message: MailMessage) => void
  onReply: (message: MailMessage) => void
  onForward: (message: MailMessage) => void
  onSendTimeline: (message: MailMessage) => void
  onArchive: (message: MailMessage) => void
  onDelete: (message: MailMessage) => void
  onToggleRead: (message: MailMessage) => void
  onAddLabel: (message: MailMessage, label: MailLabel) => void
  onRemoveLabel: (message: MailMessage, labelId: string) => void
  bulkPending: boolean
  onBulkAction: (action: BulkAction) => void
  onContextMenu: (event: React.MouseEvent, message: MailMessage) => void
  canSend: boolean
  canOrganize: boolean
  canManageLabels: boolean
  canDownloadAttachments: boolean
  language: Language
  tools?: React.ReactNode
}) {
  const selectedIndex = selectedId ? messages.findIndex((message) => message.id === selectedId) : -1
  const previousMessage = selectedIndex > 0 ? messages[selectedIndex - 1] : undefined
  const nextMessage = selectedIndex >= 0 && selectedIndex < messages.length - 1 ? messages[selectedIndex + 1] : undefined

  if (selectedId) {
    return (
      <CompactMessageDetail
        selected={selected}
        loading={detailLoading}
        error={detailError}
        labels={labels}
        labelPending={labelPending}
        previousMessage={previousMessage}
        nextMessage={nextMessage}
        onBack={onCloseReader}
        onRetry={onRetryDetail}
        onSelect={onSelect}
        onStar={onStar}
        onReply={onReply}
        onForward={onForward}
        onSendTimeline={onSendTimeline}
        onArchive={onArchive}
        onDelete={onDelete}
        onToggleRead={onToggleRead}
        onAddLabel={onAddLabel}
        onRemoveLabel={onRemoveLabel}
        canSend={canSend}
        canOrganize={canOrganize}
        canManageLabels={canManageLabels}
        canDownloadAttachments={canDownloadAttachments}
        language={language}
      />
    )
  }

  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col bg-background">
      <div className="flex min-h-11 shrink-0 items-center gap-3 border-b px-3 sm:px-4">
        <div className={cn("flex min-w-0 items-center gap-2.5", selectedIds.length === 0 && "flex-1")}>
          <Checkbox aria-label="选择当前页邮件" checked={allSelected ? true : someSelected ? "indeterminate" : false} onCheckedChange={(value) => onSelectAll(value === true)} />
          <div className="flex min-w-0 items-center gap-2 text-[15px] font-semibold">
            {icon}
            <span className="truncate">{title}</span>
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-1.5">
          {selectedIds.length > 0 ? (
            <>
              <span className="hidden text-xs text-muted-foreground min-[380px]:inline">已选 {selectedIds.length} 封</span>
              {canOrganize && <BulkActionToolbar pending={bulkPending} currentFolder={currentFolder} onAction={onBulkAction} />}
            </>
          ) : (
            <div className="flex items-center gap-1">
              <div className="text-xs text-muted-foreground">{messages.length} / {total} 封</div>
              {tools}
            </div>
          )}
        </div>
      </div>
      <ScrollArea className="min-h-0 flex-1">
        {loading && <MessageSkeleton />}
        {messages.map((message) => <CompactMessageRow key={message.id} message={message} active={selectedId === message.id} checked={selectedIds.includes(message.id)} scheduled={scheduledDraftIds.has(message.id)} onCheckedChange={(checked) => onToggleSelected(message.id, checked)} onClick={() => onSelect(message.id)} onContextMenu={(event) => onContextMenu(event, message)} onStar={() => onStar(message)} canOrganize={canOrganize} />)}
        {!loading && messages.length === 0 && <div className="p-8 text-center text-sm text-muted-foreground">{emptyMessage}</div>}
        {!loading && hasMore && (
          <div className="border-b p-4 text-center">
            <Button variant="outline" size="sm" disabled={loadingMore} onClick={onLoadMore}>
              {loadingMore ? "加载中..." : "加载更多"}
            </Button>
          </div>
        )}
      </ScrollArea>
    </div>
  )
}

function CompactMessageDetail({
  selected,
  loading,
  error,
  labels,
  labelPending,
  previousMessage,
  nextMessage,
  onBack,
  onRetry,
  onSelect,
  onStar,
  onReply,
  onForward,
  onSendTimeline,
  onArchive,
  onDelete,
  onToggleRead,
  onAddLabel,
  onRemoveLabel,
  canSend,
  canOrganize,
  canManageLabels,
  canDownloadAttachments,
  language,
}: {
  selected?: MailMessage
  loading: boolean
  error: Error | null
  labels: MailLabel[]
  labelPending: boolean
  previousMessage?: MailMessage
  nextMessage?: MailMessage
  onBack: () => void
  onRetry: () => void
  onSelect: (id: string | null) => void
  onStar: (message: MailMessage) => void
  onReply: (message: MailMessage) => void
  onForward: (message: MailMessage) => void
  onSendTimeline: (message: MailMessage) => void
  onArchive: (message: MailMessage) => void
  onDelete: (message: MailMessage) => void
  onToggleRead: (message: MailMessage) => void
  onAddLabel: (message: MailMessage, label: MailLabel) => void
  onRemoveLabel: (message: MailMessage, labelId: string) => void
  canSend: boolean
  canOrganize: boolean
  canManageLabels: boolean
  canDownloadAttachments: boolean
  language: Language
}) {
  return (
    <div className="flex min-h-0 flex-1 flex-col bg-background">
      <div className="shrink-0 border-b px-3 py-2 sm:px-4">
        <div className="flex min-h-10 items-center gap-2 sm:hidden">
          <Button variant="ghost" size="icon" onClick={onBack} aria-label="返回">
            <ArrowLeft className="h-4 w-4" />
          </Button>
          <div className="min-w-0 flex-1 truncate text-sm font-semibold">{selected?.subject || "邮件详情"}</div>
          <Button variant="ghost" size="icon" disabled={!previousMessage} onClick={() => previousMessage && onSelect(previousMessage.id)} aria-label="上一封">
            <ArrowLeft className="h-4 w-4" />
          </Button>
          <Button variant="ghost" size="icon" disabled={!nextMessage} onClick={() => nextMessage && onSelect(nextMessage.id)} aria-label="下一封">
            <ArrowLeft className="h-4 w-4 rotate-180" />
          </Button>
          {selected && (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="icon" aria-label="更多操作">
                  <Ellipsis className="h-4 w-4" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                {selected.folder === "Drafts" ? (
                  <DropdownMenuItem onSelect={() => onSelect(selected.id)}><PencilLine className="h-4 w-4" />编辑草稿</DropdownMenuItem>
                ) : (
                  <>
                    {canSend && <DropdownMenuItem onSelect={() => onReply(selected)}><Reply className="h-4 w-4" />回复</DropdownMenuItem>}
                    {canSend && <DropdownMenuItem onSelect={() => onForward(selected)}><Forward className="h-4 w-4" />转发</DropdownMenuItem>}
                    {selected.sendQueueId && <DropdownMenuItem onSelect={() => onSendTimeline(selected)}><History className="h-4 w-4" />投递时间线</DropdownMenuItem>}
                    {canOrganize && <DropdownMenuItem onSelect={() => onArchive(selected)}><Archive className="h-4 w-4" />{selected.folder === "Archive" ? "取消归档" : "归档"}</DropdownMenuItem>}
                    {canOrganize && <DropdownMenuItem onSelect={() => onToggleRead(selected)}><MailCheck className="h-4 w-4" />{selected.isRead ? "标为未读" : "标为已读"}</DropdownMenuItem>}
                    {canOrganize && <DropdownMenuItem onSelect={() => onStar(selected)}><Star className={cn("h-4 w-4", selected.isStarred && "fill-yellow-400 text-yellow-500")} />{selected.isStarred ? "取消星标" : "添加星标"}</DropdownMenuItem>}
                  </>
                )}
                {canOrganize && <DropdownMenuItem onSelect={() => onDelete(selected)} className="text-destructive"><Trash2 className="h-4 w-4" />{selected.folder === "Trash" ? "永久删除" : "移入已删除"}</DropdownMenuItem>}
              </DropdownMenuContent>
            </DropdownMenu>
          )}
        </div>
        <div className="hidden min-h-10 items-center justify-between gap-3 sm:flex">
          <div className="flex flex-wrap items-center gap-2">
            <Button variant="outline" size="sm" onClick={onBack}><ArrowLeft className="h-4 w-4" />返回</Button>
            {selected?.folder === "Drafts" ? (
              <Button variant="outline" size="sm" onClick={() => onSelect(selected.id)}><PencilLine className="h-4 w-4" />编辑草稿</Button>
            ) : (
              <>
                {selected && canSend && <Button variant="outline" size="sm" onClick={() => onReply(selected)}><Reply className="h-4 w-4" />回复</Button>}
                {selected && canSend && <Button variant="outline" size="sm" onClick={() => onForward(selected)}><Forward className="h-4 w-4" />转发</Button>}
                {selected?.sendQueueId && <Button variant="outline" size="sm" onClick={() => onSendTimeline(selected)}><History className="h-4 w-4" />投递时间线</Button>}
                {selected && canOrganize && <Button variant="outline" size="sm" onClick={() => onArchive(selected)}>{selected.folder === "Archive" ? "取消归档" : "归档"}</Button>}
                {selected && canOrganize && <Button variant="outline" size="sm" onClick={() => onToggleRead(selected)}><MailCheck className="h-4 w-4" />{selected.isRead ? "标为未读" : "标为已读"}</Button>}
                {selected && canOrganize && <Button variant="outline" size="sm" onClick={() => onStar(selected)}><Star className={cn("h-4 w-4", selected.isStarred && "fill-yellow-400 text-yellow-500")} />{selected.isStarred ? "取消星标" : "添加星标"}</Button>}
              </>
            )}
            {selected && canOrganize && <Button variant="outline" size="sm" onClick={() => onDelete(selected)}><Trash2 className="h-4 w-4" />{selected.folder === "Trash" ? "永久删除" : "移入已删除"}</Button>}
          </div>
          <div className="flex items-center gap-2">
            <Button variant="ghost" size="sm" disabled={!previousMessage} onClick={() => previousMessage && onSelect(previousMessage.id)}>上一封</Button>
            <Button variant="ghost" size="sm" disabled={!nextMessage} onClick={() => nextMessage && onSelect(nextMessage.id)}>下一封</Button>
          </div>
        </div>
      </div>
        {loading && <div className="space-y-4 p-8"><Skeleton className="h-8 w-2/3" /><Skeleton className="h-4 w-1/3" /><Separator /><Skeleton className="h-64 w-full" /></div>}
        {!loading && error && <MailDetailFailure error={error} onRetry={onRetry} />}
        {!loading && !error && !selected && <div className="grid flex-1 place-items-center text-sm text-muted-foreground">邮件不存在</div>}
        {!error && selected && (
          <ScrollArea className="min-h-0 flex-1">
            <div className="w-full px-4 py-4 sm:px-8 sm:py-6">
              <div className="space-y-5 border-b pb-5">
                <div className="flex items-start gap-3">
                  <h1 className="min-w-0 flex-1 break-words text-xl font-semibold tracking-tight sm:text-2xl">{messageSubject(selected)}</h1>
                  {canOrganize && <Button type="button" variant="ghost" size="icon" aria-label={selected.isStarred ? "取消星标" : "添加星标"} className="text-muted-foreground hover:text-yellow-500" onClick={() => onStar(selected)}>
                    <Star className={cn("h-5 w-5", selected.isStarred && "fill-yellow-400 text-yellow-500")} />
                  </Button>}
                </div>
                <MessageMetaPanel
                  message={selected}
                  {...(canManageLabels ? { availableLabels: labels, onAddLabel: (label: MailLabel) => onAddLabel(selected, label), onRemoveLabel: (labelId: string) => onRemoveLabel(selected, labelId), labelPending } : {})}
                />
              </div>
              <div className="py-6 sm:py-8">
                <TranslatableMailBody message={selected} language={language} />
                {selected.attachments && selected.attachments.length > 0 && <div className="mt-8 rounded-lg border p-4"><div className="mb-3 font-medium">附件</div><div className="space-y-2">{selected.attachments.map((a) => canDownloadAttachments ? <a className="flex flex-col gap-1 rounded-md border p-3 text-sm hover:bg-accent sm:flex-row sm:items-center sm:justify-between" href={attachmentHref(selected, a.id)} key={a.id}><span className="flex min-w-0 items-center gap-2"><Paperclip className="h-4 w-4 shrink-0" /><span className="truncate">{a.filename}</span></span><span className="text-muted-foreground">{formatBytes(a.sizeBytes)}</span></a> : <div className="flex flex-col gap-1 rounded-md border p-3 text-sm text-muted-foreground sm:flex-row sm:items-center sm:justify-between" key={a.id}><span className="flex min-w-0 items-center gap-2"><Paperclip className="h-4 w-4 shrink-0" /><span className="truncate">{a.filename}</span></span><span>{formatBytes(a.sizeBytes)}</span></div>)}</div></div>}
              </div>
            </div>
          </ScrollArea>
        )}
    </div>
  )
}



function TranslatableMailBody({ message, language }: { message: MailMessage; language: Language }) {
  const qc = useQueryClient()
  const [translatedText, setTranslatedText] = React.useState("")
  const [translatedHtml, setTranslatedHtml] = React.useState("")
  const [showTranslated, setShowTranslated] = React.useState(false)
  const [truncated, setTruncated] = React.useState(false)
  const [autoTranslate, setAutoTranslate] = React.useState(() => {
    try {
      return window.localStorage.getItem("newszxcn.mail.auto-translate") !== "false"
    } catch {
      return true
    }
  })
  const { toast } = useToast()
  const targetLanguage = normalizeTranslationLanguage(language)
  const sourceText = React.useMemo(() => (message.bodyText || stripHtml(message.bodyHtml || message.snippet || "")).trim(), [message.bodyHtml, message.bodyText, message.snippet])
  const shouldShow = targetLanguage && (message.externalAccountId || message.mailboxId) && shouldOfferMessageTranslation(sourceText, language)
  const translationKey = React.useMemo(() => ["mail-translation", message.externalAccountId || "local", message.id, targetLanguage] as const, [message.externalAccountId, message.id, targetLanguage])
  const translatedMessage = React.useMemo<MailMessage>(() => ({ ...message, bodyText: translatedText, bodyHtml: translatedHtml }), [message, translatedHtml, translatedText])
  const applyTranslation = React.useCallback((result: Awaited<ReturnType<typeof api.translateMessage>>, display = true) => {
    setTranslatedText(result.translatedText)
    setTranslatedHtml(result.translatedHtml || "")
    setTruncated(result.truncated)
    setShowTranslated(display)
  }, [])
  const translate = useMutation({
    mutationFn: async ({ force = false }: { force?: boolean } = {}) => {
      if (!force) {
        const cached = qc.getQueryData<Awaited<ReturnType<typeof api.translateMessage>>>(translationKey)
        if (cached) return cached
      }
      const result = message.externalAccountId ? await api.translateExternalMessage(message.externalAccountId, message.id, targetLanguage!) : await api.translateMessage(message.id, targetLanguage!)
      qc.setQueryData(translationKey, result)
      return result
    },
    onSuccess: (result) => applyTranslation(result),
    onError: (error) => toast({ title: "翻译失败", description: error instanceof Error ? error.message : "请稍后重试" }),
  })

  React.useEffect(() => {
    setTranslatedText("")
    setTranslatedHtml("")
    setShowTranslated(false)
    setTruncated(false)
    translate.reset()
    const cached = qc.getQueryData<Awaited<ReturnType<typeof api.translateMessage>>>(translationKey)
    if (cached) applyTranslation(cached, autoTranslate)
  }, [message.id, language])

  React.useEffect(() => {
    if (!autoTranslate || !shouldShow || translate.isPending) return
    const cached = qc.getQueryData<Awaited<ReturnType<typeof api.translateMessage>>>(translationKey)
    if (cached) {
      applyTranslation(cached)
      return
    }
    translate.mutate({ force: false })
  }, [autoTranslate, message.id, shouldShow, targetLanguage])

  const changeAutoTranslate = (checked: boolean) => {
    setAutoTranslate(checked)
    if (!checked) setShowTranslated(false)
    try {
      window.localStorage.setItem("newszxcn.mail.auto-translate", String(checked))
    } catch {
      // Keep the setting for this session when browser storage is unavailable.
    }
  }

  return (
    <>
      {(shouldShow || translatedText) && (
        <div className="mb-4 rounded-lg border bg-muted/30 p-3 text-sm">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="text-muted-foreground">
              {translatedText ? `已翻译为 ${translationTargetLabel(language)}，当前${showTranslated ? "显示译文" : "显示原文"}` : `检测到邮件可能不是当前语言，可翻译为 ${translationTargetLabel(language)}`}
              {truncated && <span className="ml-1">（内容较长，仅翻译前半部分）</span>}
            </div>
            <div className="flex items-center gap-2">
              <label className="flex cursor-pointer items-center gap-2 whitespace-nowrap text-xs text-muted-foreground">
                <Switch checked={autoTranslate} onCheckedChange={changeAutoTranslate} aria-label="自动翻译邮件" />
                自动翻译
              </label>
              {translatedText && <Button type="button" variant="ghost" size="sm" onClick={() => setShowTranslated((value) => !value)}>{showTranslated ? "显示原文" : "显示译文"}</Button>}
              <Button type="button" variant="outline" size="sm" disabled={translate.isPending} onClick={() => translate.mutate({ force: !!translatedText })}>{translate.isPending ? "翻译中..." : translatedText ? "重新翻译" : "翻译"}</Button>
            </div>
          </div>
        </div>
      )}
      {translatedText && showTranslated ? <MailHtmlFrame message={translatedMessage} /> : <MailHtmlFrame message={message} />}
    </>
  )
}

function normalizeTranslationLanguage(language: Language) {
  if (language === "zh-CN") return "zh-CN"
  if (language === "zh-TW") return "zh-TW"
  if (language === "en") return "en"
  return ""
}

function translationTargetLabel(language: Language) {
  if (language === "zh-CN") return "简体中文"
  if (language === "zh-TW") return "繁體中文"
  return "English"
}

function shouldOfferMessageTranslation(text: string, language: Language) {
  if (!text.trim()) return false
  const cjkCount = (text.match(/[\u4e00-\u9fff]/g) || []).length
  const latinCount = (text.match(/[a-zA-Z]/g) || []).length
  if (language === "zh-CN" || language === "zh-TW") return latinCount > 80 && latinCount > cjkCount * 3
  if (language === "en") return cjkCount > 20
  return false
}

function MailHtmlFrame({ message }: { message: MailMessage }) {
  const iframeRef = React.useRef<HTMLIFrameElement>(null)
  const [height, setHeight] = React.useState(260)
  const srcDoc = React.useMemo(() => buildMailFrameSrcDoc(message.bodyHtml || "", message.bodyText || ""), [message.bodyHtml, message.bodyText])

  const resize = React.useCallback(() => {
    const doc = iframeRef.current?.contentDocument
    if (!doc) return
    const body = doc.body
    const html = doc.documentElement
    const nextHeight = Math.max(180, Math.ceil(Math.max(body?.scrollHeight || 0, body?.offsetHeight || 0, html?.scrollHeight || 0, html?.offsetHeight || 0)))
    setHeight(nextHeight)
  }, [])

  React.useEffect(() => {
    setHeight(260)
    const frame = iframeRef.current
    if (!frame) return
    let observer: ResizeObserver | undefined
    const timers = [window.setTimeout(resize, 0), window.setTimeout(resize, 120), window.setTimeout(resize, 600)]
    const attach = () => {
      const doc = frame.contentDocument
      if (!doc) return
      doc.querySelectorAll("a[href]").forEach((link) => {
        link.setAttribute("target", "_blank")
        link.setAttribute("rel", "noopener noreferrer")
      })
      resize()
      if ("ResizeObserver" in window) {
        observer = new ResizeObserver(resize)
        observer.observe(doc.documentElement)
        if (doc.body) observer.observe(doc.body)
      }
      doc.querySelectorAll("img").forEach((img) => img.addEventListener("load", resize, { once: true }))
    }
    frame.addEventListener("load", attach)
    return () => {
      frame.removeEventListener("load", attach)
      observer?.disconnect()
      timers.forEach((timer) => window.clearTimeout(timer))
    }
  }, [resize, srcDoc])

  return (
    <iframe
      ref={iframeRef}
      title="邮件正文"
      className="block w-full border-0 bg-white"
      sandbox="allow-same-origin allow-popups allow-popups-to-escape-sandbox"
      referrerPolicy="no-referrer"
      srcDoc={srcDoc}
      style={{ height }}
    />
  )
}

function CompactMessageRow({ message, active, checked, scheduled, onCheckedChange, onClick, onContextMenu, onStar, canOrganize }: { message: MailMessage; active: boolean; checked: boolean; scheduled?: boolean; onCheckedChange: (checked: boolean) => void; onClick: () => void; onContextMenu: (event: React.MouseEvent) => void; onStar: () => void; canOrganize: boolean }) {
  const visibleLabels = (message.labels || []).slice(0, 2)
  const hiddenLabelCount = Math.max((message.labels?.length || 0) - visibleLabels.length, 0)
  const senderName = senderDisplayName(message)
  return (
    <div onClick={onClick} onContextMenu={onContextMenu} className={cn(
      "cursor-pointer border-b border-l-2 px-3 py-2.5 text-[13px] transition-colors sm:grid sm:grid-cols-[28px_24px_minmax(112px,180px)_minmax(0,1fr)_86px_30px] sm:items-center sm:gap-2 sm:px-3 sm:py-2",
      message.isRead ? "border-l-transparent hover:bg-accent/50" : "border-l-primary bg-primary/5 font-semibold hover:bg-primary/10",
      active && "mail-selected-row"
    )}>
      <div className="flex gap-3 sm:contents">
        <Checkbox aria-label="选择邮件" checked={checked} onCheckedChange={(value) => onCheckedChange(value === true)} onClick={(event) => event.stopPropagation()} className="mt-0.5 shrink-0 sm:mt-0" />
        {message.isRead ? (
          <MailCheck className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground/70 sm:mt-0" />
        ) : (
          <Mail className="mt-0.5 h-4 w-4 shrink-0 fill-yellow-200 text-yellow-500 sm:mt-0" />
        )}
        <div className="min-w-0 flex-1 sm:contents">
          <div className="flex min-w-0 items-center justify-between gap-2 sm:block">
            <div className="flex min-w-0 items-center gap-1.5">
              <div className="min-w-0 truncate" title={senderTitle(message)}>{senderName}</div>
              {!message.isRead && <span className="h-2 w-2 shrink-0 rounded-full bg-primary" aria-label="未读" />}
            </div>
            <div className="flex shrink-0 items-center gap-1 sm:hidden">
              <span className="text-xs text-muted-foreground">{formatDate(message.receivedAt)}</span>
              {canOrganize && <Button type="button" variant="ghost" size="icon" aria-label={message.isStarred ? "取消星标" : "添加星标"} className="h-6 w-6 text-muted-foreground hover:text-yellow-500" onClick={(event) => { event.stopPropagation(); onStar() }}>
                <Star className={cn("h-3.5 w-3.5", message.isStarred && "fill-yellow-400 text-yellow-500")} />
              </Button>}
            </div>
          </div>
          <div className="mt-1 flex min-w-0 items-center gap-2 sm:mt-0">
            <span className="truncate font-medium">{messageSubject(message)}</span>
            <span className="hidden min-w-0 truncate text-muted-foreground sm:block">{message.snippet}</span>
            {scheduled && <Badge variant="secondary" className="h-5 shrink-0 rounded-md px-1.5 text-[11px] font-normal">已定时</Badge>}
            {visibleLabels.map((label) => <MailLabelBadge key={label.id} label={label} />)}
            {hiddenLabelCount > 0 && <Badge variant="outline" className="h-5 shrink-0 rounded-md px-1.5 text-[11px] font-normal text-muted-foreground">+{hiddenLabelCount}</Badge>}
            {message.hasAttachments && <Paperclip className="h-3 w-3 shrink-0 text-muted-foreground" />}
          </div>
          <div className={cn("mt-1 line-clamp-2 text-xs sm:hidden", message.isRead ? "text-muted-foreground" : "text-foreground/70")}>{message.snippet}</div>
        </div>
      </div>
      <div className="hidden shrink-0 text-right text-xs text-muted-foreground sm:block">{formatDate(message.receivedAt)}</div>
      {canOrganize && <Button type="button" variant="ghost" size="icon" aria-label={message.isStarred ? "取消星标" : "添加星标"} className="hidden h-6 w-6 text-muted-foreground hover:text-yellow-500 sm:inline-flex" onClick={(event) => { event.stopPropagation(); onStar() }}>
        <Star className={cn("h-3.5 w-3.5", message.isStarred && "fill-yellow-400 text-yellow-500")} />
      </Button>}
    </div>
  )
}

function NewLabelButton({ collapsed, pending, onCreate, editing, onEditingChange }: { collapsed: boolean; pending: boolean; onCreate: (name: string) => void; editing?: boolean; onEditingChange?: (v: boolean) => void }) {
  const [internalEditing, setInternalEditing] = React.useState(false)
  const isEditing = editing ?? internalEditing
  const setEditingState = onEditingChange ?? setInternalEditing
  const [value, setValue] = React.useState("")
  if (collapsed) {
    return (
      <SidebarMenuButton className="justify-center px-0" onClick={() => setEditingState(true)}>
        <Plus className="h-4 w-4" />
      </SidebarMenuButton>
    )
  }
  if (isEditing) {
    return (
      <form
        className="px-2 py-1"
        onSubmit={(event) => {
          event.preventDefault()
          const name = value.trim()
          if (!name) return
          onCreate(name)
          setValue("")
          setEditingState(false)
        }}
      >
        <Input autoFocus value={value} onChange={(event) => setValue(event.target.value)} onBlur={() => { if (!value.trim()) setEditingState(false) }} placeholder="新建标签" disabled={pending} />
      </form>
    )
  }
  return (
    <SidebarMenuButton className="text-muted-foreground" onClick={() => setEditingState(true)}>
      <span>新建标签</span>
    </SidebarMenuButton>
  )
}

function AccountHeader({ collapsed, name, email, darkMode, language, onToggleTheme, onLanguageChange, onSettings }: { collapsed: boolean; name: string; email?: string; darkMode: boolean; language: Language; onToggleTheme: () => void; onLanguageChange: (language: Language) => void; onSettings: () => void }) {
  const displayName = cleanAccountName(name, email)
  const currentLanguage = languageOptions.find((item) => item.value === language) || languageOptions[0]
  if (collapsed) {
    return (
      <div className="flex justify-center">
        <Avatar className="size-[30px] rounded-full">
          <AvatarFallback className="bg-primary text-xs font-semibold text-primary-foreground">{accountInitial(displayName, email)}</AvatarFallback>
        </Avatar>
      </div>
    )
  }
  return (
    <div className="flex items-center justify-between gap-2">
      <div className="flex min-w-0 items-center gap-2">
        <Avatar className="size-[30px] rounded-full">
          <AvatarFallback className="bg-primary text-xs font-semibold text-primary-foreground">{accountInitial(displayName, email)}</AvatarFallback>
        </Avatar>
        <div className="min-w-0 text-[13px]">
          <div className="truncate text-[13px] font-semibold leading-5 text-foreground">{displayName}</div>
        </div>
      </div>
      <div className="flex shrink-0 items-center gap-1">
        <Button type="button" variant="ghost" size="icon" className="size-7 rounded-md text-muted-foreground hover:bg-transparent hover:text-foreground" onClick={onToggleTheme} title={darkMode ? "切换到浅色模式" : "切换到深色模式"} aria-label={darkMode ? "切换到浅色模式" : "切换到深色模式"}>
          {darkMode ? <Sun className="h-3.5 w-3.5 text-amber-500" /> : <Moon className="h-3.5 w-3.5" />}
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button type="button" variant="ghost" size="icon" className="hidden size-7 rounded-md text-muted-foreground hover:bg-transparent hover:text-foreground" aria-label="切换语言" title="切换语言">
              <span className="text-sm font-medium leading-none">{currentLanguage.shortLabel}</span>
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-40">
            {languageOptions.map((item) => (
              <DropdownMenuItem key={item.value} onSelect={() => onLanguageChange(item.value)} className="gap-2">
                <span className="min-w-0 flex-1">{item.label}</span>
                <Check className={cn("h-4 w-4 text-emerald-500", item.value === language ? "opacity-100" : "opacity-0")} />
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
        <Button type="button" variant="ghost" size="icon" className="size-7 rounded-md text-muted-foreground hover:bg-transparent hover:text-foreground" onClick={onSettings} title="设置" aria-label="设置">
          <Settings className="h-3.5 w-3.5" />
        </Button>
      </div>
    </div>
  )
}

function UnreadBadge({ count, tone = "danger" }: { count?: number; tone?: "danger" | "muted" }) {
  if (!count || count <= 0) return null
  return (
    <span className={cn(
      "ml-auto inline-flex h-5 min-w-5 shrink-0 items-center justify-center rounded-full px-1.5 text-[11px] font-semibold leading-none",
      tone === "danger" ? "bg-destructive text-destructive-foreground" : "bg-muted text-muted-foreground"
    )}>
      {count > 99 ? "99+" : count}
    </span>
  )
}

function UnreadSectionStatus({ hasUnread, label }: { hasUnread: boolean; label: string }) {
  if (!hasUnread) return null
  return (
    <span
      className="ml-1 h-2 w-2 shrink-0 rounded-full bg-destructive"
      aria-label={label}
      title={label}
    />
  )
}

function MailboxSwitcher({ collapsed, mailboxes, loading, selectedMailboxId, selectedMailbox, unreadCount, hasCopyAction, onSelect }: { collapsed: boolean; mailboxes: Mailbox[]; loading: boolean; selectedMailboxId: string; selectedMailbox?: Mailbox; unreadCount: number; hasCopyAction: boolean; onSelect: (mailboxId: string) => void }) {
  const [mailboxQuery, setMailboxQuery] = React.useState("")
  const isAllSelected = selectedMailboxId === "all"
  const mailboxUnavailable = loading || mailboxes.length === 0
  const displayAddress = loading ? "全部邮箱" : mailboxes.length === 0 ? "未注册邮箱" : isAllSelected ? "全部邮箱" : selectedMailbox?.address || "选择邮箱"
  const selectedUnreadCount = isAllSelected ? unreadCount : (selectedMailbox?.unreadCount ?? unreadCount)
  const normalizedQuery = mailboxQuery.trim().toLowerCase()
  const showAllMailboxOption = !normalizedQuery || "全部邮箱".includes(normalizedQuery) || "all".includes(normalizedQuery)
  const filteredMailboxes = React.useMemo(() => {
    if (!normalizedQuery) return mailboxes
    return mailboxes.filter((mailbox) => {
      const value = `${mailbox.address} ${mailbox.displayName || ""}`.toLowerCase()
      return value.includes(normalizedQuery)
    })
  }, [mailboxes, normalizedQuery])
  return (
    <DropdownMenu onOpenChange={(open) => { if (!open) setMailboxQuery("") }}>
      <DropdownMenuTrigger asChild>
        <Button disabled={mailboxUnavailable} variant="outline" className={cn("h-8 w-full min-w-0 justify-start gap-1.5 overflow-hidden rounded-md border-input bg-background px-2 text-left font-normal shadow-none hover:bg-background", hasCopyAction && !collapsed && "pr-9", collapsed && "w-8 flex-none justify-center px-0")} title={displayAddress}>
          <Mail className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          {!collapsed && (
            <>
              <span className="min-w-0 flex-1 truncate text-[13px] font-medium">{displayAddress}</span>
              <UnreadBadge count={selectedUnreadCount} />
              {!mailboxUnavailable && <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />}
            </>
          )}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="start"
        className={cn(
          "max-w-[calc(100vw-32px)] p-1",
          collapsed ? "w-[204px]" : "w-[var(--radix-dropdown-menu-trigger-width)] min-w-0"
        )}
      >
        {mailboxes.length > 0 && (
          <div className="px-1 pb-1">
            <Input
              autoFocus
              value={mailboxQuery}
              onChange={(event) => setMailboxQuery(event.target.value)}
              onKeyDown={(event) => event.stopPropagation()}
              placeholder="搜索邮箱..."
              className="h-8 rounded-md bg-background px-2 text-[13px] shadow-none"
            />
          </div>
        )}
        {mailboxes.length === 0 && <DropdownMenuItem disabled>没有可用邮箱</DropdownMenuItem>}
        {mailboxes.length > 0 && showAllMailboxOption && (
          <DropdownMenuItem onSelect={() => onSelect("all")} className={cn("h-8 gap-2 rounded-sm px-2 text-[13px] font-normal", isAllSelected && "bg-accent text-accent-foreground")}>
            <Mail className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
            <span className="min-w-0 flex-1 truncate">全部邮箱</span>
            <UnreadBadge count={unreadCount} />
          </DropdownMenuItem>
        )}
        {filteredMailboxes.map((mailbox) => (
          <DropdownMenuItem key={mailbox.id} onSelect={() => onSelect(mailbox.id)} className={cn("h-8 min-w-0 gap-2 rounded-sm px-2 text-[13px] font-normal", !isAllSelected && selectedMailbox?.id === mailbox.id && "bg-accent text-accent-foreground")}>
            <Mail className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
            <span className="min-w-0 flex-1 truncate" title={mailbox.address}>{mailbox.address}</span>
            <UnreadBadge count={mailbox.unreadCount} />
          </DropdownMenuItem>
        ))}
        {mailboxes.length > 0 && !showAllMailboxOption && filteredMailboxes.length === 0 && (
          <DropdownMenuItem disabled className="h-8 px-2 text-sm font-normal">没有匹配邮箱</DropdownMenuItem>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function cleanAccountName(name: string, email?: string) {
  const value = name.trim()
  if (!value || (email && value.toLowerCase() === email.toLowerCase())) return email?.split("@")[0] || "用户"
  return value
}

function accountInitial(name: string, email?: string) {
  const source = cleanAccountName(name, email)
  const first = Array.from(source.trim())[0]
  return (first || "蓝").toUpperCase()
}

function senderDisplayName(message: MailMessage) {
  const fromName = decodeMimeHeader(message.fromName?.trim() || "")
  if (fromName) return fromName
  return displayNameFromAddress(message.from)
}

function messageSubject(message: MailMessage) {
  return decodeMimeHeader(message.subject?.trim() || "") || "无主题"
}

function displayNameFromAddress(value: string) {
  const text = decodeMimeHeader(value.trim())
  const namedAddress = text.match(/^"?([^"<]+?)"?\s*<[^>]+>$/)
  const name = namedAddress?.[1]?.trim()
  if (name) return name
  const address = text.match(/<([^>]+)>/)?.[1]?.trim() || text
  const localPart = address.split("@")[0]?.trim()
  return localPart || text || "未知发件人"
}

function senderTitle(message: MailMessage) {
  const name = decodeMimeHeader(message.fromName?.trim() || "")
  const from = decodeMimeHeader(message.from)
  return name ? `${name} <${from}>` : from
}

function senderAddress(message: MailMessage) {
  return extractAddress(message.from) || decodeMimeHeader(message.fromName?.trim() || "") || "未知发件人地址"
}

function extractAddress(value?: string) {
  const text = decodeMimeHeader((value || "").trim())
  if (!text) return ""
  const bracketAddress = text.match(/<([^>]+)>/)?.[1]?.trim()
  return bracketAddress || text
}

function cleanAddressList(items?: string[]) {
  return (items || []).map((item) => extractAddress(item)).filter(Boolean)
}

function sameMailTime(a?: string, b?: string) {
  if (!a || !b) return false
  const left = new Date(a).getTime()
  const right = new Date(b).getTime()
  return !Number.isNaN(left) && !Number.isNaN(right) && left === right
}

function MessageMetaPanel({ message, availableLabels, onAddLabel, onRemoveLabel, labelPending }: {
  message: MailMessage
  availableLabels?: MailLabel[]
  onAddLabel?: (label: MailLabel) => void
  onRemoveLabel?: (labelId: string) => void
  labelPending?: boolean
}) {
  const fromName = senderDisplayName(message)
  const fromAddress = senderAddress(message)
  const to = cleanAddressList(message.to)
  const cc = cleanAddressList(message.cc)
  const bcc = cleanAddressList(message.bcc)
  const deliveredTo = extractAddress(message.recipientAddress || message.mailboxAddress || "")
  const showSentAt = Boolean(message.sentAt) && !sameMailTime(message.sentAt, message.receivedAt)
  const labels = message.labels || []

  return (
    <div className="space-y-3 rounded-xl border bg-muted/20 p-3 text-sm">
      <div className="flex min-w-0 items-start gap-3">
        <Avatar className="size-10 shrink-0 rounded-full">
          <AvatarFallback className="bg-primary text-sm font-semibold text-primary-foreground">{accountInitial(fromName, fromAddress)}</AvatarFallback>
        </Avatar>
        <div className="min-w-0 flex-1 space-y-2">
          <MessageMetaRow label="发件人">
            <span className="break-words font-medium text-foreground" title={senderTitle(message)}>{fromName}</span>
          </MessageMetaRow>
          <MessageMetaRow label="发件人地址">
            <span className="break-all">{fromAddress}</span>
          </MessageMetaRow>
          <MessageMetaRow label="收件人">
            <AddressList values={to} empty="未填写收件人" />
          </MessageMetaRow>
          {cc.length > 0 && (
            <MessageMetaRow label="抄送">
              <AddressList values={cc} />
            </MessageMetaRow>
          )}
          {bcc.length > 0 && (
            <MessageMetaRow label="密送">
              <AddressList values={bcc} />
            </MessageMetaRow>
          )}
          {deliveredTo && (
            <MessageMetaRow label="投递邮箱">
              <span className="break-all">{deliveredTo}</span>
            </MessageMetaRow>
          )}
          {showSentAt && (
            <MessageMetaRow label="发送时间">
              <span>{formatDateTime(message.sentAt)}</span>
            </MessageMetaRow>
          )}
          <MessageMetaRow label="接收时间">
            <span>{formatDateTime(message.receivedAt)}</span>
          </MessageMetaRow>
          {availableLabels && onAddLabel && onRemoveLabel && (
            <MessageMetaRow label="标签">
              <div className="flex flex-wrap items-center gap-1.5">
                {labels.map((label) => {
                  return (
                    <Badge key={label.id} variant="outline" className="label-badge group/badge gap-1.5 rounded-md font-normal">
                      <span className="h-2 w-2 shrink-0 rounded-full" style={{ backgroundColor: labelDotColor(label) }} />
                      <span>{label.name}</span>
                      <button
                        type="button"
                        className="label-badge-delete -mr-0.5 flex h-3.5 w-3.5 items-center justify-center rounded-full opacity-0 transition-opacity hover:bg-muted group-hover/badge:opacity-100"
                        onClick={() => onRemoveLabel(label.id)}
                        disabled={labelPending}
                        aria-label={`移除标签 ${label.name}`}
                      >
                        <X className="h-2.5 w-2.5" />
                      </button>
                    </Badge>
                  )
                })}
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <button
                      type="button"
                      className="inline-flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded-md border text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                      aria-label="添加标签"
                    >
                      <Plus className="h-3 w-3" />
                    </button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="start" className="w-48">
                    {availableLabels.length === 0 && <DropdownMenuItem disabled>请先在侧栏新建标签</DropdownMenuItem>}
                    {availableLabels.map((label) => {
                      const active = labels.some((l) => l.id === label.id)
                      return (
                        <DropdownMenuCheckboxItem
                          key={label.id}
                          checked={active}
                          onSelect={(event) => {
                            event.preventDefault()
                            active ? onRemoveLabel(label.id) : onAddLabel(label)
                          }}
                        >
                          <span className="mr-2 h-2 w-2 rounded-full" style={{ backgroundColor: labelDotColor(label) }} />
                          <span>{label.name}</span>
                        </DropdownMenuCheckboxItem>
                      )
                    })}
                  </DropdownMenuContent>
                </DropdownMenu>
              </div>
            </MessageMetaRow>
          )}
        </div>
      </div>
    </div>
  )
}

function MessageMetaRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid gap-1 sm:grid-cols-[5rem_minmax(0,1fr)]">
      <div className="shrink-0 text-xs font-medium text-muted-foreground sm:pt-0.5">{label}</div>
      <div className="min-w-0 text-foreground">{children}</div>
    </div>
  )
}

function AddressList({ values, empty = "无" }: { values: string[]; empty?: string }) {
  if (values.length === 0) return <span className="text-muted-foreground">{empty}</span>
  return (
    <div className="flex min-w-0 flex-wrap gap-1.5">
      {values.map((value, index) => (
        <span key={`${value}-${index}`} className="inline-flex max-w-full rounded-md bg-background px-2 py-0.5 text-xs text-foreground ring-1 ring-border">
          <span className="truncate" title={value}>{value}</span>
        </span>
      ))}
    </div>
  )
}

function MessageRow({
  message,
  active,
  checked,
  scheduled,
  onCheckedChange,
  onClick,
  onContextMenu,
  onStar,
  onArchive,
  onTrash,
  onToggleRead,
  canOrganize,
}: {
  message: MailMessage
  active: boolean
  checked: boolean
  scheduled?: boolean
  onCheckedChange: (checked: boolean) => void
  onClick: () => void
  onContextMenu: (event: React.MouseEvent) => void
  onStar: () => void
  onArchive: () => void
  onTrash: () => void
  onToggleRead: () => void
  canOrganize: boolean
}) {
  const visibleLabels = (message.labels || []).slice(0, 2)
  const hiddenLabelCount = Math.max((message.labels?.length || 0) - visibleLabels.length, 0)
  const senderName = senderDisplayName(message)
  const quickActionsVisible = checked || active
  const quickButtonClass = "h-6 w-6 text-muted-foreground hover:bg-accent hover:text-foreground"
  const archiveLabel = message.folder === "Archive" ? "移回收件箱" : "归档"
  return <div onClick={onClick} onContextMenu={onContextMenu} className={cn(
    "group cursor-pointer border-b border-l-2 px-3 py-2.5 transition-colors",
    message.isRead ? "border-l-transparent hover:bg-accent/60" : "border-l-primary bg-primary/5 font-semibold hover:bg-primary/10",
    active && "mail-selected-row",
    checked && "mail-selected-row"
  )}>
    <div className="flex gap-2.5">
      <Checkbox
        aria-label="选择邮件"
        checked={checked}
        onCheckedChange={(value) => onCheckedChange(value === true)}
        onClick={(event) => event.stopPropagation()}
        className="mt-0.5 shrink-0"
      />
      <div className="min-w-0 flex-1">
        <div className="mb-1 flex items-center justify-between gap-2">
          <div className="flex min-w-0 items-center gap-1.5">
            <div className="min-w-0 truncate text-[13px] font-medium" title={senderTitle(message)}>{senderName}</div>
            {!message.isRead && <span className="h-2 w-2 shrink-0 rounded-full bg-primary" aria-label="未读" />}
          </div>
          <div className="flex shrink-0 items-center gap-0.5">
            {canOrganize && (
              <div className={cn("hidden shrink-0 items-center gap-0.5", quickActionsVisible ? "flex" : "group-hover:flex")}>
                <Button type="button" variant="ghost" size="icon" aria-label={archiveLabel} title={archiveLabel} className={quickButtonClass} onClick={(event) => { event.stopPropagation(); onArchive() }}>
                  <Archive className="h-3.5 w-3.5" />
                </Button>
                <Button type="button" variant="ghost" size="icon" aria-label={message.folder === "Trash" ? "永久删除" : "移入已删除"} title={message.folder === "Trash" ? "永久删除" : "移入已删除"} className={quickButtonClass} onClick={(event) => { event.stopPropagation(); onTrash() }}>
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
                <Button type="button" variant="ghost" size="icon" aria-label={message.isRead ? "标为未读" : "标为已读"} title={message.isRead ? "标为未读" : "标为已读"} className={quickButtonClass} onClick={(event) => { event.stopPropagation(); onToggleRead() }}>
                  {message.isRead ? <Mail className="h-3.5 w-3.5" /> : <MailCheck className="h-3.5 w-3.5" />}
                </Button>
              </div>
            )}
            {canOrganize && <Button
              type="button"
              variant="ghost"
              size="icon"
              aria-label={message.isStarred ? "取消星标" : "添加星标"}
              className="h-6 w-6 text-muted-foreground hover:text-yellow-500"
              onClick={(e) => { e.stopPropagation(); onStar() }}
            >
              <Star className={cn("h-3.5 w-3.5", message.isStarred && "fill-yellow-400 text-yellow-500")} />
            </Button>}
            <div className={cn("text-xs text-muted-foreground", canOrganize && (quickActionsVisible ? "hidden" : "group-hover:hidden"))}>{formatDate(message.receivedAt)}</div>
          </div>
        </div>
        <div className="mb-1 flex min-w-0 items-center gap-2">
          <span className="min-w-0 truncate text-[13px] text-foreground">{messageSubject(message)}</span>
          {scheduled && <Badge variant="secondary" className="h-5 shrink-0 rounded-md px-1.5 text-[11px] font-normal">已定时</Badge>}
          {visibleLabels.map((label) => <MailLabelBadge key={label.id} label={label} />)}
          {hiddenLabelCount > 0 && <Badge variant="outline" className="h-5 shrink-0 rounded-md px-1.5 text-[11px] font-normal text-muted-foreground">+{hiddenLabelCount}</Badge>}
          {message.hasAttachments && <Paperclip className="h-3 w-3 shrink-0 text-muted-foreground" />}
        </div>
        <div className={cn("line-clamp-2 text-xs leading-5", message.isRead ? "text-muted-foreground" : "text-foreground/70")}>{message.snippet}</div>
      </div>
    </div>
  </div>
}

function MailLabelBadge({ label }: { label: MailLabel }) {
  return (
    <Badge variant="outline" className="shrink-0 gap-1.5 rounded-md font-normal">
      <span className="h-2 w-2 shrink-0 rounded-full" style={{ backgroundColor: labelDotColor(label) }} />
      {label.name}
    </Badge>
  )
}

function labelDotColor(label: MailLabel) {
  return label.color?.trim() || generateLabelColor(label.name).backgroundColor
}

function ComposeDialog({ mailboxes, mailbox, open, draft, limits, canSend, canManageDrafts, canSchedule, canManageSignatures, onOpenChange, onSent }: { mailboxes: Mailbox[]; mailbox?: Mailbox; open: boolean; draft?: ComposeDraft; limits?: PermissionLimits; canSend: boolean; canManageDrafts: boolean; canSchedule: boolean; canManageSignatures: boolean; onOpenChange: (v: boolean) => void; onSent: () => void }) {
  const { toast } = useToast()
  const qc = useQueryClient()
  const initialMailboxId = mailboxes.find((item) => item.id === draft?.mailboxId)?.id || mailboxes.find((item) => item.id === mailbox?.id)?.id || mailboxes[0]?.id || ""
  const [senderMailboxId, setSenderMailboxId] = React.useState(initialMailboxId)
  const senderMailbox = React.useMemo(() => mailboxes.find((item) => item.id === senderMailboxId) || mailboxes[0], [mailboxes, senderMailboxId])
  const [files, setFiles] = React.useState<File[]>([])
  const [draftAttachments, setDraftAttachments] = React.useState<SendPayload["attachments"]>([])
  const [attachmentsTouched, setAttachmentsTouched] = React.useState(false)
  const [draftId, setDraftId] = React.useState(draft?.id || "")
  const [toValue, setToValue] = React.useState(draft?.to || "")
  const [ccValue, setCcValue] = React.useState(draft?.cc || "")
  const [bccValue, setBccValue] = React.useState(draft?.bcc || "")
  const [subjectValue, setSubjectValue] = React.useState(draft?.subject || "")
  const [draftStatus, setDraftStatus] = React.useState<"idle" | "saving" | "saved" | "error">("idle")
  const [lastSavedAt, setLastSavedAt] = React.useState<Date | null>(null)
  const [closing, setClosing] = React.useState(false)
  const [scheduleDialogOpen, setScheduleDialogOpen] = React.useState(false)
  const [sendIntent, setSendIntent] = React.useState<ComposeSendIntent | null>(null)
  const sendStartedRef = React.useRef(false)
  const lastSavedPayloadRef = React.useRef("")
  const initializedSessionRef = React.useRef("")
  const [showCc, setShowCc] = React.useState(Boolean(draft?.cc))
  const [showBcc, setShowBcc] = React.useState(Boolean(draft?.bcc))
  const [sendSeparately, setSendSeparately] = React.useState(false)
  const defaultSignature = useQuery({ queryKey: ["signature", "default", senderMailbox?.id], queryFn: () => api.defaultSignature(senderMailbox?.id), enabled: open && !!senderMailbox?.id && canManageSignatures })
  const signatureText = defaultSignature.data?.signature?.content || ""
  const composerText = draft?.html || (draft?.text !== undefined ? draft.text : signatureText ? `\n\n-- \n${signatureText}` : "")
  const [body, setBody] = React.useState<ComposerValue>(() => draft?.html !== undefined ? htmlComposerValue(draft.html) : plainTextComposerValue(composerText))
  const activeMailboxId = senderMailbox?.id || ""
  const maxAttachmentBytes = attachmentLimitBytes(limits)
  const maxAttachmentText = maxAttachmentBytes > 0 ? formatBytes(maxAttachmentBytes) : "不限"
  const composePayload = React.useMemo<DraftPayload>(() => ({
    mailboxId: activeMailboxId,
    to: splitEmails(toValue),
    cc: showCc ? splitEmails(ccValue) : [],
    bcc: showBcc ? splitEmails(bccValue) : [],
    subject: subjectValue,
    text: body.text,
    html: body.html || plainTextToHtml(body.text),
    ...(attachmentsTouched ? { attachments: draftAttachments } : {}),
  }), [activeMailboxId, toValue, showCc, ccValue, showBcc, bccValue, subjectValue, body, attachmentsTouched, draftAttachments])
  const hasDraftContent = open && !!activeMailboxId && (toValue.trim() || ccValue.trim() || bccValue.trim() || subjectValue.trim() || body.text.trim() || body.html.trim())
  const send = useMutation({
    mutationFn: async (payloads: SendPayload[]) => {
      const sent: MailMessage[] = []
      for (const payload of payloads) sent.push(await api.send(payload))
      return sent
    },
    onSuccess: async (_, payloads) => {
      let draftDeleteFailed = false
      if (draftId) {
        try {
          await api.deleteDraft(draftId)
        } catch {
          draftDeleteFailed = true
        }
      }
      toast({ title: payloads.length > 1 ? `已分别发送 ${payloads.length} 封邮件` : "发送成功", description: draftDeleteFailed ? "邮件已发送，但原草稿未能自动删除" : undefined })
      setFiles([])
      setDraftId("")
      onSent()
    },
    onError: (e) => toast({ title: "发送失败", description: e.message }),
  })
  const scheduleSend = useMutation({
    mutationFn: (payload: SendPayload & { draftId?: string; sendAt: string }) => api.scheduleSend(payload),
    onSuccess: (scheduled) => {
      sendStartedRef.current = true
      toast({ title: `已定时发送 ${formatDateTime(scheduled.sendAt)}` })
      setScheduleDialogOpen(false)
      setFiles([])
      void Promise.all([
        qc.invalidateQueries({ queryKey: ["messages"] }),
        qc.invalidateQueries({ queryKey: ["folders"] }),
        qc.invalidateQueries({ queryKey: ["mail-stats"] }),
        qc.invalidateQueries({ queryKey: ["scheduled-sends"] }),
      ])
      onSent()
    },
    onError: (e) => toast({ title: "定时发送失败", description: e.message }),
  })

  React.useEffect(() => {
    if (!open) {
      initializedSessionRef.current = ""
      return
    }
    const sessionKey = draft?.key || draft?.id || "new"
    if (initializedSessionRef.current === sessionKey) return
    initializedSessionRef.current = sessionKey
    sendStartedRef.current = false
    const nextMailboxId = mailboxes.find((item) => item.id === draft?.mailboxId)?.id || mailboxes.find((item) => item.id === mailbox?.id)?.id || mailboxes[0]?.id || ""
    const nextShowCc = Boolean(draft?.cc)
    const nextShowBcc = Boolean(draft?.bcc)
    const nextBody = draft?.html !== undefined ? htmlComposerValue(draft.html) : plainTextComposerValue(draft?.text || "")
    lastSavedPayloadRef.current = JSON.stringify({
      mailboxId: nextMailboxId,
      to: splitEmails(draft?.to || ""),
      cc: nextShowCc ? splitEmails(draft?.cc || "") : [],
      bcc: nextShowBcc ? splitEmails(draft?.bcc || "") : [],
      subject: draft?.subject || "",
      text: nextBody.text,
      html: nextBody.html || plainTextToHtml(nextBody.text),
      draftId: draft?.id || "",
    })
    setSenderMailboxId(nextMailboxId)
    setDraftId(draft?.id || "")
    setToValue(draft?.to || "")
    setCcValue(draft?.cc || "")
    setBccValue(draft?.bcc || "")
    setSubjectValue(draft?.subject || "")
    setBody(nextBody)
    setDraftStatus("idle")
    setLastSavedAt(null)
    setShowCc(nextShowCc)
    setShowBcc(nextShowBcc)
    setSendSeparately(false)
    setFiles(draft?.files || [])
    setDraftAttachments([])
    setAttachmentsTouched(false)
  }, [open, draft?.key, draft?.id, draft?.mailboxId, draft?.to, draft?.cc, draft?.bcc, draft?.subject, draft?.text, draft?.html, draft?.files, mailbox?.id, mailboxes])

  React.useEffect(() => {
    let cancelled = false
    Promise.all(files.map(fileToAttachment)).then((attachments) => {
      if (!cancelled) setDraftAttachments(attachments)
    })
    return () => { cancelled = true }
  }, [files])

  React.useEffect(() => {
    if (!open || closing || sendStartedRef.current || !hasDraftContent || !canManageDrafts) return
    const payloadKey = JSON.stringify({ ...composePayload, draftId })
    if (payloadKey === lastSavedPayloadRef.current) return
    const timer = window.setTimeout(async () => {
      try {
        setDraftStatus("saving")
        const saved = await api.saveDraft(composePayload, draftId || undefined)
        setDraftId(saved.id)
        lastSavedPayloadRef.current = JSON.stringify({ ...composePayload, draftId: saved.id })
        setLastSavedAt(new Date())
        setDraftStatus("saved")
        await Promise.all([
          qc.invalidateQueries({ queryKey: ["messages"] }),
          qc.invalidateQueries({ queryKey: ["folders"] }),
          qc.invalidateQueries({ queryKey: ["mail-stats"] }),
        ])
      } catch {
        setDraftStatus("error")
      }
    }, 5000)
    return () => window.clearTimeout(timer)
  }, [open, closing, hasDraftContent, composePayload, draftId, qc, canManageDrafts])

  function buildSendWarnings(attachmentsCount: number) {
    const warnings: string[] = []
    const normalizedBody = `${body.text}\n${stripHtml(body.html)}`.toLowerCase()
    if (!subjectValue.trim()) warnings.push("这封邮件还没有主题。")
    if (!body.text.trim() && !htmlContainsMeaningfulContent(body.html)) warnings.push("正文还是空的。")
    if (/(附件|附上|见附件|attached|attachment)/i.test(normalizedBody) && attachmentsCount === 0) warnings.push("正文提到了附件，但还没有添加附件。")
    return warnings
  }

  function confirmOrRun(intent: Omit<ComposeSendIntent, "description"> & { warnings: string[]; defaultDescription?: string }) {
    if (intent.warnings.length === 0) {
      intent.onConfirm()
      return
    }
    setSendIntent({
      title: intent.title,
      description: intent.defaultDescription ? `${intent.defaultDescription}\n${intent.warnings.join("\n")}` : intent.warnings.join("\n"),
      confirmText: intent.confirmText,
      onConfirm: intent.onConfirm,
    })
  }

  function addFiles(nextFiles: File[]) {
    if (nextFiles.length === 0) return
    const allowed = maxAttachmentBytes > 0 ? nextFiles.filter((file) => file.size <= maxAttachmentBytes) : nextFiles
    const blockedCount = nextFiles.length - allowed.length
    if (blockedCount > 0) {
      toast({ title: "附件超过配额上限", description: `当前单个附件上限 ${maxAttachmentText}` })
    }
    if (allowed.length > 0) {
      setAttachmentsTouched(true)
      setFiles((current) => [...current, ...allowed])
    }
  }

  function attachmentsWithinLimit() {
    if (maxAttachmentBytes <= 0) return true
    if (files.every((file) => file.size <= maxAttachmentBytes)) return true
    toast({ title: "附件超过配额上限", description: `当前单个附件上限 ${maxAttachmentText}` })
    return false
  }

  async function prepareSend() {
    if (!canSend) return
    if (!senderMailbox) return
    if (!attachmentsWithinLimit()) return
    const attachments = await Promise.all(files.map(fileToAttachment))
    const to = splitEmails(toValue)
    const cc = showCc ? splitEmails(ccValue) : []
    const bcc = showBcc ? splitEmails(bccValue) : []
    if (to.length === 0) {
      toast({ title: "请填写收件人" })
      return
    }
    const text = body.text
    const html = body.html || plainTextToHtml(text)
    const payload: SendPayload = { mailboxId: senderMailbox.id, to, cc, bcc, subject: subjectValue, text, html, attachments }
    const separateRecipients = Array.from(new Set([...to, ...cc, ...bcc]))
    const payloads = sendSeparately && separateRecipients.length > 0
      ? separateRecipients.map((recipient): SendPayload => ({ ...payload, to: [recipient], cc: [], bcc: [] }))
      : [payload]
    confirmOrRun({
      title: "确认发送这封邮件？",
      confirmText: sendSeparately && payloads.length > 1 ? "继续分别发送" : "继续发送",
      warnings: buildSendWarnings(attachments.length),
      onConfirm: () => {
        sendStartedRef.current = true
        setSendIntent(null)
        send.mutate(payloads)
      },
    })
  }

  async function submit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()
    if (!senderMailbox) {
      toast({ title: "请选择发件邮箱" })
      return
    }
    await prepareSend()
  }
  async function scheduleAt(sendAt: string) {
    if (!canSchedule) return
    if (!senderMailbox) {
      toast({ title: "请选择发件邮箱" })
      return
    }
    if (!attachmentsWithinLimit()) return
    const attachments = await Promise.all(files.map(fileToAttachment))
    const to = splitEmails(toValue)
    const cc = showCc ? splitEmails(ccValue) : []
    const bcc = showBcc ? splitEmails(bccValue) : []
    if (to.length === 0) {
      toast({ title: "请填写收件人" })
      return
    }
    const payload: SendPayload & { draftId?: string; sendAt: string } = {
      mailboxId: senderMailbox.id,
      to,
      cc,
      bcc,
      subject: subjectValue,
      text: body.text,
      html: body.html || plainTextToHtml(body.text),
      attachments,
      draftId: draftId || undefined,
      sendAt,
    }
    confirmOrRun({
      title: "确认定时发送？",
      confirmText: "继续定时发送",
      defaultDescription: `发送时间：${formatDateTime(sendAt)}`,
      warnings: buildSendWarnings(attachments.length),
      onConfirm: () => {
        sendStartedRef.current = true
        setSendIntent(null)
        scheduleSend.mutate(payload)
      },
    })
  }

  async function closeCompose() {
    if (closing) return
    if (!canManageDrafts || sendStartedRef.current || !hasDraftContent) {
      onOpenChange(false)
      return
    }
    setClosing(true)
    setDraftStatus("saving")
    try {
      const attachments = await Promise.all(files.map(fileToAttachment))
      const payload: DraftPayload = { ...composePayload, attachments }
      const saved = await api.saveDraft(payload, draftId || undefined)
      setDraftId(saved.id)
      lastSavedPayloadRef.current = JSON.stringify({ ...payload, draftId: saved.id })
      setLastSavedAt(new Date())
      setDraftStatus("saved")
      await Promise.all([
        qc.invalidateQueries({ queryKey: ["messages"] }),
        qc.invalidateQueries({ queryKey: ["folders"] }),
        qc.invalidateQueries({ queryKey: ["mail-stats"] }),
      ])
      onOpenChange(false)
    } catch (error) {
      setDraftStatus("error")
      toast({ title: "草稿保存失败", description: error instanceof Error ? error.message : "请稍后重试" })
    } finally {
      setClosing(false)
    }
  }
  return (
    <Dialog open={open} onOpenChange={(nextOpen) => { if (nextOpen) onOpenChange(true); else void closeCompose() }}>
      <DialogContent
        className="flex h-svh w-screen max-w-none overflow-hidden p-0 sm:h-[min(88vh,48rem)] sm:w-[min(92vw,56rem)]"
        onInteractOutside={(event) => event.preventDefault()}
        onPointerDownOutside={(event) => event.preventDefault()}
      >
        <form key={draft?.key || "new"} className="flex min-h-0 min-w-0 flex-1 flex-col" onSubmit={submit}>
          <DialogHeader className="border-b bg-muted/20 px-4 py-3 text-left sm:px-5">
            <DialogTitle className="flex min-w-0 flex-col gap-1 pr-8 sm:flex-row sm:items-center sm:justify-between sm:gap-4 sm:pr-6">
              <span>{draftId ? "编辑草稿" : "写信"}</span>
              <span className={cn("text-xs font-normal", draftStatus === "error" ? "text-destructive" : "text-muted-foreground")}>
                {draftStatus === "saving" ? "正在保存草稿..." : draftStatus === "saved" && lastSavedAt ? `草稿已保存 ${lastSavedAt.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}` : draftStatus === "error" ? "草稿保存失败" : ""}
              </span>
            </DialogTitle>
          </DialogHeader>
          <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
            <ComposeField label="发件邮箱">
              {mailboxes.length > 1 ? (
                <Select value={senderMailbox?.id || ""} onValueChange={setSenderMailboxId}>
                  <SelectTrigger aria-label="发件邮箱" className="h-10 flex-1 rounded-none border-0 px-0 shadow-none focus:ring-0">
                    <SelectValue placeholder="选择发件邮箱" />
                  </SelectTrigger>
                  <SelectContent className="max-w-[calc(100vw-2rem)]">
                    {mailboxes.map((item) => <SelectItem key={item.id} value={item.id}>{item.address}</SelectItem>)}
                  </SelectContent>
                </Select>
              ) : (
                <Input value={senderMailbox?.address || "未选择"} readOnly className="h-10 flex-1 rounded-none border-0 px-0 shadow-none focus-visible:ring-0" />
              )}
            </ComposeField>
            <ComposeField
              label="收件人"
              action={
                <div className="flex shrink-0 flex-wrap items-center justify-start gap-1 text-sm sm:justify-end sm:gap-2">
                  <Button type="button" variant="ghost" size="sm" className="h-8 px-2 font-normal" onClick={() => setShowCc((value) => !value)}>抄送</Button>
                  <Button type="button" variant="ghost" size="sm" className="h-8 px-2 font-normal" onClick={() => setShowBcc((value) => !value)}>密送</Button>
                  <div className="flex items-center gap-2 rounded-md px-2 py-1">
                    <Checkbox id="compose-send-separately" checked={sendSeparately} onCheckedChange={(value) => setSendSeparately(value === true)} />
                    <Label htmlFor="compose-send-separately" className="cursor-pointer text-sm font-normal">分别发送</Label>
                  </div>
                </div>
              }
            >
              <Input name="to" placeholder="name@example.com，多个地址用逗号或空格分隔" value={toValue} onChange={(event) => setToValue(event.target.value)} required className="h-10 flex-1 rounded-none border-0 px-0 shadow-none focus-visible:ring-0" />
            </ComposeField>
            {showCc && (
              <ComposeField label="抄送">
                <Input name="cc" placeholder="cc@example.com" value={ccValue} onChange={(event) => setCcValue(event.target.value)} className="h-10 flex-1 rounded-none border-0 px-0 shadow-none focus-visible:ring-0" />
              </ComposeField>
            )}
            {showBcc && (
              <ComposeField label="密送">
                <Input name="bcc" placeholder="bcc@example.com" value={bccValue} onChange={(event) => setBccValue(event.target.value)} className="h-10 flex-1 rounded-none border-0 px-0 shadow-none focus-visible:ring-0" />
              </ComposeField>
            )}
            <ComposeField label="主题">
              <Input name="subject" placeholder="输入主题" value={subjectValue} onChange={(event) => setSubjectValue(event.target.value)} className="h-10 flex-1 rounded-none border-0 px-0 shadow-none focus-visible:ring-0" />
            </ComposeField>
            {defaultSignature.isError && (
              <div className="flex items-center justify-between gap-3 border-b bg-destructive/5 px-4 py-2 text-xs text-destructive sm:px-6" role="alert">
                <span>默认签名读取失败</span>
                <Button type="button" variant="ghost" size="sm" className="h-7 text-destructive hover:text-destructive" onClick={() => { void defaultSignature.refetch() }}>
                  <RefreshCcw className="h-3.5 w-3.5" />重新读取
                </Button>
              </div>
            )}
            <MailBodyComposer
              defaultValue={composerText}
              defaultHtml={draft?.html}
              files={files}
              signatureText={signatureText}
              maxAttachmentText={maxAttachmentText}
              onChange={setBody}
              onPickFiles={addFiles}
              onRemoveFile={(index) => { setAttachmentsTouched(true); setFiles((current) => current.filter((_, itemIndex) => itemIndex !== index)) }}
            />
          </div>
          <DialogFooter className="flex flex-row items-center justify-between gap-3 border-t bg-muted/15 px-4 py-3 sm:px-5">
            {canSend && (
              <div className="flex min-w-0 items-center">
                <Button className={cn("compose-send-button min-h-10 px-4", canSchedule && "rounded-r-none")} disabled={send.isPending || !senderMailbox}><Send className="h-4 w-4" />{send.isPending ? "发送中..." : "发送"}</Button>
                {canSchedule && (
                  <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                      <Button type="button" size="icon" className="compose-send-button h-10 w-9 shrink-0 rounded-l-none border-l border-white/30 px-0" title="发送选项" aria-label="发送选项" disabled={send.isPending || scheduleSend.isPending || !senderMailbox}>
                        <ChevronDown className="h-4 w-4" />
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end" side="top" sideOffset={8} collisionPadding={12} className="w-[120px] min-w-[120px]">
                      <DropdownMenuItem className={composerMenuItemClass} onSelect={() => setScheduleDialogOpen(true)}><Clock3 className="h-4 w-4" />定时发送</DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                )}
              </div>
            )}
            <Button type="button" variant="ghost" className="min-h-10 px-3" disabled={closing} onClick={() => { void closeCompose() }}>{closing ? "保存中..." : "取消"}</Button>
          </DialogFooter>
        </form>
        <ScheduleSendDialog open={scheduleDialogOpen} pending={scheduleSend.isPending} onOpenChange={setScheduleDialogOpen} onConfirm={scheduleAt} />
        <ConfirmDialog
          open={!!sendIntent}
          title={sendIntent?.title || ""}
          description={sendIntent?.description}
          confirmText={sendIntent?.confirmText || "继续"}
          pending={send.isPending || scheduleSend.isPending}
          onOpenChange={(nextOpen) => { if (!nextOpen) setSendIntent(null) }}
          onConfirm={() => sendIntent?.onConfirm()}
        />
      </DialogContent>
    </Dialog>
  )
}

function ComposeField({ label, children, action }: { label: string; children: React.ReactNode; action?: React.ReactNode }) {
  return (
    <div className="flex min-h-12 flex-col gap-1 border-b px-4 py-1.5 transition-colors focus-within:bg-muted/20 sm:flex-row sm:items-center sm:gap-2 sm:px-5">
      <Label className="shrink-0 text-sm font-normal text-muted-foreground sm:w-16">{label}</Label>
      <div className="flex min-w-0 flex-1 flex-col gap-2 sm:flex-row sm:items-center">
        {children}
        {action}
      </div>
    </div>
  )
}

function ScheduleSendDialog({ open, pending, onOpenChange, onConfirm }: { open: boolean; pending: boolean; onOpenChange: (open: boolean) => void; onConfirm: (sendAt: string) => void }) {
  const [value, setValue] = React.useState("")
  const { toast } = useToast()
  const presets = React.useMemo(() => scheduledSendPresets(), [open])

  React.useEffect(() => {
    if (open) setValue(defaultScheduledSendValue())
  }, [open])

  function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const date = new Date(value)
    if (!value || Number.isNaN(date.getTime()) || !date.getTime()) {
      toast({ title: "请选择发送时间" })
      return
    }
    if (date.getTime() <= Date.now() + 30_000) {
      toast({ title: "发送时间需要晚于当前时间" })
      return
    }
    onConfirm(date.toISOString())
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <form className="grid gap-4" onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>定时发送</DialogTitle>
          </DialogHeader>
          <div className="grid grid-cols-2 gap-2">
            {presets.map((preset) => (
              <Button
                type="button"
                key={preset.label}
                variant={value === preset.value ? "secondary" : "outline"}
                className="justify-start font-normal"
                onClick={() => setValue(preset.value)}
              >
                <Clock3 className="h-4 w-4" />{preset.label}
              </Button>
            ))}
          </div>
          <div className="grid gap-2">
            <Label htmlFor="schedule-send-at">发送时间</Label>
            <Input id="schedule-send-at" type="datetime-local" value={value} min={toDateTimeLocalValue(new Date(Date.now() + 60_000))} onChange={(event) => setValue(event.target.value)} />
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>取消</Button>
            <Button type="submit" disabled={pending}>{pending ? "正在设置..." : "确认定时"}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

type ComposerValue = { text: string; html: string }
type InsertDialogState = { kind: "link" | "image"; selectedText: string; url?: string; alt?: string; editing?: boolean }
type InsertDialogValue = { url: string; text: string; alt: string }
const composerFontOptions = ["Arial", "Georgia", "Times New Roman", "Courier New", "Microsoft YaHei"]
const composerFontSizeOptions = [
  ["2", "小号"],
  ["3", "标准"],
  ["4", "中号"],
  ["5", "大号"],
] as const
const composerFontSizeValueByKey: Record<string, string> = { "2": "13px", "3": "16px", "4": "20px", "5": "24px" }
const composerParagraphOptions: ReadonlyArray<{ key: string; label: string; level?: 1 | 2 | 3 }> = [
  { key: "paragraph", label: "正文" },
  { key: "heading-1", label: "标题 1", level: 1 as const },
  { key: "heading-2", label: "标题 2", level: 2 as const },
  { key: "heading-3", label: "标题 3", level: 3 as const },
] as const
const composerTextColors = [["#111827", "默认"], ["#dc2626", "红色"], ["#2563eb", "蓝色"], ["#16a34a", "绿色"], ["#9333ea", "紫色"]] as const
const composerHighlightColors = [["transparent", "无高亮"], ["#fef3c7", "黄色"], ["#dcfce7", "绿色"], ["#dbeafe", "蓝色"], ["#fce7f3", "粉色"]] as const
const composerEmojiOptions = ["😀", "😄", "😊", "🙂", "😉", "😍", "😘", "😎", "🤔", "👍", "👏", "🙏", "💪", "🎉", "🔥", "✨", "❤️", "✅", "📌", "📅", "☕", "💡", "🚀", "⭐"]
const composerMenuItemClass = "min-h-9 rounded-md px-3 text-sm transition-colors data-[highlighted]:bg-primary/10 data-[highlighted]:font-semibold data-[highlighted]:text-foreground hover:bg-primary/10 hover:font-semibold hover:text-foreground"

function normalizeFontName(value: string) {
  const cleaned = value.replace(/["']/g, "").split(",")[0]?.trim() || ""
  if (!cleaned || cleaned === "默认字体") return ""
  if (/microsoft yahei/i.test(cleaned) || cleaned.includes("微软雅黑")) return "Microsoft YaHei"
  const lower = cleaned.toLowerCase()
  return composerFontOptions.find((font) => {
    const option = font.toLowerCase()
    return lower === option || lower.includes(option) || option.includes(lower)
  }) || ""
}

function normalizeFontSize(value: string) {
  const cleaned = value.trim().toLowerCase()
  if (!cleaned) return ""
  if (composerFontSizeOptions.some(([size]) => size === cleaned)) return cleaned
  const px = Number(cleaned.replace("px", ""))
  if (Number.isFinite(px)) {
    if (px <= 13) return "2"
    if (px <= 17) return "3"
    if (px <= 22) return "4"
    return "5"
  }
  if (cleaned.includes("small")) return "2"
  if (cleaned.includes("large") || cleaned.includes("x-large")) return "5"
  if (cleaned.includes("medium") || cleaned.includes("normal")) return "3"
  return ""
}

function fontLabel(value: string) {
  const normalized = normalizeFontName(value)
  if (!normalized) return "默认字体"
  return normalized === "Microsoft YaHei" ? "微软雅黑" : normalized
}

function fontSizeLabel(value: string) {
  const normalized = normalizeFontSize(value) || "3"
  return composerFontSizeOptions.find(([size]) => size === normalized)?.[1] || "标准"
}

function paragraphStyleLabel(editor?: Editor | null) {
  if (!editor) return "正文"
  for (const option of composerParagraphOptions) {
    if (option.level && editor.isActive("heading", { level: option.level })) return option.label
  }
  return "正文"
}

function normalizeInsertUrl(value: string, kind: InsertDialogState["kind"]) {
  const trimmed = value.trim()
  if (!trimmed) return ""
  const allowed = kind === "image" ? /^(https?:|cid:|data:image\/|\/)/i : /^(https?:|mailto:|tel:|#|\/)/i
  return allowed.test(trimmed) ? trimmed : `https://${trimmed}`
}

const ScheduleCardNode = Node.create({
  name: "scheduleCard",
  group: "block",
  atom: true,
  selectable: true,
  draggable: false,
  addAttributes() {
    return {
      title: { default: "" },
      time: { default: "" },
      duration: { default: "" },
      reminder: { default: "" },
      repeat: { default: "" },
      location: { default: "" },
      description: { default: "" },
    }
  },
  parseHTML() {
    return [{ tag: "div[data-schedule-card]" }]
  },
  renderHTML({ HTMLAttributes }) {
    const { title, time, duration, reminder, repeat, location, description } = HTMLAttributes
    const rows = [
      ["时间", time],
      ["持续", duration],
      ["提醒", reminder],
      ["重复", repeat],
      location ? ["位置", location] : undefined,
      description ? ["描述", description] : undefined,
    ].filter(Boolean) as string[][]
    return [
      "div",
      mergeAttributes(HTMLAttributes, {
        "data-schedule-card": "true",
        style: "border:1px solid #d4d4d8;border-radius:8px;padding:14px 16px;margin:16px 0;background:#fafafa;",
      }),
      ["div", { style: "font-weight:600;font-size:16px;margin-bottom:10px;" }, title || "日程"],
      ...rows.map(([label, value]) => ["div", { style: "margin:6px 0;" }, ["span", { style: "color:#71717a;" }, `${label}：`], value]),
    ]
  },
})

function composerInitialHtml(defaultValue: string, defaultHtml?: string) {
  return sanitizeComposerHtml(defaultHtml !== undefined ? defaultHtml : plainTextToHtml(defaultValue)) || "<p></p>"
}

function composerValueFromEditor(editor: Editor): ComposerValue {
  const text = editor.getText({ blockSeparator: "\n" }).replace(/\u00a0/g, " ").trimEnd()
  const html = sanitizeComposerHtml(editor.getHTML())
  if (!text.trim() && !htmlContainsMeaningfulContent(html)) return { text: "", html: "" }
  return { text, html: html || plainTextToHtml(text) }
}

function editorTextSelection(editor: Editor) {
  const { from, to, empty } = editor.state.selection
  if (empty) return ""
  return editor.state.doc.textBetween(from, to, " ").trim()
}

function selectedImageAttributes(editor: Editor) {
  const attrs = editor.getAttributes("image") as { src?: string; alt?: string }
  return attrs.src ? attrs : null
}

function scheduleToNodeAttributes(schedule: ScheduleDraft) {
  const start = parseScheduleStart(schedule)
  const end = schedule.allDay ? new Date(start.getTime() + 24 * 60 * 60 * 1000) : new Date(start.getTime() + schedule.durationMinutes * 60 * 1000)
  return {
    title: schedule.title,
    time: schedule.allDay ? formatDate(start.toISOString()) : `${formatDateTime(start.toISOString())} - ${formatTimeOnly(end)}`,
    duration: schedule.allDay ? "全天" : durationLabel(schedule.durationMinutes),
    reminder: reminderLabel(schedule.reminderMinutes),
    repeat: repeatLabel(schedule.repeat),
    location: schedule.location,
    description: schedule.description,
  }
}

function MailBodyComposer({ defaultValue, defaultHtml, files, signatureText, maxAttachmentText, onChange, onPickFiles, onRemoveFile }: { defaultValue: string; defaultHtml?: string; files: File[]; signatureText: string; maxAttachmentText: string; onChange: (value: ComposerValue) => void; onPickFiles: (files: File[]) => void; onRemoveFile: (index: number) => void }) {
  const fileInputRef = React.useRef<HTMLInputElement>(null)
  const dirtyRef = React.useRef(false)
  const lastDefaultRef = React.useRef(`${defaultValue}\n${defaultHtml || ""}`)
  const isMobile = useIsMobile()
  const [formatOpen, setFormatOpen] = React.useState(() => typeof window === "undefined" ? true : window.innerWidth >= 768)
  const [scheduleOpen, setScheduleOpen] = React.useState(false)
  const [emojiOpen, setEmojiOpen] = React.useState(false)
  const [insertDialog, setInsertDialog] = React.useState<InsertDialogState | null>(null)
  const [previewOpen, setPreviewOpen] = React.useState(false)
  const [empty, setEmpty] = React.useState(!defaultValue.trim())
  const [selectionVersion, setSelectionVersion] = React.useState(0)

  React.useEffect(() => {
    setFormatOpen(!isMobile)
  }, [isMobile])

  const editor = useEditor({
    extensions: [
      StarterKit.configure({ link: false }),
      TextStyle,
      Color,
      BackgroundColor,
      FontFamily,
      FontSize,
      LinkExtension.configure({
        openOnClick: false,
        enableClickSelection: true,
        HTMLAttributes: { target: "_blank", rel: "noopener noreferrer" },
      }),
      ImageExtension.configure({ allowBase64: true, HTMLAttributes: { style: "max-width:100%;height:auto;border-radius:8px;margin:12px 0;" } }),
      TextAlign.configure({ types: ["heading", "paragraph"] }),
      Placeholder.configure({ placeholder: "输入正文" }),
      ScheduleCardNode,
    ],
    content: composerInitialHtml(defaultValue, defaultHtml),
    editorProps: {
      attributes: {
        class: "mail-html min-h-[220px] min-w-0 flex-1 overflow-y-auto px-4 py-4 text-base leading-7 outline-none sm:min-h-[260px] sm:px-5",
        "aria-label": "正文",
        spellcheck: "true",
        autocorrect: "on",
      },
      handlePaste(view, event) {
        const clipboard = event.clipboardData
        if (!clipboard) return false
        const html = clipboard.getData("text/html")
        const text = clipboard.getData("text/plain")
        if (!html && !text) return false
        event.preventDefault()
        const content = html ? sanitizeComposerHtml(html) : plainTextToHtml(text)
        const container = document.createElement("div")
        container.innerHTML = content || plainTextToHtml(text)
        const slice = ProseMirrorDOMParser.fromSchema(view.state.schema).parseSlice(container)
        view.dispatch(view.state.tr.replaceSelection(slice).scrollIntoView())
        return true
      },
    },
    onCreate({ editor }) {
      const next = composerValueFromEditor(editor)
      onChange(next)
      setEmpty(!next.text.trim() && !htmlContainsMeaningfulContent(next.html))
    },
    onUpdate({ editor }) {
      dirtyRef.current = true
      const next = composerValueFromEditor(editor)
      onChange(next)
      setEmpty(!next.text.trim() && !htmlContainsMeaningfulContent(next.html))
    },
    onSelectionUpdate() {
      setSelectionVersion((value) => value + 1)
    },
    onTransaction() {
      setSelectionVersion((value) => value + 1)
    },
  })

  React.useEffect(() => {
    if (!editor) return
    const defaultKey = `${defaultValue}\n${defaultHtml || ""}`
    if (defaultKey === lastDefaultRef.current) return
    lastDefaultRef.current = defaultKey
    if (!dirtyRef.current || editor.isEmpty) {
      const next = defaultHtml !== undefined ? htmlComposerValue(defaultHtml) : plainTextComposerValue(defaultValue)
      editor.commands.setContent(next.html || "<p></p>", { emitUpdate: false })
      onChange(next)
      setEmpty(!next.text.trim() && !htmlContainsMeaningfulContent(next.html))
    }
  }, [editor, defaultValue, defaultHtml, onChange])

  const textStyleAttributes = editor?.getAttributes("textStyle") as { fontFamily?: string; fontSize?: string; color?: string; backgroundColor?: string } | undefined
  const activeFont = normalizeFontName(textStyleAttributes?.fontFamily || "")
  const activeFontSize = normalizeFontSize(textStyleAttributes?.fontSize || "") || "3"
  const activeColor = textStyleAttributes?.color || ""
  const activeHighlight = textStyleAttributes?.backgroundColor || ""
  const characterCount = editor?.getText().replace(/\s/g, "").length || 0
  void selectionVersion

  function applyFont(font: string) {
    editor?.chain().focus().setFontFamily(font).run()
  }

  function applyFontSize(size: string) {
    const value = composerFontSizeValueByKey[size]
    if (value) editor?.chain().focus().setFontSize(value).run()
  }

  function openInsertDialog(kind: InsertDialogState["kind"]) {
    if (!editor) return
    if (kind === "link") {
      const attrs = editor.getAttributes("link") as { href?: string }
      setInsertDialog({ kind, selectedText: editorTextSelection(editor), url: attrs.href || "", editing: Boolean(attrs.href) })
      return
    }
    const imageAttrs = selectedImageAttributes(editor)
    setInsertDialog({ kind, selectedText: "", url: imageAttrs?.src || "", alt: imageAttrs?.alt || "", editing: Boolean(imageAttrs?.src) })
  }

  function confirmInsert(value: InsertDialogValue) {
    if (!editor || !insertDialog) return
    const url = normalizeInsertUrl(value.url, insertDialog.kind)
    if (!url) return
    if (insertDialog.kind === "link") {
      const text = value.text.trim() || insertDialog.selectedText || value.url.trim()
      if (editor.state.selection.empty && !insertDialog.editing) {
        editor.chain().focus().insertContent(`<a href="${escapeHtml(url)}" target="_blank" rel="noopener noreferrer">${escapeHtml(text)}</a>`).run()
      } else {
        if (value.text.trim() && value.text.trim() !== insertDialog.selectedText) editor.chain().focus().insertContent(escapeHtml(text)).run()
        editor.chain().focus().extendMarkRange("link").setLink({ href: url, target: "_blank", rel: "noopener noreferrer" }).run()
      }
      return
    }
    if (insertDialog.editing) {
      editor.chain().focus().updateAttributes("image", { src: url, alt: value.alt.trim() }).run()
      return
    }
    editor.chain().focus().setImage({ src: url, alt: value.alt.trim() }).run()
  }

  function insertSignature() {
    if (!editor || !signatureText.trim()) return
    editor.chain().focus().insertContent(`<p><br></p><p>-- <br>${plainTextToHtmlFragment(signatureText)}</p>`).run()
  }

  function insertSchedule(schedule: ScheduleDraft) {
    if (!editor) return
    const normalized = normalizeSchedule(schedule)
    editor.chain().focus().insertContent({ type: "scheduleCard", attrs: scheduleToNodeAttributes(normalized) }).run()
    onPickFiles([scheduleToFile(normalized)])
  }

  function insertEmoji(emoji: string) {
    editor?.chain().focus().insertContent(emoji).run()
    setEmojiOpen(false)
  }

  function handlePickedFiles(event: React.ChangeEvent<HTMLInputElement>) {
    const nextFiles = Array.from(event.currentTarget.files || [])
    if (nextFiles.length > 0) onPickFiles(nextFiles)
    event.currentTarget.value = ""
  }

  return (
    <div className="flex min-h-[300px] min-w-0 flex-1 flex-col bg-background sm:min-h-[360px]">
      <Input ref={fileInputRef} type="file" multiple className="hidden" onChange={handlePickedFiles} />
      <div className="flex min-h-11 flex-wrap items-center gap-1 overflow-visible border-b px-3 py-2 sm:px-5">
        <ToolbarButton label="撤销" disabled={!editor?.can().undo()} onClick={() => editor?.chain().focus().undo().run()}><Undo2 className="h-4 w-4" /></ToolbarButton>
        <ToolbarButton label="重做" disabled={!editor?.can().redo()} onClick={() => editor?.chain().focus().redo().run()}><Redo2 className="h-4 w-4" /></ToolbarButton>
        <Separator orientation="vertical" className="mx-2 h-6" />
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button type="button" variant="ghost" size="sm" className="h-8 gap-1 rounded-md px-2 font-normal hover:bg-accent hover:shadow-sm" onMouseDown={(event) => event.preventDefault()}>
              <Plus className="h-4 w-4" />插入<ChevronDown className="h-3.5 w-3.5" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start">
            <DropdownMenuItem className={composerMenuItemClass} onSelect={() => fileInputRef.current?.click()}><Paperclip className="h-4 w-4" />附件</DropdownMenuItem>
            <DropdownMenuItem className={composerMenuItemClass} onSelect={() => openInsertDialog("link")}><Link className="h-4 w-4" />链接</DropdownMenuItem>
            <DropdownMenuItem className={composerMenuItemClass} onSelect={() => openInsertDialog("image")}><Image className="h-4 w-4" />图片链接</DropdownMenuItem>
            <DropdownMenuItem className={composerMenuItemClass} onSelect={() => editor?.chain().focus().setHorizontalRule().run()}><span className="h-4 w-4 border-t border-current" aria-hidden />分隔线</DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        <span className="px-1 text-xs text-muted-foreground" title={`单个附件上限 ${maxAttachmentText}`}>附件上限 {maxAttachmentText}</span>
        <ToolbarTextButton label="日程" icon={<Calendar className="h-4 w-4" />} onClick={() => setScheduleOpen(true)} />
        <DropdownMenu open={emojiOpen} onOpenChange={setEmojiOpen}>
          <DropdownMenuTrigger asChild>
            <Button type="button" variant={emojiOpen ? "secondary" : "ghost"} size="sm" className={cn("h-8 gap-1.5 rounded-md px-2 font-normal hover:bg-accent hover:shadow-sm", emojiOpen && "border border-primary/30 bg-primary/10 text-primary")} onMouseDown={(event) => event.preventDefault()}>
              <Smile className="h-4 w-4" />表情
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="w-64 p-2">
            <div className="grid grid-cols-8 gap-1">
              {composerEmojiOptions.map((emoji) => (
                <Button key={emoji} type="button" variant="ghost" size="icon" className="h-8 w-8 rounded-md text-lg" aria-label={`插入表情 ${emoji}`} title={`插入 ${emoji}`} onClick={() => insertEmoji(emoji)}>
                  {emoji}
                </Button>
              ))}
            </div>
          </DropdownMenuContent>
        </DropdownMenu>
        <ToolbarTextButton label="格式" icon={<Type className="h-4 w-4" />} active={formatOpen} onClick={() => setFormatOpen((value) => !value)} />
        <div className="flex items-center gap-1">
          <ToolbarTextButton label="预览" icon={<Eye className="h-4 w-4" />} active={previewOpen} onClick={() => setPreviewOpen(true)} />
          <ToolbarTextButton label="签名" icon={<Signature className="h-4 w-4" />} onClick={insertSignature} disabled={!signatureText.trim()} />
        </div>
      </div>
      {formatOpen && (
        <div className="flex min-h-14 flex-wrap items-center gap-1 overflow-visible border-b bg-muted/40 px-3 py-2 sm:px-5">
          <ToolbarButton label="清除格式" disabled={!editor} onClick={() => editor?.chain().focus().unsetAllMarks().clearNodes().run()}><Eraser className="h-4 w-4" /></ToolbarButton>
          <Separator orientation="vertical" className="mx-1 h-6" />
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button type="button" variant="ghost" size="sm" className={cn("h-8 min-w-[76px] justify-between rounded-md border border-transparent px-2 font-normal hover:border-border hover:bg-accent", paragraphStyleLabel(editor) !== "正文" && "border-primary/35 bg-primary/10 text-primary")} onMouseDown={(event) => event.preventDefault()} disabled={!editor}>
                {paragraphStyleLabel(editor)}<ChevronDown className="h-3.5 w-3.5" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start">
              {composerParagraphOptions.map((option) => (
                <DropdownMenuItem key={option.key} className={composerMenuItemClass} onSelect={() => option.level ? editor?.chain().focus().toggleHeading({ level: option.level }).run() : editor?.chain().focus().setParagraph().run()}>
                  <Check className={cn("h-4 w-4", paragraphStyleLabel(editor) === option.label ? "opacity-100" : "opacity-0")} />
                  <span className={cn(option.level === 1 && "text-lg font-semibold", option.level === 2 && "text-base font-semibold", option.level === 3 && "text-sm font-semibold")}>{option.label}</span>
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button type="button" variant="ghost" size="sm" className={cn("h-8 min-w-[112px] justify-between rounded-md border border-transparent px-2 font-normal hover:border-border hover:bg-accent hover:shadow-sm", activeFont && "border-primary/35 bg-primary/10 text-primary shadow-sm")} onMouseDown={(event) => event.preventDefault()} disabled={!editor}>
                <span className="truncate">{fontLabel(activeFont)}</span><ChevronDown className="h-3.5 w-3.5" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start">
              {composerFontOptions.map((font) => (
                <DropdownMenuItem key={font} className={composerMenuItemClass} onSelect={() => applyFont(font)}>
                  <Check className={cn("h-4 w-4", activeFont === font ? "opacity-100" : "opacity-0")} />
                  <span style={{ fontFamily: font }}>{font}</span>
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button type="button" variant="ghost" size="sm" className={cn("h-8 min-w-[84px] justify-between rounded-md border border-transparent px-2 font-normal hover:border-border hover:bg-accent hover:shadow-sm", activeFontSize !== "3" && "border-primary/35 bg-primary/10 text-primary shadow-sm")} onMouseDown={(event) => event.preventDefault()} disabled={!editor}>
                {fontSizeLabel(activeFontSize)}<ChevronDown className="h-3.5 w-3.5" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start">
              {composerFontSizeOptions.map(([size, label]) => (
                <DropdownMenuItem key={size} className={composerMenuItemClass} onSelect={() => applyFontSize(size)}>
                  <Check className={cn("h-4 w-4", activeFontSize === size ? "opacity-100" : "opacity-0")} />
                  {label}
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
          <Separator orientation="vertical" className="mx-1 h-6" />
          <ToolbarButton label="加粗" active={editor?.isActive("bold")} disabled={!editor} onClick={() => editor?.chain().focus().toggleBold().run()}><Bold className="h-4 w-4" /></ToolbarButton>
          <ToolbarButton label="斜体" active={editor?.isActive("italic")} disabled={!editor} onClick={() => editor?.chain().focus().toggleItalic().run()}><Italic className="h-4 w-4" /></ToolbarButton>
          <ToolbarButton label="下划线" active={editor?.isActive("underline")} disabled={!editor} onClick={() => editor?.chain().focus().toggleUnderline().run()}><Underline className="h-4 w-4" /></ToolbarButton>
          <ToolbarButton label="删除线" active={editor?.isActive("strike")} disabled={!editor} onClick={() => editor?.chain().focus().toggleStrike().run()}><Strikethrough className="h-4 w-4" /></ToolbarButton>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button type="button" variant="ghost" size="icon" className={cn("h-8 w-8 rounded-md border border-transparent text-muted-foreground hover:border-border hover:bg-accent hover:text-foreground hover:shadow-sm", activeColor && "border-primary/35 bg-primary/10 text-primary shadow-sm")} title="文字颜色" aria-label="文字颜色" onMouseDown={(event) => event.preventDefault()} disabled={!editor}>
                <Type className="h-4 w-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="w-36">
              {composerTextColors.map(([color, label]) => (
                <DropdownMenuItem key={color} className={composerMenuItemClass} onSelect={() => color === "#111827" ? editor?.chain().focus().unsetColor().run() : editor?.chain().focus().setColor(color).run()}>
                  <Check className={cn("h-4 w-4", activeColor === color || (!activeColor && color === "#111827") ? "opacity-100" : "opacity-0")} />
                  <span className="h-3 w-3 rounded-full border" style={{ backgroundColor: color }} />{label}
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button type="button" variant="ghost" size="icon" className={cn("h-8 w-8 rounded-md border border-transparent text-muted-foreground hover:border-border hover:bg-accent hover:text-foreground hover:shadow-sm", activeHighlight && "border-primary/35 bg-primary/10 text-primary shadow-sm")} title="高亮" aria-label="高亮" onMouseDown={(event) => event.preventDefault()} disabled={!editor}>
                <Highlighter className="h-4 w-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start">
              {composerHighlightColors.map(([color, label]) => (
                <DropdownMenuItem key={color} className={composerMenuItemClass} onSelect={() => color === "transparent" ? editor?.chain().focus().unsetBackgroundColor().run() : editor?.chain().focus().setBackgroundColor(color).run()}>
                  <Check className={cn("h-4 w-4", activeHighlight === color || (!activeHighlight && color === "transparent") ? "opacity-100" : "opacity-0")} />
                  <span className="h-3 w-3 rounded-sm border" style={{ backgroundColor: color }} />{label}
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
          <Separator orientation="vertical" className="mx-1 h-6" />
          <ToolbarButton label="无序列表" active={editor?.isActive("bulletList")} disabled={!editor} onClick={() => editor?.chain().focus().toggleBulletList().run()}><List className="h-4 w-4" /></ToolbarButton>
          <ToolbarButton label="有序列表" active={editor?.isActive("orderedList")} disabled={!editor} onClick={() => editor?.chain().focus().toggleOrderedList().run()}><ListOrdered className="h-4 w-4" /></ToolbarButton>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button type="button" variant="ghost" size="icon" className="h-8 w-8 rounded-md text-muted-foreground hover:bg-accent hover:text-foreground" title="更多格式" aria-label="更多格式" onMouseDown={(event) => event.preventDefault()} disabled={!editor}>
                <Ellipsis className="h-4 w-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-44">
              <DropdownMenuItem className={composerMenuItemClass} disabled={!editor?.can().liftListItem("listItem")} onSelect={() => editor?.chain().focus().liftListItem("listItem").run()}><IndentDecrease className="h-4 w-4" />减少缩进</DropdownMenuItem>
              <DropdownMenuItem className={composerMenuItemClass} disabled={!editor?.can().sinkListItem("listItem")} onSelect={() => editor?.chain().focus().sinkListItem("listItem").run()}><IndentIncrease className="h-4 w-4" />增加缩进</DropdownMenuItem>
              <DropdownMenuItem className={composerMenuItemClass} onSelect={() => editor?.chain().focus().setTextAlign("left").run()}><AlignLeft className="h-4 w-4" />左对齐</DropdownMenuItem>
              <DropdownMenuItem className={composerMenuItemClass} onSelect={() => editor?.chain().focus().setTextAlign("center").run()}><AlignCenter className="h-4 w-4" />居中</DropdownMenuItem>
              <DropdownMenuItem className={composerMenuItemClass} onSelect={() => editor?.chain().focus().setTextAlign("right").run()}><AlignRight className="h-4 w-4" />右对齐</DropdownMenuItem>
              <DropdownMenuItem className={composerMenuItemClass} onSelect={() => editor?.chain().focus().toggleBlockquote().run()}><Quote className="h-4 w-4" />引用</DropdownMenuItem>
              <DropdownMenuItem className={composerMenuItemClass} onSelect={() => editor?.chain().focus().toggleCodeBlock().run()}><Code2 className="h-4 w-4" />代码块</DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
          <span className="ml-auto whitespace-nowrap px-1 text-xs tabular-nums text-muted-foreground" aria-live="polite">{characterCount} 字</span>
        </div>
      )}
      <div className={cn(
        "composer-editor relative flex min-h-[220px] min-w-0 flex-1 overflow-hidden border-b focus-within:bg-card/40 sm:min-h-[260px]",
        "[&_.ProseMirror]:min-h-[220px] [&_.ProseMirror]:min-w-0 [&_.ProseMirror]:w-full [&_.ProseMirror]:max-w-full [&_.ProseMirror]:flex-1 [&_.ProseMirror]:overflow-x-hidden [&_.ProseMirror]:overflow-y-auto [&_.ProseMirror]:px-4 [&_.ProseMirror]:py-4 [&_.ProseMirror]:text-base [&_.ProseMirror]:leading-7 [&_.ProseMirror]:outline-none [&_.ProseMirror]:[overflow-wrap:anywhere] sm:[&_.ProseMirror]:min-h-[260px] sm:[&_.ProseMirror]:px-5",
        "[&_.ProseMirror_p.is-editor-empty:first-child::before]:pointer-events-none [&_.ProseMirror_p.is-editor-empty:first-child::before]:float-left [&_.ProseMirror_p.is-editor-empty:first-child::before]:h-0 [&_.ProseMirror_p.is-editor-empty:first-child::before]:text-muted-foreground [&_.ProseMirror_p.is-editor-empty:first-child::before]:content-[attr(data-placeholder)]",
        "[&_.ProseMirror_a]:break-all [&_.ProseMirror_p]:max-w-full [&_.ProseMirror_ul]:list-disc [&_.ProseMirror_ol]:list-decimal [&_.ProseMirror_ul]:pl-6 [&_.ProseMirror_ol]:pl-6 [&_.ProseMirror_blockquote]:border-l-4 [&_.ProseMirror_blockquote]:border-border [&_.ProseMirror_blockquote]:pl-4 [&_.ProseMirror_blockquote]:text-muted-foreground [&_.ProseMirror_pre]:max-w-full [&_.ProseMirror_pre]:whitespace-pre-wrap [&_.ProseMirror_pre]:break-words [&_.ProseMirror_pre]:rounded-md [&_.ProseMirror_pre]:bg-muted [&_.ProseMirror_pre]:p-3",
        empty && "bg-background"
      )}>
        <EditorContent editor={editor} className="flex min-h-0 min-w-0 flex-1 overflow-hidden" />
      </div>
      {files.length > 0 && (
        <div className="border-t px-4 py-3 sm:px-5">
          <div className="flex flex-wrap gap-2">
            {files.map((file, index) => (
              <Badge key={`${file.name}-${file.size}-${index}`} variant="outline" className="h-8 gap-2 rounded-md px-2 font-normal">
                <Paperclip className="h-3.5 w-3.5" />
                <span className="max-w-48 truncate">{file.name}</span>
                <span className="text-muted-foreground">{formatBytes(file.size)}</span>
                <Button type="button" variant="ghost" size="icon" className="h-5 w-5 rounded-md" aria-label={`移除附件 ${file.name}`} title="移除附件" onClick={() => onRemoveFile(index)}>
                  <X className="h-3.5 w-3.5" />
                </Button>
              </Badge>
            ))}
          </div>
        </div>
      )}
      <InsertContentDialog state={insertDialog} onOpenChange={(open) => { if (!open) setInsertDialog(null) }} onConfirm={confirmInsert} />
      <ScheduleDialog open={scheduleOpen} onOpenChange={setScheduleOpen} onConfirm={(schedule) => { insertSchedule(schedule); setScheduleOpen(false) }} />
      <Dialog open={previewOpen} onOpenChange={setPreviewOpen}>
        <DialogContent className="w-[min(92vw,44rem)] max-w-none">
          <DialogHeader>
            <DialogTitle>邮件预览</DialogTitle>
          </DialogHeader>
          <div className="mail-html max-h-[60vh] max-w-full overflow-x-hidden overflow-y-auto rounded-md border bg-background p-5 text-sm leading-7 [overflow-wrap:anywhere] [&_a]:break-all [&_pre]:max-w-full [&_pre]:whitespace-pre-wrap [&_pre]:break-words" dangerouslySetInnerHTML={{ __html: sanitizeComposerHtml(editor?.getHTML() || "") || "<p></p>" }} />
        </DialogContent>
      </Dialog>
    </div>
  )
}

function ToolbarTextButton({ label, icon, active, disabled, onClick }: { label: string; icon: React.ReactNode; active?: boolean; disabled?: boolean; onClick?: () => void }) {
  return (
    <Button type="button" variant={active ? "secondary" : "ghost"} size="sm" className={cn("h-8 gap-1.5 rounded-md px-2 font-normal transition-all hover:bg-accent hover:text-foreground hover:shadow-sm", active && "border border-primary/30 bg-primary/10 text-primary shadow-sm")} title={label} aria-label={label} aria-pressed={active || undefined} onMouseDown={(event) => event.preventDefault()} onClick={onClick} disabled={disabled}>
      {icon}{label}
    </Button>
  )
}

function InsertContentDialog({ state, onOpenChange, onConfirm }: { state: InsertDialogState | null; onOpenChange: (open: boolean) => void; onConfirm: (value: InsertDialogValue) => void }) {
  const kind = state?.kind || "link"
  const [url, setUrl] = React.useState("")
  const [text, setText] = React.useState("")
  const [alt, setAlt] = React.useState("")

  React.useEffect(() => {
    if (!state) return
    setUrl(state.url || "")
    setText(state.kind === "link" ? state.selectedText : "")
    setAlt(state.alt || "")
  }, [state])

  function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!url.trim()) return
    onConfirm({ url, text, alt })
    onOpenChange(false)
  }

  return (
    <Dialog open={!!state} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <form className="grid gap-4" onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>{kind === "link" ? (state?.editing ? "编辑链接" : "插入链接") : (state?.editing ? "编辑图片" : "插入图片")}</DialogTitle>
          </DialogHeader>
          <div className="grid gap-2">
            <Label htmlFor="composer-insert-url">{kind === "link" ? "链接地址" : "图片地址"}</Label>
            <Input id="composer-insert-url" value={url} onChange={(event) => setUrl(event.target.value)} placeholder={kind === "link" ? "https://example.com" : "https://example.com/image.png"} autoFocus />
          </div>
          {kind === "link" ? (
            <div className="grid gap-2">
              <Label htmlFor="composer-insert-text">显示文字</Label>
              <Input id="composer-insert-text" value={text} onChange={(event) => setText(event.target.value)} placeholder="默认使用链接地址" />
            </div>
          ) : (
            <div className="grid gap-2">
              <Label htmlFor="composer-insert-alt">替代文字</Label>
              <Input id="composer-insert-alt" value={alt} onChange={(event) => setAlt(event.target.value)} placeholder="图片说明" />
            </div>
          )}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>取消</Button>
            <Button type="submit">{state?.editing ? "更新" : "插入"}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

type ScheduleDraft = {
  title: string
  start: string
  durationMinutes: number
  reminderMinutes: number
  repeat: "none" | "daily" | "weekly" | "monthly" | "yearly"
  allDay: boolean
  customDuration: boolean
  customReminder: boolean
  lunar: boolean
  location: string
  description: string
}

const durationOptions = [
  { value: "15", label: "15分钟" },
  { value: "30", label: "30分钟" },
  { value: "60", label: "1小时" },
  { value: "120", label: "2小时" },
  { value: "1440", label: "1天" },
]
const reminderOptions = [
  { value: "0", label: "准时" },
  { value: "5", label: "5分钟前" },
  { value: "15", label: "15分钟前" },
  { value: "30", label: "30分钟前" },
  { value: "60", label: "1小时前" },
  { value: "1440", label: "1天前" },
]
const repeatOptions = [
  { value: "none", label: "永不" },
  { value: "daily", label: "每天" },
  { value: "weekly", label: "每周" },
  { value: "monthly", label: "每月" },
  { value: "yearly", label: "每年" },
] as const

function ScheduleDialog({ open, onOpenChange, onConfirm }: { open: boolean; onOpenChange: (open: boolean) => void; onConfirm: (schedule: ScheduleDraft) => void }) {
  const [duration, setDuration] = React.useState("60")
  const [reminder, setReminder] = React.useState("15")
  const [repeat, setRepeat] = React.useState<ScheduleDraft["repeat"]>("none")
  const [allDay, setAllDay] = React.useState(false)
  const [customDuration, setCustomDuration] = React.useState(false)
  const [customReminder, setCustomReminder] = React.useState(false)
  const [lunar, setLunar] = React.useState(false)
  const defaultStart = React.useMemo(() => defaultScheduleStartValue(), [open])
  const { toast } = useToast()

  function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const title = String(form.get("title") || "").trim()
    if (!title) {
      toast({ title: "请输入日程主题" })
      return
    }
    const durationMinutes = customDuration ? Number(form.get("customDuration") || 60) : Number(duration)
    const reminderMinutes = customReminder ? Number(form.get("customReminder") || 15) : Number(reminder)
    onConfirm({
      title,
      start: String(form.get("start") || defaultStart),
      durationMinutes: Math.max(1, durationMinutes || 60),
      reminderMinutes: Math.max(0, reminderMinutes || 0),
      repeat,
      allDay,
      customDuration,
      customReminder,
      lunar,
      location: String(form.get("location") || ""),
      description: String(form.get("description") || ""),
    })
    event.currentTarget.reset()
    setDuration("60")
    setReminder("15")
    setRepeat("none")
    setAllDay(false)
    setCustomDuration(false)
    setCustomReminder(false)
    setLunar(false)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[min(92vw,36rem)] max-w-none">
        <DialogHeader>
          <DialogTitle>新建日程</DialogTitle>
        </DialogHeader>
        <form className="space-y-5" onSubmit={submit}>
          <Input name="title" placeholder="输入日程主题" className="h-11 border-0 border-b px-0 text-lg shadow-none focus-visible:ring-0" />
          <div className="grid gap-4">
            <ScheduleRow label="开始">
              <Input name="start" type={allDay ? "date" : "datetime-local"} defaultValue={allDay ? defaultStart.slice(0, 10) : defaultStart} className="h-11" />
              <CheckLabel id="schedule-all-day" label="全天" checked={allDay} onCheckedChange={setAllDay} />
            </ScheduleRow>
            <ScheduleRow label="持续">
              {customDuration ? (
                <Input name="customDuration" type="number" min={1} defaultValue="60" className="h-11" />
              ) : (
                <Select value={duration} onValueChange={setDuration}>
                  <SelectTrigger className="h-11"><SelectValue /></SelectTrigger>
                  <SelectContent>{durationOptions.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}</SelectContent>
                </Select>
              )}
              <CheckLabel id="schedule-custom-duration" label="自定义" checked={customDuration} onCheckedChange={setCustomDuration} />
            </ScheduleRow>
            <ScheduleRow label="提醒">
              {customReminder ? (
                <Input name="customReminder" type="number" min={0} defaultValue="15" className="h-11" />
              ) : (
                <Select value={reminder} onValueChange={setReminder}>
                  <SelectTrigger className="h-11"><SelectValue /></SelectTrigger>
                  <SelectContent>{reminderOptions.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}</SelectContent>
                </Select>
              )}
              <CheckLabel id="schedule-custom-reminder" label="自定义" checked={customReminder} onCheckedChange={setCustomReminder} />
            </ScheduleRow>
            <ScheduleRow label="重复">
              <Select value={repeat} onValueChange={(value) => setRepeat(value as ScheduleDraft["repeat"])}>
                <SelectTrigger className="h-11"><SelectValue /></SelectTrigger>
                <SelectContent>{repeatOptions.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}</SelectContent>
              </Select>
              <CheckLabel id="schedule-lunar" label="农历" checked={lunar} onCheckedChange={setLunar} />
            </ScheduleRow>
            <ScheduleRow label="位置">
              <Input name="location" placeholder="请输入位置" className="h-11" />
            </ScheduleRow>
            <ScheduleRow label="描述">
              <Input name="description" placeholder="输入描述" className="h-11" />
            </ScheduleRow>
          </div>
          <DialogFooter>
            <Button type="submit">确定</Button>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>取消</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function ScheduleRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid gap-2 sm:grid-cols-[3rem_minmax(0,1fr)_5.5rem] sm:items-center">
      <Label className="text-base font-normal">{label}</Label>
      {children}
    </div>
  )
}

function CheckLabel({ id, label, checked, onCheckedChange }: { id: string; label: string; checked: boolean; onCheckedChange: (checked: boolean) => void }) {
  return (
    <div className="flex items-center gap-2">
      <Checkbox id={id} checked={checked} onCheckedChange={(value) => onCheckedChange(value === true)} />
      <Label htmlFor={id} className="cursor-pointer text-sm font-normal">{label}</Label>
    </div>
  )
}

function ToolbarButton({ label, children, active, onClick, disabled }: { label: string; children: React.ReactNode; active?: boolean; onClick?: () => void; disabled?: boolean }) {
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon"
      className={cn(
        "h-8 w-8 rounded-md border border-transparent text-muted-foreground transition-all hover:border-border hover:bg-accent hover:text-foreground hover:shadow-sm",
        active && "border-primary/35 bg-primary/10 text-primary shadow-sm hover:bg-primary/15 hover:text-primary"
      )}
      title={label}
      aria-label={label}
      aria-pressed={active || undefined}
      onMouseDown={(event) => event.preventDefault()}
      onClick={onClick}
      disabled={disabled}
    >
      {children}
    </Button>
  )
}

function splitEmails(s: string) { return s.split(/[;,，\s]+/).map((v) => v.trim()).filter(Boolean) }
function defaultScheduledSendValue() {
  const date = new Date(Date.now() + 30 * 60 * 1000)
  const minute = date.getMinutes()
  date.setMinutes(minute + (5 - (minute % 5 || 5)))
  return toDateTimeLocalValue(date)
}
function scheduledSendPresets() {
  return [
    { label: "30 分钟后", value: toDateTimeLocalValue(new Date(Date.now() + 30 * 60 * 1000)) },
    { label: "2 小时后", value: toDateTimeLocalValue(new Date(Date.now() + 2 * 60 * 60 * 1000)) },
    { label: "明早 9 点", value: toDateTimeLocalValue(nextMorningAtNine()) },
    { label: "下周一 9 点", value: toDateTimeLocalValue(nextMondayAtNine()) },
  ]
}
function nextMorningAtNine() {
  const date = new Date()
  date.setDate(date.getDate() + 1)
  date.setHours(9, 0, 0, 0)
  return date
}
function nextMondayAtNine() {
  const date = new Date()
  const day = date.getDay()
  const daysUntilMonday = (8 - day) % 7 || 7
  date.setDate(date.getDate() + daysUntilMonday)
  date.setHours(9, 0, 0, 0)
  return date
}
function defaultScheduleStartValue() {
  const date = new Date()
  date.setMinutes(date.getMinutes() + (60 - (date.getMinutes() % 60 || 60)))
  return toDateTimeLocalValue(date)
}
function toDateTimeLocalValue(date: Date) {
  const pad = (value: number) => String(value).padStart(2, "0")
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}
function normalizeSchedule(schedule: ScheduleDraft): ScheduleDraft {
  return { ...schedule, title: schedule.title.trim(), location: schedule.location.trim(), description: schedule.description.trim() }
}
function scheduleToFile(schedule: ScheduleDraft) {
  const ics = scheduleToIcs(schedule)
  const filename = `${safeFilename(schedule.title || "schedule")}.ics`
  return new File([ics], filename, { type: "text/calendar;charset=utf-8" })
}
function scheduleToIcs(schedule: ScheduleDraft) {
  const start = parseScheduleStart(schedule)
  const end = schedule.allDay ? new Date(start.getTime() + 24 * 60 * 60 * 1000) : new Date(start.getTime() + schedule.durationMinutes * 60 * 1000)
  const uid = `${Date.now()}-${Math.random().toString(36).slice(2)}@lanqin-email`
  const lines = [
    "BEGIN:VCALENDAR",
    "VERSION:2.0",
    "PRODID:-//NewSzxcn Email//Webmail//CN",
    "CALSCALE:GREGORIAN",
    "METHOD:PUBLISH",
    "BEGIN:VEVENT",
    `UID:${uid}`,
    `DTSTAMP:${toIcsDateTime(new Date())}`,
    schedule.allDay ? `DTSTART;VALUE=DATE:${toIcsDate(start)}` : `DTSTART:${toIcsDateTime(start)}`,
    schedule.allDay ? `DTEND;VALUE=DATE:${toIcsDate(end)}` : `DTEND:${toIcsDateTime(end)}`,
    `SUMMARY:${escapeIcs(schedule.title)}`,
    schedule.location ? `LOCATION:${escapeIcs(schedule.location)}` : "",
    schedule.description ? `DESCRIPTION:${escapeIcs(schedule.description)}` : "",
    schedule.repeat !== "none" ? `RRULE:FREQ=${schedule.repeat.toUpperCase()}` : "",
  ].filter(Boolean)
  if (schedule.reminderMinutes > 0) {
    lines.push("BEGIN:VALARM", `TRIGGER:-PT${schedule.reminderMinutes}M`, "ACTION:DISPLAY", `DESCRIPTION:${escapeIcs(schedule.title)}`, "END:VALARM")
  }
  lines.push("END:VEVENT", "END:VCALENDAR")
  return `${lines.join("\r\n")}\r\n`
}
function parseScheduleStart(schedule: ScheduleDraft) {
  const value = schedule.allDay ? `${schedule.start.slice(0, 10)}T00:00` : schedule.start
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? new Date() : date
}
function toIcsDateTime(date: Date) {
  const pad = (value: number) => String(value).padStart(2, "0")
  return `${date.getUTCFullYear()}${pad(date.getUTCMonth() + 1)}${pad(date.getUTCDate())}T${pad(date.getUTCHours())}${pad(date.getUTCMinutes())}${pad(date.getUTCSeconds())}Z`
}
function toIcsDate(date: Date) {
  const pad = (value: number) => String(value).padStart(2, "0")
  return `${date.getFullYear()}${pad(date.getMonth() + 1)}${pad(date.getDate())}`
}
function formatTimeOnly(date: Date) {
  return date.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" })
}
function durationLabel(minutes: number) {
  if (minutes % 1440 === 0) return `${minutes / 1440}天`
  if (minutes % 60 === 0) return `${minutes / 60}小时`
  return `${minutes}分钟`
}
function reminderLabel(minutes: number) {
  if (minutes <= 0) return "准时"
  return `${durationLabel(minutes)}前`
}
function repeatLabel(repeat: ScheduleDraft["repeat"]) {
  return ({ none: "永不", daily: "每天", weekly: "每周", monthly: "每月", yearly: "每年" } as Record<ScheduleDraft["repeat"], string>)[repeat]
}
function safeFilename(value: string) {
  return value.trim().replace(/[\\/:*?"<>|]+/g, "-").replace(/\s+/g, "-").slice(0, 64) || "schedule"
}
function escapeIcs(value: string) {
  return value.replace(/\\/g, "\\\\").replace(/\n/g, "\\n").replace(/,/g, "\\,").replace(/;/g, "\\;")
}
function plainTextComposerValue(value: string): ComposerValue { return { text: value, html: plainTextToHtml(value) } }
function htmlComposerValue(value: string): ComposerValue {
  const html = sanitizeComposerHtml(value || "")
  const text = stripHtml(html)
  return { text, html: html || plainTextToHtml(text) }
}
function plainTextToHtml(value: string) {
  const normalized = value.replace(/\r\n/g, "\n")
  if (!normalized.trim()) return ""
  return sanitizeComposerHtml(normalized.split(/\n{2,}/).map((paragraph) => `<p>${plainTextToHtmlFragment(paragraph) || "<br>"}</p>`).join(""))
}
function plainTextToHtmlFragment(value: string) { return value.split("\n").map((line) => escapeHtml(line)).join("<br>") }
function escapeHtml(value: string) {
  return value.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;").replace(/'/g, "&#39;")
}
function buildMailFrameSrcDoc(bodyHtml: string, bodyText: string) {
  const rawBody = bodyHtml.trim() ? bodyHtml : `<pre>${escapeHtml(bodyText || "")}</pre>`
  const sanitized = DOMPurify.sanitize(rawBody, {
    ADD_ATTR: ["style", "type", "align", "valign", "bgcolor", "border", "cellpadding", "cellspacing", "width", "height"],
    ADD_TAGS: ["html", "head", "body", "style", "center", "font"],
    WHOLE_DOCUMENT: /<html[\s>]/i.test(rawBody) || /<body[\s>]/i.test(rawBody),
  })
  if (/<html[\s>]/i.test(sanitized) || /<body[\s>]/i.test(sanitized)) {
    const hasHead = /<head[\s>]/i.test(sanitized)
    const withBase = hasHead
      ? sanitized.replace(/<head([^>]*)>/i, `<head$1><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><base target="_blank">${mailFrameBaseStyle()}`)
      : sanitized.replace(/<html([^>]*)>/i, `<html$1><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><base target="_blank">${mailFrameBaseStyle()}</head>`)
    return /<!doctype/i.test(withBase) ? withBase : `<!doctype html>${withBase}`
  }
  return `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<base target="_blank">
${mailFrameBaseStyle()}
</head>
<body>${sanitized}</body>
</html>`
}
function mailFrameBaseStyle() {
  return `<style>
  html, body { margin: 0; padding: 0; background: #fff; color: #111827; }
  body {
    box-sizing: border-box;
    overflow-wrap: anywhere;
    -webkit-text-size-adjust: 100%;
    font-family: Arial, "Helvetica Neue", Helvetica, sans-serif;
    font-size: 14px;
    line-height: 1.5;
  }
  *, *::before, *::after { box-sizing: border-box; }
  img { max-width: 100%; height: auto; }
  table { max-width: 100%; }
  pre { white-space: pre-wrap; word-break: break-word; font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
  a { color: #2563eb; }
</style>`
}
function sanitizeComposerHtml(value: string) {
  return DOMPurify.sanitize(value || "")
}
function htmlContainsMeaningfulContent(html: string) {
  return /<(img|hr|table|ul|ol|li|blockquote|pre|div)[\s>]/i.test(html) || stripHtml(html).trim().length > 0
}
function playIncomingMailSound(ref: React.MutableRefObject<AudioContext | null>) {
  const AudioContextCtor = window.AudioContext || (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext
  if (!AudioContextCtor) return
  const ctx = ref.current || new AudioContextCtor()
  ref.current = ctx
  if (ctx.state === "suspended") void ctx.resume()
  const now = ctx.currentTime
  const gain = ctx.createGain()
  gain.gain.setValueAtTime(0.0001, now)
  gain.gain.exponentialRampToValueAtTime(0.12, now + 0.02)
  gain.gain.exponentialRampToValueAtTime(0.0001, now + 0.45)
  gain.connect(ctx.destination)
  for (const [index, frequency] of [880, 1175].entries()) {
    const osc = ctx.createOscillator()
    const start = now + index * 0.14
    osc.type = "sine"
    osc.frequency.setValueAtTime(frequency, start)
    osc.connect(gain)
    osc.start(start)
    osc.stop(start + 0.18)
  }
}
function withPrefix(subject: string, prefix: string) { return subject.toLowerCase().startsWith(prefix.toLowerCase()) ? subject : `${prefix} ${subject}` }
function quoteMessage(message: MailMessage) {
  const body = message.bodyText || stripHtml(message.bodyHtml || message.snippet || "")
  const quote = body.split("\n").map((line) => `> ${line}`).join("\n")
  return `\n\n----- 原始邮件 -----\nFrom: ${senderTitle(message)}\nTo: ${message.to.join(", ")}\nDate: ${formatDateTime(message.receivedAt)}\nSubject: ${messageSubject(message)}\n\n${quote}`
}
function stripHtml(html: string) { const div = document.createElement("div"); div.innerHTML = DOMPurify.sanitize(html); return div.textContent || div.innerText || "" }
function attachmentLimitBytes(limits?: PermissionLimits) {
  const mb = limits?.maxAttachmentMb || 0
  return mb > 0 ? mb * 1024 * 1024 : 0
}
async function fileToAttachment(file: File) {
  const buffer = await file.arrayBuffer()
  let binary = ""
  const bytes = new Uint8Array(buffer)
  for (let i = 0; i < bytes.length; i++) binary += String.fromCharCode(bytes[i])
  return { filename: file.name, contentType: file.type || "application/octet-stream", contentBase64: btoa(binary) }
}
async function attachmentFilesFromMessage(message: MailMessage) {
  if (!message.attachments?.length) return []
  return Promise.all(message.attachments.map(async (attachment) => {
    const response = await fetch(`/api/mail/attachments/${attachment.id}`, { credentials: "include" })
    const blob = await response.blob()
    return new File([blob], attachment.filename, { type: attachment.contentType || blob.type || "application/octet-stream" })
  }))
}
