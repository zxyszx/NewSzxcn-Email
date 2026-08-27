import { useQuery } from "@tanstack/react-query"
import { Link, useSearchParams } from "react-router-dom"
import { ArrowClockwise20Regular, CheckmarkCircle20Regular, MailArrowForward20Regular, Warning20Regular } from "@fluentui/react-icons"
import { api } from "@/lib/api"
import { BrandMark } from "@/components/brand-mark"
import { Button } from "@/components/ui/button"

export function ForwardingConfirmPage() {
  const [searchParams] = useSearchParams()
  const token = searchParams.get("token")?.trim() || ""
  const confirmation = useQuery({
    queryKey: ["forwarding-verification-confirm", token],
    queryFn: () => api.confirmForwardingEmail(token),
    enabled: !!token,
    retry: false,
  })
  const result = token ? confirmation.data : { ok: false, email: "", message: "验证链接无效", activations: [], status: 400 }
  const pending = !!token && confirmation.isLoading
  const failed = !pending && (confirmation.isError || !result?.ok)

  return (
    <main className="forwarding-confirm-page">
      <section className="forwarding-confirm-card" aria-live="polite">
        <header className="forwarding-confirm-brand"><BrandMark /><span><strong>NewSzxcn Email</strong><small>邮件转发安全验证</small></span></header>
        <span className={`forwarding-confirm-icon ${pending ? "is-pending" : failed ? "is-error" : "is-success"}`} aria-hidden="true">
          {pending ? <ArrowClockwise20Regular className="animate-spin" /> : failed ? <Warning20Regular /> : <CheckmarkCircle20Regular />}
        </span>
        <h1>{pending ? "正在验证邮箱" : failed ? "验证失败" : "邮箱验证成功"}</h1>
        <p>{pending ? "正在验证邮箱，请稍候。" : confirmation.isError ? "网络请求失败，请稍后重试。" : result?.message}</p>
        {!!result?.email && <div className="forwarding-confirm-email"><MailArrowForward20Regular /><span>{result.email}</span></div>}
        {!!result?.activations?.length && <div className="forwarding-confirm-relations"><strong>邮件转发已自动启用</strong>{result.activations.map((item) => <div key={`${item.scope}-${item.sourceEmail}-${item.targetEmail}`}><span>{item.sourceEmail}</span><MailArrowForward20Regular /><span>{item.targetEmail}</span></div>)}</div>}
        <div className="forwarding-confirm-actions">
          {failed && token && <Button type="button" variant="outline" onClick={() => void confirmation.refetch()}><ArrowClockwise20Regular />重新验证</Button>}
          <Button asChild><Link to="/mail/forwarding/verification">返回邮件转发</Link></Button>
        </div>
      </section>
    </main>
  )
}
