import * as React from "react"
import { Link, Navigate, useNavigate } from "react-router-dom"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { ArrowLeft, ArrowRight, LoaderCircle, LockKeyhole, Mail, Moon, Sun, UserPlus } from "lucide-react"
import { api } from "@/lib/api"
import type { PublicDomain } from "@/lib/api"
import { useMe } from "@/hooks/use-me"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { useToast } from "@/hooks/use-toast"
import { PasswordInput } from "@/components/ui/password-input"
import { TurnstileBox } from "@/components/turnstile-box"
import { validatePasswordConfirm } from "@/lib/validation"
import { AuthError, AuthLoading } from "@/components/auth-states"
import { BrandMark } from "@/components/brand-mark"
import "./login.css"
import { publicAsset } from "@/lib/demo"

export function RegisterPage() {
  const me = useMe()
  const qc = useQueryClient()
  const navigate = useNavigate()
  const { toast } = useToast()
  const publicSettings = useQuery({ queryKey: ["public-settings"], queryFn: api.publicSettings, staleTime: 0, refetchOnMount: "always" })
  const [turnstileToken, setTurnstileToken] = React.useState("")
  const [domainId, setDomainId] = React.useState("")
  const [visualTheme, setVisualTheme] = React.useState<"day" | "night">(() => {
    const savedTheme = window.localStorage.getItem("newszxcn:landing-theme")
    if (savedTheme === "day" || savedTheme === "night") return savedTheme
    return window.matchMedia("(prefers-color-scheme: dark)").matches ? "night" : "day"
  })
  const domains: PublicDomain[] = publicSettings.data?.mailboxDomains || []
  const selectedDomain = domains.find((d) => d.id === domainId)
  React.useEffect(() => {
    if (!domainId && domains[0]) setDomainId(domains[0].id)
    if (domainId && !domains.some((domain) => domain.id === domainId)) setDomainId(domains[0]?.id || "")
  }, [domainId, domains])

  const register = useMutation({
    mutationFn: (form: FormData) => {
      const password = String(form.get("password") || "")
      const confirmPassword = String(form.get("confirmPassword") || "")
      validatePasswordConfirm(password, confirmPassword)
      const displayName = String(form.get("displayName") || "").trim()
      if (!displayName) throw new Error("请输入显示名称")
      const localPart = String(form.get("localPart") || "").trim()
      if (!localPart) throw new Error("请输入邮箱前缀")
      if (!domainId || !selectedDomain) throw new Error("请选择邮箱域名")
      return api.register({
        email: `${localPart}@${selectedDomain.name}`,
        displayName,
        password,
        turnstileToken,
        domainId,
        localPart,
      })
    },
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["me"] })
      toast({ title: "注册成功" })
      navigate("/profile", { replace: true })
    },
    onError: (e) => toast({ title: "注册失败", description: e.message }),
  })
  const turnstileRequired = !!publicSettings.data?.turnstileEnabled
  React.useEffect(() => {
    window.localStorage.setItem("newszxcn:landing-theme", visualTheme)
  }, [visualTheme])

  const handlePointerMove = (event: React.PointerEvent<HTMLElement>) => {
    const x = (event.clientX / window.innerWidth - 0.5) * 2
    const y = (event.clientY / window.innerHeight - 0.5) * 2
    event.currentTarget.style.setProperty("--pointer-x", x.toFixed(3))
    event.currentTarget.style.setProperty("--pointer-y", y.toFixed(3))
  }

  if (me.data?.user) return <Navigate to="/" replace />
  if (publicSettings.isLoading) return <AuthLoading />
  if (publicSettings.isError) return <AuthError message={publicSettings.error.message} onRetry={() => { void publicSettings.refetch() }} />
  return (
    <main data-visual-theme={visualTheme} className="space-hero relative min-h-svh overflow-x-hidden bg-[#020617] text-white" onPointerMove={handlePointerMove}>
      <div className="space-backgrounds absolute inset-0" aria-hidden="true">
        <img src={publicAsset("space-anime-hero.jpg")} alt="" className="space-background space-background-night absolute inset-0 size-full object-cover" />
        <img src={publicAsset("space-anime-day.jpg")} alt="" className="space-background space-background-day absolute inset-0 size-full object-cover" />
      </div>
      <div className="space-overlay absolute inset-0" aria-hidden="true" />
      <div className="relative z-10 flex min-h-svh flex-col px-5 py-5 sm:px-9 sm:py-7 lg:px-14 xl:px-20">
        <header className="flex items-center justify-between gap-4">
          <Link to="/login" className="flex min-w-0 items-center gap-3" aria-label="返回登录">
            <BrandMark className="space-brand-mark size-10" />
            <span className="space-serif truncate text-xl font-medium text-white">NewSzxcn 邮箱</span>
          </Link>
          <div className="flex shrink-0 items-center gap-2">
            <Button type="button" variant="outline" size="icon" className="space-theme-button size-10 rounded-full" aria-label={visualTheme === "night" ? "切换到日间模式" : "切换到夜间模式"} title={visualTheme === "night" ? "切换到日间模式" : "切换到夜间模式"} onClick={() => setVisualTheme((current) => current === "night" ? "day" : "night")}>
              {visualTheme === "night" ? <Sun aria-hidden="true" /> : <Moon aria-hidden="true" />}
            </Button>
            <Button asChild variant="outline" className="space-login-button h-10 rounded-full px-5 text-white hover:text-white sm:px-6">
              <Link to="/login"><ArrowLeft className="size-4" />返回登录</Link>
            </Button>
          </div>
        </header>

        <section className="relative grid flex-1 items-center pb-8 pt-8 lg:pt-4">
          <div className="space-auth-stage relative w-full max-w-[560px]">
            <div className="space-auth-rail" aria-hidden="true"><span /><span /><span /></div>
            <div className="space-auth-content">
              <div className="space-auth-kicker space-serif"><span />一 方 私 域，星 河 作 证<span /></div>
              <h1 className="space-auth-title space-serif">创建你的邮箱</h1>
              <p className="space-auth-subtitle space-serif">注册一个属于你的通信空间</p>
              {publicSettings.isSuccess && !publicSettings.data.openRegistration ? (
                <div className="mt-8 max-w-[440px] space-y-5">
                  <div className="space-auth-notice">当前未开放注册</div>
                  <Button type="button" variant="outline" className="space-login-button h-12 w-full rounded-xl text-white hover:text-white" asChild>
                    <Link to="/login"><ArrowLeft className="size-4" />返回登录</Link>
                  </Button>
                </div>
              ) : (
                <form className="space-login-form mt-7 max-w-[560px] space-y-4" onSubmit={(e) => { e.preventDefault(); if (turnstileRequired && !turnstileToken) { toast({ title: "请先完成人机验证" }); return }; register.mutate(new FormData(e.currentTarget)) }}>
                  {domains.length > 0 ? (
                    <div className="space-y-2.5">
                      <Label htmlFor="localPart" className="space-auth-label">邮箱地址</Label>
                      <div className="grid grid-cols-1 gap-2 sm:grid-cols-[minmax(0,1fr)_168px]">
                        <div className="space-auth-field"><Mail className="space-auth-field-icon" aria-hidden="true" /><Input id="localPart" name="localPart" className="space-auth-input" placeholder="邮箱前缀" required /></div>
                        <Select value={domainId} onValueChange={setDomainId} required>
                          <SelectTrigger className="space-auth-select"><SelectValue placeholder="选择域名" /></SelectTrigger>
                          <SelectContent>{domains.map((d) => <SelectItem key={d.id} value={d.id}>{d.name}</SelectItem>)}</SelectContent>
                        </Select>
                      </div>
                    </div>
                  ) : (
                    <div className="space-auth-notice">当前没有可注册的邮箱域名</div>
                  )}
                  <div className="space-y-2.5"><Label htmlFor="displayName" className="space-auth-label">显示名称</Label><div className="space-auth-field"><UserPlus className="space-auth-field-icon" aria-hidden="true" /><Input id="displayName" name="displayName" autoComplete="name" required className="space-auth-input" placeholder="请输入显示名称" /></div></div>
                  <div className="space-y-2.5"><Label htmlFor="password" className="space-auth-label">密码</Label><div className="space-auth-field"><LockKeyhole className="space-auth-field-icon" aria-hidden="true" /><PasswordInput id="password" name="password" autoComplete="new-password" minLength={6} required className="space-auth-input" placeholder="请输入密码" /></div></div>
                  <div className="space-y-2.5"><Label htmlFor="confirmPassword" className="space-auth-label">确认密码</Label><div className="space-auth-field"><LockKeyhole className="space-auth-field-icon" aria-hidden="true" /><PasswordInput id="confirmPassword" name="confirmPassword" autoComplete="new-password" minLength={6} required className="space-auth-input" placeholder="请再次输入密码" /></div></div>
                  {turnstileRequired && <TurnstileBox siteKey={publicSettings.data?.turnstileSiteKey || ""} onToken={setTurnstileToken} />}
                  <Button className="space-auth-submit space-serif" disabled={register.isPending || domains.length === 0}>{register.isPending ? <LoaderCircle className="size-4 animate-spin" /> : <ArrowRight className="size-4" />}{register.isPending ? "注册中..." : "注册"}</Button>
                  <div className="flex items-center justify-center gap-2 text-sm text-indigo-100/65"><span>已有账号？</span><Button type="button" variant="link" className="h-auto px-1 text-sm text-indigo-200 hover:text-white" asChild><Link to="/login">返回登录</Link></Button></div>
                </form>
              )}
            </div>
          </div>
        </section>
      </div>
    </main>
  )
}
