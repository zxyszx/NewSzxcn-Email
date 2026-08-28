import * as React from "react"
import { Outlet, Link, useLocation } from "react-router-dom"
import { ArchiveRestore, ClipboardList, Forward, Globe2, Inbox, LayoutDashboard, LogOut, Mailbox, Moon, Settings, ShieldCheck, Sun, UserCog } from "lucide-react"
import { useMe } from "@/hooks/use-me"
import { useLogout } from "@/hooks/use-logout"
import { AuthGuard } from "@/components/auth-guard"
import { Button } from "@/components/ui/button"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { SystemVersionDialog } from "@/components/system-version-dialog"
import { BrandMark } from "@/components/brand-mark"
import { LanguageSwitcher } from "@/components/language-switcher"
import { hasAnyPermission } from "@/lib/permissions"
import type { PermissionKey } from "@/lib/api-types"
import { applyTheme, getInitialTheme } from "@/lib/theme"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarRail,
  SidebarTrigger,
  useSidebar,
} from "@/components/ui/sidebar"

const adminSections: { key: string; label: string; icon: React.ReactNode; permissions: PermissionKey[] }[] = [
  { key: "overview", label: "仪表盘", icon: <LayoutDashboard />, permissions: ["admin.overview.view"] },
  { key: "users", label: "账号管理", icon: <UserCog />, permissions: ["admin.users.view"] },
  { key: "permissionGroups", label: "权限配置", icon: <ShieldCheck />, permissions: ["admin.permission_groups.view"] },
  { key: "domains", label: "域名管理", icon: <Globe2 />, permissions: ["admin.domains.view", "admin.dns.view"] },
  { key: "mailboxes", label: "邮箱管理", icon: <Mailbox />, permissions: ["admin.mailboxes.view"] },
  { key: "aliases", label: "邮件转发", icon: <Forward />, permissions: ["admin.aliases.view"] },
  { key: "messages", label: "全部邮件", icon: <Inbox />, permissions: ["admin.messages.view"] },
  { key: "sendAudit", label: "发送队列", icon: <ClipboardList />, permissions: ["admin.messages.view"] },
  { key: "backups", label: "备份与恢复", icon: <ArchiveRestore />, permissions: ["admin.settings.view"] },
  { key: "settings", label: "系统设置", icon: <Settings />, permissions: ["admin.settings.view", "admin.templates.view"] },
]

export function ProtectedLayout() {
  return (
    <AuthGuard>
      <ProtectedContent />
    </AuthGuard>
  )
}

