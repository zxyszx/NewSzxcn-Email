import type {
  AdminOverview, AdminUser, Alias, Domain, ForwardingSettings, MailFolder, MailLabel,
  MailMessage, MailStats, Mailbox, PermissionKey, SystemSettings, User,
} from "./api-types"

const now = new Date().toISOString()
const permissions: PermissionKey[] = [
  "mail.access", "mail.messages.read", "mail.messages.send", "mail.messages.drafts", "mail.messages.schedule", "mail.messages.organize",
  "mail.labels.manage", "mail.attachments.download", "mail.contacts.manage", "mail.signatures.manage", "mail.rules.manage",
  "mail.blocked_senders.manage", "mail.stats.view", "mail.mailboxes.apply", "admin.overview.view", "admin.users.view",
  "admin.permission_groups.view", "admin.domains.view", "admin.dns.view", "admin.mailboxes.view", "admin.aliases.view",
  "admin.messages.view", "admin.messages.read", "admin.messages.attachments", "admin.settings.view", "admin.templates.view",
]
const limits = { maxAttachmentMb: 25, maxMailboxCount: 10, smtpDailyLimit: 500, smtpMinuteLimit: 20, imapMinuteLimit: 120, pop3MinuteLimit: 60 }
const user: User = { id: "demo-admin", loginName: "admin", email: "admin@demo.newszxcn.com", displayName: "演示管理员", role: "admin", disabled: false, protected: true, twoFactorEnabled: false, permissions, limits, permissionGroupIds: [], permissionGroups: [], createdAt: now }
const domain: Domain = { id: "demo-domain", name: "demo.newszxcn.com", status: "active", dkimSelector: "mail", dnsStatus: "ok", dnsCheckedAt: now, createdAt: now }
const mailboxes: Mailbox[] = [
  { id: "mailbox-1", userId: user.id, userEmail: user.email, domainId: domain.id, localPart: "admin", address: "admin@demo.newszxcn.com", displayName: "工作邮箱", quotaMb: 2048, status: "active", primary: true, unreadCount: 3, createdAt: now },
  { id: "mailbox-2", userId: user.id, userEmail: user.email, domainId: domain.id, localPart: "hello", address: "hello@demo.newszxcn.com", displayName: "公开联系", quotaMb: 1024, status: "active", unreadCount: 1, createdAt: now },
]
const folders: MailFolder[] = [
  ["inbox", "收件箱", "inbox", 3, 8], ["drafts", "草稿箱", "drafts", 0, 1], ["sent", "已发送", "sent", 0, 4], ["archive", "已归档", "archive", 0, 2], ["trash", "已删除", "trash", 0, 0], ["spam", "垃圾邮件", "spam", 0, 0],
].map(([id, name, role, unreadCount, totalCount], sortOrder) => ({ id: String(id), name: String(name), role: String(role), icon: "", sortOrder, unreadCount: Number(unreadCount), totalCount: Number(totalCount), uidValidity: 1, uidNext: Number(totalCount) + 1, highestModseq: Number(totalCount) }))
const labels: MailLabel[] = [
  { id: "label-1", mailboxId: mailboxes[0].id, name: "重要", color: "#ef4444", messageCount: 2, unreadCount: 1 },
  { id: "label-2", mailboxId: mailboxes[0].id, name: "工作", color: "#2563eb", messageCount: 4, unreadCount: 2 },
]
const messageSeeds = [
  ["NewSzxcn 团队", "欢迎使用 NewSzxcn 邮箱在线演示", "这是一个与真实服务器完全隔离的公开演示环境。"],
  ["系统通知", "域名 DNS 检测已通过", "demo.newszxcn.com 的邮件记录配置状态正常。"],
  ["项目协作", "本周邮件系统运行报告", "本周邮件投递稳定，队列与存储状态均正常。"],
  ["GitHub", "NewSzxcn-Email 项目动态", "仓库收到新的关注与反馈。"],
  ["客户支持", "关于邮箱转发设置的回复", "已验证邮箱可以绑定到多个内部邮箱账号。"],
  ["安全中心", "新的登录活动提醒", "检测到在线演示账号登录，本提示仅为模拟数据。"],
]
const messages: MailMessage[] = messageSeeds.map(([fromName, subject, snippet], index) => ({
  id: `message-${index + 1}`, mailboxId: mailboxes[index % 2].id, mailboxAddress: mailboxes[index % 2].address,
  ownerEmail: user.email, recipientAddress: mailboxes[index % 2].address, folderId: "inbox", folder: "inbox",
  messageUid: String(index + 1), imapUid: index + 1, imapModseq: index + 1, messageId: `<demo-${index + 1}@newszxcn.local>`,
  subject, from: `${fromName.toLowerCase().replace(/\s/g, "")}@example.com`, fromName, to: [mailboxes[index % 2].address], cc: [], bcc: [],
  sentAt: new Date(Date.now() - index * 7_200_000).toISOString(), receivedAt: new Date(Date.now() - index * 7_200_000).toISOString(),
  snippet, bodyText: `${snippet}\n\n在线预览中的邮件、邮箱和统计数据均为模拟内容。`, bodyHtml: `<p>${snippet}</p><p>在线预览中的邮件、邮箱和统计数据均为模拟内容。</p>`,
  isRead: index > 2, isStarred: index === 1, hasAttachments: false, sizeBytes: 3200 + index * 420, labels: index === 1 ? [labels[0]] : [],
}))
const overview: AdminOverview = { users: 3, activeUsers: 3, domains: 2, mailboxes: 6, activeMailboxes: 6, aliases: 4, messages: 128, unreadMessages: 9, storageBytes: 18_874_368, accountForwardingRules: 2, mailboxForwardingRules: 3, forwardedMailboxes: 3, todaySent: 16, todayReceived: 42, sendDelivered: 15, sendFailed: 1, queueMessages: 1 }
const systemSettings: SystemSettings = { publicHostname: "demo.newszxcn.com", publicBaseUrl: "https://zxyszx.github.io/NewSzxcn-Email/", smtpHost: "smtp.demo.local", smtpPort: "587", smtpUsername: "demo", smtpPasswordSet: true, smtpRequireTls: true, maildirRoot: "/var/mail/vhosts", maildirScanSeconds: 30, sessionTtlHours: 24, allowInsecureHttp: false, openRegistration: false, twoFactorEnabled: true, turnstileEnabled: false, turnstileSiteKey: "", turnstileSecretSet: false, catchAllEnabled: false, mailAutoRefresh: true, mailRefreshSeconds: 30, userMailboxApplyEnabled: true, userMailboxDomainIds: [domain.id], reservedMailboxPrefixes: "postmaster,abuse", externalImapEnabled: true, externalImapSecretSet: true, externalImapSyncSeconds: 300, externalImapAllowPrivateHosts: false, externalImapGmailClientId: "", externalImapGmailClientSecretSet: false, externalImapOutlookClientId: "", externalImapOutlookClientSecretSet: false, telegramMailEnabled: false, telegramBotTokenSet: false, telegramPrivateChatId: "", telegramBodyMode: "summary", telegramMailboxIds: [], telegramIncludeUnregistered: false }
const forwarding: ForwardingSettings = { verifiedEmails: [{ id: "verified-1", email: "archive@example.com", verified: true, verifiedAt: now, createdAt: now, deliveryStatus: "verified", mailboxBindings: [mailboxes[0].address] }], accountTargetEmail: "", accountTargetEmails: [], mailboxRules: [{ mailboxId: mailboxes[0].id, mailboxAddress: mailboxes[0].address, targetEmail: "archive@example.com", targetEmails: ["archive@example.com"] }], pendingBindings: [], mailboxSummaries: [{ mailboxId: mailboxes[0].id, mailboxAddress: mailboxes[0].address, independentTargets: 1, inheritedTargets: 0, enabled: true, targets: [{ email: "archive@example.com", verified: true, source: "mailbox" }] }] }

