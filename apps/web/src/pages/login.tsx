import * as React from "react"
import { Navigate, useLocation, useNavigate } from "react-router-dom"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { HardDriveDownload, KeyRound, LoaderCircle, LockKeyhole, Mail, MonitorSmartphone, Moon, ShieldCheck, Sun, Tags } from "lucide-react"
import { api } from "@/lib/api"
import { useMe } from "@/hooks/use-me"
import { TurnstileBox } from "@/components/turnstile-box"
import { PasswordInput } from "@/components/ui/password-input"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { useToast } from "@/hooks/use-toast"
import { safeReturnPath } from "@/lib/navigation"
import { isDemoMode, publicAsset } from "@/lib/demo"
import { AuthError, AuthLoading } from "@/components/auth-states"
import { BrandMark } from "@/components/brand-mark"
import "./login.css"

export function LoginPage() {
  const me = useMe()
  const location = useLocation()
  const navigate = useNavigate()
  const qc = useQueryClient()
  const { toast } = useToast()
  const publicSettings = useQuery({ queryKey: ["public-settings"], queryFn: api.publicSettings, staleTime: 0, refetchOnMount: "always" })
  const [turnstileToken, setTurnstileToken] = React.useState("")
  const [challengeToken, setChallengeToken] = React.useState("")
  const [loginOpen, setLoginOpen] = React.useState(false)
  const [visualTheme, setVisualTheme] = React.useState<"day" | "night">(() => {
    const savedTheme = window.localStorage.getItem("newszxcn:landing-theme")
    if (savedTheme === "day" || savedTheme === "night") return savedTheme
    return window.matchMedia("(prefers-color-scheme: dark)").matches ? "night" : "day"
  })
  const [transitionPhase, setTransitionPhase] = React.useState<"idle" | "to-login" | "to-home" | "to-register">("idle")
  const transitionTimerRef = React.useRef<number | null>(null)
  const heroRef = React.useRef<HTMLElement>(null)
  const login = useMutation({
    mutationFn: (form: FormData) => challengeToken
      ? api.login({ challengeToken, twoFactorCode: String(form.get("twoFactorCode") || "") })
      : api.login({ email: String(form.get("email") || ""), password: String(form.get("password") || ""), turnstileToken }),
    onSuccess: async (data) => {
      if (data.twoFactorRequired && data.challengeToken) {
        setTransitionPhase("idle")
        setChallengeToken(data.challengeToken)
        setLoginOpen(true)
        toast({ title: "请输入双因素验证码" })
        return
      }
      await qc.invalidateQueries({ queryKey: ["me"] })
    },
    onError: (e) => toast({ title: "登录失败", description: e.message }),
  })
  const turnstileRequired = !!publicSettings.data?.turnstileEnabled
  const returnPath = safeReturnPath((location.state as { from?: unknown } | null)?.from)

  React.useEffect(() => () => {
    if (transitionTimerRef.current !== null) window.clearTimeout(transitionTimerRef.current)
  }, [])

  React.useEffect(() => {
    window.localStorage.setItem("newszxcn:landing-theme", visualTheme)
  }, [visualTheme])

  // Do not redirect with stale cached user data after the session has expired.
  // Otherwise the login page and AuthGuard can continuously redirect each other.
  if (me.isSuccess && me.data?.user) return <Navigate to={returnPath} replace />
  if (publicSettings.isLoading) return <AuthLoading />
  if (publicSettings.isError) return <AuthError message={publicSettings.error.message} onRetry={() => { void publicSettings.refetch() }} />

  const handlePointerMove = (event: React.PointerEvent<HTMLElement>) => {
    const x = (event.clientX / window.innerWidth - 0.5) * 2
    const y = (event.clientY / window.innerHeight - 0.5) * 2
    heroRef.current?.style.setProperty("--pointer-x", x.toFixed(3))
    heroRef.current?.style.setProperty("--pointer-y", y.toFixed(3))
  }

  const resetPointer = () => {
    heroRef.current?.style.setProperty("--pointer-x", "0")
    heroRef.current?.style.setProperty("--pointer-y", "0")
  }

  const startLoginTransition = () => {
    if (transitionPhase !== "idle") return
    window.scrollTo({ top: 0, behavior: "auto" })
    setTransitionPhase("to-login")
    transitionTimerRef.current = window.setTimeout(() => {
      setLoginOpen(true)
      setTransitionPhase("idle")
      transitionTimerRef.current = null
    }, 680)
  }

  const startHomeTransition = () => {
    if (transitionPhase !== "idle") return
    window.scrollTo({ top: 0, behavior: "auto" })
    setTransitionPhase("to-home")
    transitionTimerRef.current = window.setTimeout(() => {
      setLoginOpen(false)
      setChallengeToken("")
      setTransitionPhase("idle")
      transitionTimerRef.current = null
    }, 680)
  }

  const startRegisterTransition = () => {
    if (transitionPhase !== "idle" || !publicSettings.data?.openRegistration) return
    window.scrollTo({ top: 0, behavior: "auto" })
    setTransitionPhase("to-register")
    transitionTimerRef.current = window.setTimeout(() => {
      navigate("/register")
      transitionTimerRef.current = null
    }, 240)
  }

  const authActive = loginOpen || !!challengeToken
  const authVisualActive = authActive

  return (
    <main ref={heroRef} data-visual-theme={visualTheme} className={`space-hero relative min-h-svh overflow-x-hidden bg-[#020617] text-white ${transitionPhase !== "idle" ? "is-transitioning" : ""}`} onPointerMove={handlePointerMove} onPointerLeave={resetPointer}>
      <div className="space-backgrounds absolute inset-0" aria-hidden="true">
        <img src={publicAsset("space-anime-hero.jpg")} alt="" className="space-background space-background-night absolute inset-0 size-full object-cover" />
        <img src={publicAsset("space-anime-day.jpg")} alt="" className="space-background space-background-day absolute inset-0 size-full object-cover" />
      </div>
      <div className="space-overlay absolute inset-0" aria-hidden="true" />
      <Starfield />

      <div className="relative z-10 flex min-h-svh flex-col px-5 py-5 sm:px-9 sm:py-7 lg:px-14 xl:px-20">
        <header className="flex items-center justify-between gap-2 sm:gap-4">
          <div className="flex min-w-0 items-center gap-2 sm:gap-3">
            <BrandMark className="space-brand-mark size-9 shrink-0 sm:size-10" />
            <span className="space-serif truncate text-base font-medium text-white sm:text-xl">NewSzxcn 邮箱</span>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            <Button
              type="button"
              variant="outline"
              size="icon"
              className="space-theme-button size-9 rounded-full sm:size-10"
              aria-label={visualTheme === "night" ? "切换到日间模式" : "切换到夜间模式"}
              title={visualTheme === "night" ? "切换到日间模式" : "切换到夜间模式"}
              onClick={() => setVisualTheme((current) => current === "night" ? "day" : "night")}
            >
              {visualTheme === "night" ? <Sun aria-hidden="true" /> : <Moon aria-hidden="true" />}
            </Button>
            {!authVisualActive && publicSettings.data?.openRegistration && (
              <Button type="button" variant="ghost" className="space-register-button h-9 rounded-full px-3 sm:h-10 sm:px-5" onClick={startRegisterTransition} disabled={transitionPhase !== "idle"}>
                注册
              </Button>
            )}
            <Button
              variant="outline"
              className="space-login-button h-9 rounded-full px-3 text-white hover:text-white sm:h-10 sm:px-6"
              disabled={transitionPhase !== "idle"}
              onClick={authVisualActive ? startHomeTransition : startLoginTransition}
            >
              <span className="space-login-button-label">{authVisualActive ? "返回" : "登录"}</span>
            </Button>
          </div>
        </header>

        <section className="relative grid flex-1 items-center pb-10 pt-14 lg:pt-10 xl:grid-cols-[0.42fr_0.58fr] xl:pb-14">
          {authVisualActive ? (
            <LoginArtwork
              leaving={transitionPhase === "to-home" || transitionPhase === "to-register"}
              challengeToken={challengeToken}
              loginPending={login.isPending}
              openRegistration={!!publicSettings.data?.openRegistration}
              turnstileRequired={turnstileRequired}
              turnstileSiteKey={publicSettings.data?.turnstileSiteKey || ""}
              demoMode={isDemoMode}
              onTurnstileToken={setTurnstileToken}
              onBackToLogin={() => setChallengeToken("")}
              onRegister={startRegisterTransition}
              onSubmit={(form) => {
                if (!challengeToken && turnstileRequired && !turnstileToken) {
                  toast({ title: "请先完成人机验证" })
                  return
                }
                login.mutate(form)
              }}
            />
          ) : (
            <div className={`space-home-content w-full max-w-[720px] xl:pl-3 ${transitionPhase === "to-login" || transitionPhase === "to-register" ? "is-leaving" : ""}`}>
              <div className="hero-reveal hero-delay-1">
                <p className="space-kicker space-serif">属 于 你 的 通 信 空 间</p>
                <div className="space-kicker-divider" aria-hidden="true"><span /></div>
              </div>

              <div className="space-title-art mt-7">
                <span className="space-spark space-spark-one" aria-hidden="true" />
                <span className="space-spark space-spark-two" aria-hidden="true" />
                <span className="space-spark space-spark-three" aria-hidden="true" />
                <h1 className="space-editorial-title space-poetic-title hero-reveal hero-delay-2">星河很远，思念很近。</h1>
                <p className="space-brand-title space-poetic-subtitle hero-reveal hero-delay-3">每一封来信，都有属于它的归处。</p>
              </div>

              <p className="space-description space-serif hero-reveal hero-delay-4 mt-7">安全、私密、稳定的邮件服务，<br />连接你我，沟通无限。</p>

              <div className="hero-reveal hero-delay-5 mt-9 sm:mt-11">
                <div className="space-feature-heading mb-6">
                  <p className="space-serif">邮 箱 功 能 介 绍</p>
                  <span aria-hidden="true" />
                </div>
                <div className="grid grid-cols-2 gap-x-5 gap-y-5 sm:max-w-[580px] sm:gap-x-8 sm:gap-y-6">
                  <HeroFeature icon={<ShieldCheck />} title="私有部署" detail="邮件与配置由你掌控" />
                  <HeroFeature icon={<MonitorSmartphone />} title="多端同步" detail="Web · IMAP · SMTP" />
                  <HeroFeature icon={<Tags />} title="邮件归类" detail="文件夹与标签管理" />
                  <HeroFeature icon={<HardDriveDownload />} title="加密备份" detail="本地与云端安全备份" />
                </div>
              </div>
            </div>
          )}
        </section>
      </div>
    </main>
  )
}

