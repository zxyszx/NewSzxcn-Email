import * as React from "react"
import { useMutation, useQuery } from "@tanstack/react-query"
import { CheckCircle2, Download, ExternalLink, Loader2, RefreshCcw, TriangleAlert } from "lucide-react"
import { api } from "@/lib/api"
import { cn, formatDate } from "@/lib/utils"
import { useMe } from "@/hooks/use-me"
import { useToast } from "@/hooks/use-toast"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog"

const frontendVersion = import.meta.env.VITE_APP_VERSION || "dev"

export function SystemVersionDialog({ mode = "sidebar", className }: { mode?: "sidebar" | "inline"; className?: string }) {
  const me = useMe()
  const { toast } = useToast()
  const [open, setOpen] = React.useState(false)
  const [updatePhase, setUpdatePhase] = React.useState<"idle" | "starting" | "restarting">("idle")
  const version = useQuery({
    queryKey: ["admin", "system-version"],
    queryFn: api.systemVersion,
    staleTime: 5 * 60_000,
    retry: 1,
  })
  const currentVersion = version.data?.currentVersion || frontendVersion
  const isSystemAdmin = me.data?.user.role === "admin"
  const update = useMutation({
    mutationFn: async () => {
      setUpdatePhase("starting")
      const targetVersion = version.data?.latestVersion
      let result: Awaited<ReturnType<typeof api.updateSystem>>
      try {
        result = await api.updateSystem()
      } catch (error) {
        if (!targetVersion || !isUpdateConnectionInterruption(error)) throw error
        result = {
          ok: true,
          currentVersion,
          targetVersion,
          message: "更新请求已发送，正在等待服务恢复",
        }
      }
      setUpdatePhase("restarting")
      await waitForUpdatedService(result.targetVersion)
      return result
    },
    onError: (error) => {
      setUpdatePhase("idle")
      toast({ title: "更新失败", description: error.message })
    },
  })

  const trigger = mode === "inline" ? (
    <Button type="button" variant="outline" className={cn("h-11 justify-start gap-2 px-4 text-base font-normal", className)}>
      <RefreshCcw className="h-5 w-5 text-primary" />
      {currentVersion}
      {version.data?.updateAvailable && <Badge className="ml-1">可更新</Badge>}
    </Button>
  ) : (
    <Button
      type="button"
      variant={version.data?.updateAvailable ? "secondary" : "ghost"}
      className={cn("h-8 w-fit max-w-full justify-start gap-2 rounded-md px-2 text-xs font-medium group-data-[collapsible=icon]:hidden", className)}
      aria-label={`系统版本 ${currentVersion}`}
    >
      <span className="truncate">{currentVersion}</span>
      <span className={cn("h-2 w-2 shrink-0 rounded-full", version.data?.updateAvailable ? "bg-amber-500" : "bg-emerald-500")} aria-hidden="true" />
    </Button>
  )

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>{trigger}</DialogTrigger>
      <DialogContent className="max-h-[88svh] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <div className="flex items-center justify-between gap-3 pr-7">
            <DialogTitle>系统版本</DialogTitle>
            <Button type="button" variant="ghost" size="icon" className="h-8 w-8" onClick={() => version.refetch()} disabled={version.isFetching || update.isPending} aria-label="重新检查更新" title="重新检查更新">
              <RefreshCcw className={cn("h-4 w-4", version.isFetching && "animate-spin")} />
            </Button>
          </div>
        </DialogHeader>

        <div className="space-y-3">
          <div className="border-b pb-3 text-center">
            <div className="text-xs text-muted-foreground">当前版本</div>
            <div className="mt-1 text-3xl font-semibold tabular-nums">{currentVersion}</div>
            {version.data?.latestVersion && <div className="mt-1 text-xs text-muted-foreground">最新版本：{version.data.latestVersion}</div>}
          </div>

          {version.isLoading && <VersionState icon={<Loader2 className="animate-spin" />} title="正在检查更新" description="正在连接 GitHub Release。" />}
          {version.data?.checkError && <VersionState icon={<TriangleAlert />} title="暂时无法检查更新" description={version.data.checkError} tone="warning" />}
          {version.data && !version.data.checkError && !version.data.updateAvailable && <VersionState icon={<CheckCircle2 />} title="已是最新版本" description="当前无需更新。" tone="success" />}
          {version.data?.updateAvailable && (
            <VersionState
              icon={<Download />}
              title="发现新版本"
              description={`${version.data.latestVersion} 已发布${version.data.publishedAt ? ` · ${formatDate(version.data.publishedAt)}` : ""}`}
              tone="warning"
            />
          )}

          {version.data?.releaseNotes && (
            <div className="space-y-2">
              <div className="text-sm font-medium">更新日志</div>
              <div className="h-[clamp(12rem,30svh,18rem)] overflow-y-auto whitespace-pre-wrap rounded-md border bg-muted/20 p-4 text-sm leading-6 text-foreground/80">
                {version.data.releaseNotes}
              </div>
            </div>
          )}

          {update.isPending && (
            <div className="rounded-md border bg-muted/30 p-4">
              <div className="flex items-center gap-3 font-medium">
                <Loader2 className="h-5 w-5 animate-spin" />
                {updatePhase === "starting" ? "正在准备更新" : "正在重启服务"}
              </div>
              <div className="mt-2 text-sm text-muted-foreground">请保持页面打开，服务恢复后会自动刷新。</div>
            </div>
          )}

          {version.data?.updateAvailable && !version.data.updateEnabled && (
            <div className="rounded-md border p-3 text-sm text-muted-foreground">
              当前部署未启用页面更新，请在服务器执行 <code className="rounded bg-muted px-1.5 py-0.5 text-foreground">sudo newszxcn-email update</code>。
            </div>
          )}
        </div>

        <DialogFooter className="gap-2 sm:justify-between">
          <div>
            {version.data?.releaseUrl && (
              <Button type="button" variant="ghost" asChild>
                <a href={version.data.releaseUrl} target="_blank" rel="noreferrer">
                  更新详情<ExternalLink className="h-4 w-4" />
                </a>
              </Button>
            )}
          </div>
          {version.data?.updateAvailable && version.data.updateEnabled && (
            <Button type="button" disabled={!isSystemAdmin || update.isPending} onClick={() => update.mutate()}>
              {update.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Download className="h-4 w-4" />}
              {isSystemAdmin ? "立即更新" : "仅超级管理员可更新"}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function VersionState({ icon, title, description, tone = "neutral" }: { icon: React.ReactNode; title: string; description: string; tone?: "neutral" | "success" | "warning" }) {
  return (
    <div className={cn(
      "flex items-start gap-3 rounded-md border p-4",
      tone === "success" && "border-emerald-200 bg-emerald-50 text-emerald-900 dark:border-emerald-900 dark:bg-emerald-950/30 dark:text-emerald-100",
      tone === "warning" && "border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-100",
    )}>
      <span className="mt-0.5 [&>svg]:h-5 [&>svg]:w-5">{icon}</span>
      <span className="min-w-0">
        <span className="block font-medium">{title}</span>
        <span className="mt-1 block text-sm opacity-75">{description}</span>
      </span>
    </div>
  )
}

async function waitForUpdatedService(targetVersion: string) {
  const deadline = Date.now() + 8 * 60_000
  while (Date.now() < deadline) {
    await delay(3000)
    try {
      const health = await fetch(`/healthz?update=${Date.now()}`, { cache: "no-store" })
      if (!health.ok) {
        continue
      }
      const response = await fetch(`/api/admin/system/version?update=${Date.now()}`, { credentials: "include", cache: "no-store" })
      if (!response.ok) continue
      const body = await response.json() as { currentVersion?: string }
      if (body.currentVersion === targetVersion) {
        window.location.reload()
        return
      }
    } catch {}
  }
  throw new Error("更新等待超时，请稍后手动刷新页面检查服务状态")
}

function delay(ms: number) {
  return new Promise((resolve) => window.setTimeout(resolve, ms))
}

function isUpdateConnectionInterruption(error: unknown) {
  if (!(error instanceof Error)) return false
  return /(?:502|503|504|网络请求失败|请求超时|failed to fetch|networkerror)/i.test(error.message)
}