function ProtectedContent() {
  const me = useMe()
  const location = useLocation()
  const logout = useLogout()
  const [darkMode, setDarkMode] = React.useState(getInitialTheme)
  const themeMountedRef = React.useRef(false)
  const user = me.data!.user
  const isMailRoute = location.pathname === "/" || location.pathname.startsWith("/mail")
  const isProfileRoute = location.pathname.startsWith("/profile")
  const isAdminRoute = location.pathname.startsWith("/admin")

  React.useEffect(() => {
    applyTheme(darkMode, themeMountedRef.current)
    themeMountedRef.current = true
  }, [darkMode])
  React.useEffect(() => {
    if (!isAdminRoute) return
    document.documentElement.classList.add("workspace-ui")
    return () => document.documentElement.classList.remove("workspace-ui")
  }, [isAdminRoute])

  const adminSection = new URLSearchParams(location.search).get("section") || "overview"
  const visibleAdminSections = adminSections.filter((item) => hasAnyPermission(user, item.permissions) && (item.key !== "backups" || user.role === "admin"))

  if (isMailRoute || isProfileRoute) {
    return <Outlet />
  }

  return (
    <SidebarProvider className="admin-workspace-theme">
      <Sidebar collapsible="icon">
        <SidebarHeader className="border-b">
          <div className="flex items-start gap-1">
            <SidebarMenu className="min-w-0 flex-1">
              <SidebarMenuItem>
                <SidebarMenuButton size="lg" asChild>
                  <Link to="/">
                    <BrandMark className="size-8" />
                    <div className="grid flex-1 text-left text-sm leading-tight">
                      <span className="truncate font-semibold">NewSzxcn 邮箱</span>
                    </div>
                  </Link>
                </SidebarMenuButton>
              </SidebarMenuItem>
            </SidebarMenu>
            <LanguageSwitcher className="mt-1 size-8 rounded-full group-data-[collapsible=icon]:hidden" variant="outline" />
            <Button
              type="button"
              variant="outline"
              size="icon"
              className="workspace-theme-toggle mt-1 size-8 shrink-0 rounded-full group-data-[collapsible=icon]:hidden"
              onClick={() => setDarkMode((value) => !value)}
              aria-label={darkMode ? "切换日间模式" : "切换夜间模式"}
              title={darkMode ? "日间模式" : "夜间模式"}
            >
              {darkMode ? <Sun className="h-4 w-4 text-amber-300" /> : <Moon className="h-4 w-4" />}
            </Button>
          </div>
          {isAdminRoute && <SystemVersionDialog className="ml-10" />}
        </SidebarHeader>
        <SidebarContent>
          {isAdminRoute && visibleAdminSections.length > 0 && (
            <SidebarGroup>
              <SidebarGroupContent>
                <SidebarMenu>
                  <AdminSectionItems activeSection={adminSection} sections={visibleAdminSections} />
                </SidebarMenu>
              </SidebarGroupContent>
            </SidebarGroup>
          )}
        </SidebarContent>
        <SidebarFooter>
          <SidebarMenu>
            <SidebarMenuItem>
              <SidebarMenuButton size="lg" className="group-data-[collapsible=icon]:!p-0" asChild>
                <Link to="/profile">
                  <Avatar className="h-8 w-8 rounded-lg">
                    <AvatarFallback className="rounded-lg bg-muted text-foreground">
                      {user.displayName.slice(0, 1).toUpperCase()}
                    </AvatarFallback>
                  </Avatar>
                  <div className="grid flex-1 text-left text-sm leading-tight">
                    <span className="truncate font-semibold">{user.displayName}</span>
                    <span className="truncate text-xs text-muted-foreground">{user.email}</span>
                  </div>
                </Link>
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
          <div className="p-2">
            <Button variant="outline" size="sm" className="w-full gap-2 border-destructive/35 text-xs text-destructive shadow-none hover:border-destructive/55 hover:bg-destructive/10 hover:text-destructive dark:border-destructive/45 dark:hover:bg-destructive/15" onClick={logout}>
              <LogOut className="h-3.5 w-3.5" />退出登录
            </Button>
          </div>
        </SidebarFooter>
        <SidebarRail />
      </Sidebar>
      <SidebarInset>
        <div className="admin-workspace-surface flex min-h-svh flex-col bg-muted/20">
          <div className="flex h-12 items-center gap-3 border-b bg-background px-3 md:hidden">
            <SidebarTrigger aria-label="打开导航" />
            <div className="min-w-0 flex-1 truncate text-sm font-semibold">
              {isAdminRoute ? visibleAdminSections.find((item) => item.key === adminSection)?.label || "系统管理" : "NewSzxcn 邮箱"}
            </div>
            <LanguageSwitcher className="size-8" variant="outline" />
          </div>
          <Outlet />
        </div>
      </SidebarInset>
    </SidebarProvider>
  )
}

function AdminSectionItems({ activeSection, sections }: { activeSection: string; sections: typeof adminSections }) {
  const { isMobile, setOpenMobile } = useSidebar()

  function closeMobile() {
    if (isMobile) setOpenMobile(false)
  }
  return sections.map((item) => (
    <SidebarMenuItem key={item.key}>
      <SidebarMenuButton asChild isActive={activeSection === item.key} tooltip={item.label}>
        <Link to={`/admin?section=${item.key}`} onClick={closeMobile}>
          {item.icon}
          <span>{item.label}</span>
        </Link>
      </SidebarMenuButton>
    </SidebarMenuItem>
  ))
}