function LoginArtwork({ challengeToken, leaving, loginPending, openRegistration, turnstileRequired, turnstileSiteKey, demoMode, onTurnstileToken, onBackToLogin, onRegister, onSubmit }: {
  challengeToken: string
  leaving: boolean
  loginPending: boolean
  openRegistration: boolean
  turnstileRequired: boolean
  turnstileSiteKey: string
  demoMode: boolean
  onTurnstileToken: (token: string) => void
  onBackToLogin: () => void
  onRegister: () => void
  onSubmit: (form: FormData) => void
}) {
  return (
    <div className={`space-auth-stage relative w-full max-w-[524px] ${leaving ? "is-leaving" : ""}`}>
      <div className="space-auth-rail" aria-hidden="true"><span /><span /><span /></div>
      <div className="space-auth-content">
        <div className="space-auth-kicker space-serif"><span />一 方 私 域，星 河 作 证<span /></div>
        <h1 className="space-auth-title space-serif">{challengeToken ? "验证身份" : "欢迎回来"}</h1>
        <p className="space-auth-subtitle space-serif">{challengeToken ? "完成安全验证" : "登录你的邮箱"}</p>

        <form className="space-login-form mt-9 space-y-5" onSubmit={(event) => { event.preventDefault(); onSubmit(new FormData(event.currentTarget)) }}>
          <div className={challengeToken ? "sr-only" : "space-y-2.5"} aria-hidden={challengeToken ? true : undefined}>
            <Label htmlFor="email" className="space-auth-label">{demoMode ? "演示账号" : "邮箱地址"}</Label>
            <div className="space-auth-field">
              <Mail className="space-auth-field-icon" aria-hidden="true" />
              <Input id="email" name="email" type={demoMode ? "text" : "email"} autoComplete="username" allowPasswordManager placeholder={demoMode ? "admin" : "请输入邮箱地址"} defaultValue={demoMode ? "admin" : ""} required={!challengeToken} autoFocus={!challengeToken} tabIndex={challengeToken ? -1 : undefined} className="space-auth-input" />
            </div>
          </div>
          {!challengeToken ? (
            <div className="space-y-2.5">
              <Label htmlFor="password" className="space-auth-label">密码</Label>
              <div className="space-auth-field">
                <LockKeyhole className="space-auth-field-icon" aria-hidden="true" />
                <PasswordInput id="password" name="password" autoComplete="current-password" allowPasswordManager placeholder={demoMode ? "admin" : "请输入密码"} defaultValue={demoMode ? "admin" : ""} required className="space-auth-input" />
              </div>
            </div>
          ) : (
            <div className="space-y-2.5">
              <Label htmlFor="twoFactorCode" className="space-auth-label">双因素验证码或恢复码</Label>
              <div className="space-auth-field">
                <KeyRound className="space-auth-field-icon" aria-hidden="true" />
                <Input
                  id="twoFactorCode"
                  name="twoFactorCode"
                  type="text"
                  autoComplete="one-time-code"
                  allowPasswordManager
                  autoCapitalize="off"
                  autoCorrect="off"
                  spellCheck={false}
                  enterKeyHint="done"
                  minLength={6}
                  required
                  autoFocus
                  className="space-auth-input text-center text-lg tracking-[0.25em]"
                />
              </div>
            </div>
          )}

          {!challengeToken && turnstileRequired && <TurnstileBox siteKey={turnstileSiteKey} onToken={onTurnstileToken} />}

          <Button className="space-auth-submit space-serif" disabled={loginPending}>
            {loginPending ? <LoaderCircle className="size-4 animate-spin" /> : null}
            {loginPending ? "正在登录..." : challengeToken ? "验证登录" : "登录"}
          </Button>

          {challengeToken ? (
            <Button type="button" variant="ghost" className="w-full text-indigo-100/70 hover:bg-white/5 hover:text-white" onClick={onBackToLogin}>返回账号登录</Button>
          ) : openRegistration ? (
            <div className="space-auth-register-hint flex items-center justify-center gap-1 text-sm">
              <span>没有账号？</span>
              <Button type="button" variant="link" className="space-auth-register-link h-auto px-1 text-sm" onClick={onRegister}>
                注册账号
              </Button>
            </div>
          ) : null}
        </form>
      </div>
    </div>
  )
}