export class DemoApiError extends Error {
  constructor(message: string, readonly status: number) { super(message) }
}

function list<T>(items: T[]) { return { items } }
function authenticated() { return window.sessionStorage.getItem("newszxcn:demo-auth") === "1" }

export async function demoRequest<T>(path: string, init: RequestInit = {}): Promise<T> {
  const url = new URL(path, window.location.origin)
  const method = (init.method || "GET").toUpperCase()
  if (url.pathname === "/api/public/settings") return { openRegistration: false, turnstileEnabled: false, turnstileSiteKey: "", publicHostname: "demo.newszxcn.com", mailAutoRefresh: false, mailRefreshMs: 0, externalImapEnabled: true, mailboxDomains: [{ id: domain.id, name: domain.name }] } as T
  if (url.pathname === "/api/auth/login" && method === "POST") {
    const body = JSON.parse(String(init.body || "{}")) as { email?: string; loginName?: string; password?: string }
    if ((body.email || body.loginName) !== "admin" || body.password !== "admin") throw new DemoApiError("演示账号或密码错误", 401)
    window.sessionStorage.setItem("newszxcn:demo-auth", "1")
    return { user } as T
  }
  if (url.pathname === "/api/auth/logout") { window.sessionStorage.removeItem("newszxcn:demo-auth"); return { ok: true } as T }
  if (!authenticated()) throw new DemoApiError("请先登录在线演示", 401)
  if (method !== "GET") throw new DemoApiError("在线预览为只读模式，不会修改任何真实数据", 403)

  if (url.pathname === "/api/me") return { user } as T
  if (url.pathname === "/api/mail/mailboxes" || url.pathname === "/api/admin/mailboxes") return list(mailboxes) as T
  if (url.pathname === "/api/mail/folders") return list(folders) as T
  if (url.pathname === "/api/mail/labels") return list(labels) as T
  if (url.pathname === "/api/mail/messages" || url.pathname === "/api/mail/starred" || url.pathname === "/api/admin/messages") return list(url.pathname.endsWith("starred") ? messages.filter((item) => item.isStarred) : messages) as T
  if (/^\/api\/(?:mail|admin)\/messages\/[^/]+$/.test(url.pathname)) return (messages.find((item) => item.id === url.pathname.split("/").pop()) || messages[0]) as T
  if (url.pathname === "/api/me/stats") return { totalMessages: 128, totalIncoming: 94, totalOutgoing: 34, unreadMessages: 9, todayOutgoing: 16, draftMessages: 1, failedSends: 1, starredMessages: 6, attachmentCount: 18, attachmentBytes: 8_388_608, storageBytes: 18_874_368, quotaBytes: 2_147_483_648, quotaUsedPct: 0.88, averageMessageBytes: 147456, byFolder: folders.map((folder) => ({ folder: folder.name, role: folder.role, count: folder.totalCount, unread: folder.unreadCount, bytes: folder.totalCount * 120000 })), trend: Array.from({ length: 7 }, (_, index) => ({ date: new Date(Date.now() - (6 - index) * 86400000).toISOString().slice(0, 10), incoming: 8 + index * 2, outgoing: 3 + index })), distribution: [{ key: "incoming", label: "收件", count: 94 }, { key: "outgoing", label: "发件", count: 34 }], topContacts: [{ email: "team@example.com", count: 12 }, { email: "support@example.com", count: 8 }] } satisfies MailStats as T
  if (url.pathname === "/api/me/mailbox-apply-options") return { enabled: true, domains: [domain], reservedPrefixes: ["postmaster", "abuse"] } as T
  if (url.pathname === "/api/me/forwarding") return forwarding as T
  if (url.pathname === "/api/me/telegram") return { enabled: false, privateChatId: "", mailboxIds: [], botConfigured: false } as T
  if (url.pathname === "/api/admin/overview") return overview as T
  if (url.pathname === "/api/admin/users") return list([{ ...user, mailboxCount: 2, mailboxes: mailboxes.map((item) => item.address), storageQuotaMb: 4096 } satisfies AdminUser]) as T
  if (url.pathname === "/api/admin/domains") return list([domain]) as T
  if (url.pathname === "/api/admin/aliases") return list([{ id: "alias-1", domainId: domain.id, source: "contact@demo.newszxcn.com", destination: mailboxes[0].address, enabled: true, createdAt: now } satisfies Alias]) as T
  if (url.pathname === "/api/admin/settings") return systemSettings as T
  if (url.pathname === "/api/admin/system/version") return { currentVersion: "v1.2.72-demo", latestVersion: "v1.2.72", updateAvailable: false, updateEnabled: false } as T
  if (url.pathname === "/api/admin/permission-limits/defaults") return limits as T
  if (url.pathname === "/api/admin/permission-groups") return { items: [], catalog: [] } as T
  if (url.pathname === "/api/admin/maildir-sync/health") return { configured: true, enabled: true, root: "/var/mail/vhosts", scanSeconds: 30, workerStarted: true, running: false, recentErrors: [], summary: { filesScanned: 128, imported: 128, backfilled: 0, cleaned: 0, fileErrors: 0 } } as T
  if (url.pathname === "/api/admin/backups") return { enabled: true, telegramSet: false, telegramBotSet: false, telegramLimit: 50000000, items: [], schedule: { enabled: true, days: 7, passwordSet: true, serverIp: "", chatId: "", telegramMode: "system", telegramEnabled: false, googleDriveEnabled: false }, googleDrive: { clientId: "", clientSecretSet: false, connected: false, folderName: "NewSzxcn Email" }, transfers: [] } as T
  if (url.pathname === "/api/admin/mail-templates") return list([]) as T
  if (url.pathname.includes("/dns-records")) return { items: [] } as T
  if (url.pathname.startsWith("/api/mail/scheduled-sends") || url.pathname.startsWith("/api/mail/send-queue") || url.pathname === "/api/admin/send-audit") return list([]) as T
  if (url.pathname.startsWith("/api/mail/external-accounts") || url.pathname.startsWith("/api/me/external-imap-accounts")) return list([]) as T
  if (url.pathname === "/api/me/signatures/default") return { signature: null } as T
  if (/^\/api\/me\/(api-tokens|contacts|signatures|rules|blocked-senders)$/.test(url.pathname)) return list([]) as T
  throw new DemoApiError("此功能未包含在在线预览中", 404)
}