function HeroFeature({ icon, title, detail }: { icon: React.ReactNode; title: string; detail: string }) {
  return (
    <div className="space-feature group flex min-w-0 items-center gap-3">
      <span className="space-feature-icon grid size-12 shrink-0 place-items-center text-indigo-200 [&>svg]:size-5" aria-hidden="true">{icon}</span>
      <span className="min-w-0">
        <strong className="space-serif block text-[15px] font-medium text-white">{title}</strong>
        <span className="mt-1 block text-xs leading-5 text-indigo-100/75">{detail}</span>
      </span>
    </div>
  )
}

function Starfield() {
  const canvasRef = React.useRef<HTMLCanvasElement>(null)

  React.useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas) return
    const context = canvas.getContext("2d")
    if (!context) return
    const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches
    let width = 0
    let height = 0
    let frame = 0
    let lastTime = performance.now()
    let nextMeteor = lastTime + 10000 + Math.random() * 10000
    let meteorStart = 0
    const stars = Array.from({ length: 14 }, () => ({
      x: Math.random(),
      y: Math.random() * 0.62,
      radius: 0.4 + Math.random() * 0.95,
      alpha: 0.12 + Math.random() * 0.24,
      drift: 0.0000008 + Math.random() * 0.0000014,
      phase: Math.random() * Math.PI * 2,
    }))

    const resize = () => {
      const ratio = Math.min(window.devicePixelRatio || 1, 2)
      width = window.innerWidth
      height = window.innerHeight
      canvas.width = Math.floor(width * ratio)
      canvas.height = Math.floor(height * ratio)
      canvas.style.width = `${width}px`
      canvas.style.height = `${height}px`
      context.setTransform(ratio, 0, 0, ratio, 0, 0)
    }

    const draw = (now: number) => {
      const delta = Math.min(now - lastTime, 32)
      lastTime = now
      context.clearRect(0, 0, width, height)
      for (const star of stars) {
        if (!reduceMotion) star.x = (star.x + star.drift * delta + 1) % 1
        const pulse = reduceMotion ? 1 : 0.82 + Math.sin(now * 0.00045 + star.phase) * 0.18
        context.beginPath()
        context.fillStyle = `rgba(205, 220, 255, ${star.alpha * pulse})`
        context.arc(star.x * width, star.y * height, star.radius, 0, Math.PI * 2)
        context.fill()
      }

      if (!reduceMotion && now >= nextMeteor && !meteorStart) meteorStart = now
      if (meteorStart) {
        const progress = (now - meteorStart) / 720
        if (progress <= 1) {
          const startX = width * 0.55
          const startY = height * 0.14
          const x = startX + width * 0.16 * progress
          const y = startY + height * 0.09 * progress
          const trail = 95
          const gradient = context.createLinearGradient(x - trail, y - trail * 0.55, x, y)
          gradient.addColorStop(0, "rgba(129, 92, 246, 0)")
          gradient.addColorStop(1, `rgba(226, 232, 255, ${Math.sin(progress * Math.PI) * 0.85})`)
          context.strokeStyle = gradient
          context.lineWidth = 1.5
          context.beginPath()
          context.moveTo(x - trail, y - trail * 0.55)
          context.lineTo(x, y)
          context.stroke()
        } else {
          meteorStart = 0
          nextMeteor = now + 10000 + Math.random() * 10000
        }
      }
      frame = window.requestAnimationFrame(draw)
    }

    resize()
    window.addEventListener("resize", resize)
    frame = window.requestAnimationFrame(draw)
    return () => {
      window.cancelAnimationFrame(frame)
      window.removeEventListener("resize", resize)
    }
  }, [])

  return <canvas ref={canvasRef} className="space-particles pointer-events-none absolute inset-0 z-[2]" aria-hidden="true" />
}
