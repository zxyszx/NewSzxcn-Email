import * as React from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { ChevronDown, Loader2, Plus, Send, Trash2 } from "lucide-react"
import { api, type SmtpPoolSettings, type SmtpPoolAccount } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Checkbox } from "@/components/ui/checkbox"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"

const empty: SmtpPoolSettings = {source:"inherit",enabled:false,allProviders:false,providers:[],domains:[],strategy:"rotate",accountId:"",accounts:[]}
export function SmtpPoolSettingsPanel({admin=false,disabled=false}:{admin?:boolean;disabled?:boolean}) {
 const qc=useQueryClient()
 const query=useQuery({queryKey:["smtp-pool",admin?"admin":"mine"],queryFn:()=>api.smtpPool(admin)})
 const [draft,setDraft]=React.useState<SmtpPoolSettings>(empty)
 const [domains,setDomains]=React.useState("")
 const [saved,setSaved]=React.useState(false)
 React.useEffect(()=>{if(query.data){setDraft({...empty,...query.data.settings});setDomains((query.data.settings.domains||[]).join(", "))}},[query.data])
 const save=useMutation({mutationFn:()=>api.saveSmtpPool({...draft,domains:domains.split(/[,，\s]+/).map(x=>x.trim()).filter(Boolean)},admin),onSuccess:(data)=>{qc.setQueryData(["smtp-pool",admin?"admin":"mine"],data);setSaved(true)}})
 const update=(patch:Partial<SmtpPoolSettings>)=>{setSaved(false);setDraft(d=>({...d,...patch}))}
 const account=(index:number,patch:Partial<SmtpPoolAccount>)=>{setSaved(false);setDraft(d=>({...d,accounts:d.accounts.map((a,i)=>i===index?{...a,...patch}:a)}))}
 const personal=admin||draft.source==="personal"
 const id=admin?"shared-smtp":"personal-smtp"
 const providerName=(key:string)=>query.data?.providers.find(p=>p.id===key)?.name||key
 return <section aria-label={admin?"后台外部账号池":"外部 IMAP 使用配置"} className="mb-4 space-y-4 rounded-xl border bg-card p-4 sm:p-5">
  <div className="space-y-2"><h2 className="flex items-center gap-2 text-base font-semibold"><Send className="size-4"/>{admin?"后台外部账号池与发信分流":"外部 IMAP 使用配置"}</h2><p className="text-sm leading-6 text-muted-foreground">{admin?"授权用户可默认使用这个共享发信账号池。账号密码仅保存在服务器，不会提供给用户。":"默认沿用后台的账号池和分流规则；切换个人配置后，仅使用你自己添加的账号和规则。"}</p></div>
  {query.isLoading?<p role="status">正在读取配置…</p>:query.isError?<div role="alert" className="space-y-2"><p className="text-sm text-destructive">读取配置失败</p><Button type="button" variant="outline" onClick={()=>void query.refetch()}>重试</Button></div>:<fieldset disabled={disabled||save.isPending} className="min-w-0 space-y-5">
   {!admin&&<div className="space-y-3"><Label htmlFor={`${id}-source`}>使用哪一套配置</Label><Select value={draft.source} onValueChange={v=>update({source:v as SmtpPoolSettings["source"]})}><SelectTrigger id={`${id}-source`}><SelectValue/></SelectTrigger><SelectContent><SelectItem value="inherit">使用后台配置</SelectItem><SelectItem value="personal">使用个人配置</SelectItem></SelectContent></Select>
   {!personal&&<div className="space-y-2 rounded-lg bg-muted/40 p-3 text-sm leading-6"><p>后台发信通道：{query.data?.shared?.enabled?"已开启":"未开启"} · {query.data?.shared?.accountCount||0} 个可用账号</p><p>收件厂商：{query.data?.shared?.allProviders?"全部收件厂商":(query.data?.shared?.providers||[]).map(providerName).join("、")||"尚未选择"}</p>{!!query.data?.shared?.domains?.length&&<p className="break-all">另含指定域名：{query.data.shared.domains.join("、")}</p>}<p>未命中的收件人继续由原服务器发送。后台配置变化后自动生效。</p></div>}</div>}
   {personal&&<>
    <div className="flex min-h-11 items-center justify-between gap-3"><Label htmlFor={`${id}-enabled`}>开启使用外部账号发信</Label><Switch id={`${id}-enabled`} checked={draft.enabled} onCheckedChange={enabled=>update({enabled})}/></div>
    <div className="space-y-3"><div className="flex flex-wrap items-center justify-between gap-2"><h3 className="text-sm font-semibold">绑定的外部账号（{draft.accounts.length}）</h3><Button type="button" size="sm" variant="outline" onClick={()=>update({accounts:[...draft.accounts,{id:"",name:"",host:"",port:465,username:"",from:"",enabled:true,rotateAfter:100,passwordSet:false,password:""}]})}><Plus className="size-4"/>添加账号</Button></div>
    <p className="text-xs leading-5 text-muted-foreground">这里绑定 SMTP 发信凭据；IMAP 收信账号在下方单独管理。SMTP 仅接受 TLS 465 或 STARTTLS 587。请使用厂商支持的应用密码。</p>
    {draft.accounts.map((a,i)=><details key={a.id||`new-${i}`} open={!a.id||undefined} className="group rounded-lg border">
     <summary className="flex min-h-12 cursor-pointer list-none items-center gap-2 px-3 py-2 [&::-webkit-details-marker]:hidden"><span className="min-w-0 flex-1 truncate text-sm font-medium">{a.name||`新账号 ${i+1}`}</span><span className="text-xs text-muted-foreground">{a.enabled?`每轮 ${a.rotateAfter} 人`:"已停用"}</span><ChevronDown className="size-4 shrink-0 transition-transform group-open:rotate-180"/></summary>
     <div className="space-y-3 border-t p-3"><div className="grid min-w-0 gap-3 sm:grid-cols-2">
     <PoolField id={`${id}-${i}-name`} label="账号名称"><Input id={`${id}-${i}-name`} value={a.name} placeholder="例如：Gmail 主账号" onChange={e=>account(i,{name:e.target.value})}/></PoolField>
     <PoolField id={`${id}-${i}-from`} label="发件邮箱地址"><Input id={`${id}-${i}-from`} type="email" value={a.from} onChange={e=>account(i,{from:e.target.value})}/></PoolField>
     <PoolField id={`${id}-${i}-host`} label="SMTP 服务器"><Input id={`${id}-${i}-host`} value={a.host} placeholder="填写邮箱厂商提供的 SMTP 主机" onChange={e=>account(i,{host:e.target.value})}/></PoolField>
     <PoolField id={`${id}-${i}-port`} label="加密端口"><Select value={String(a.port)} onValueChange={v=>account(i,{port:Number(v)})}><SelectTrigger id={`${id}-${i}-port`}><SelectValue/></SelectTrigger><SelectContent><SelectItem value="465">465 · TLS</SelectItem><SelectItem value="587">587 · STARTTLS</SelectItem></SelectContent></Select></PoolField>
     <PoolField id={`${id}-${i}-username`} label="SMTP 登录账号"><Input id={`${id}-${i}-username`} autoComplete="off" value={a.username} onChange={e=>account(i,{username:e.target.value})}/></PoolField>
     <PoolField id={`${id}-${i}-password`} label={a.passwordSet?"应用密码（留空保留）":"应用密码"}><Input id={`${id}-${i}-password`} type="password" autoComplete="new-password" value={a.password||""} onChange={e=>account(i,{password:e.target.value})}/></PoolField>
     <PoolField id={`${id}-${i}-batch`} label="每轮分配收件人数"><Input id={`${id}-${i}-batch`} type="number" min={1} max={10000} value={a.rotateAfter} onChange={e=>account(i,{rotateAfter:Number(e.target.value)})}/></PoolField>
     </div><div className="flex min-h-11 items-center gap-3"><Label htmlFor={`${id}-${i}-active`}>启用这个账号</Label><Switch id={`${id}-${i}-active`} checked={a.enabled} onCheckedChange={enabled=>account(i,{enabled})}/><Button type="button" className="ml-auto" size="sm" variant="ghost" aria-label={`移除账号 ${a.name||i+1}`} onClick={()=>{if(window.confirm("移除账号后，已分配给该账号的待发邮件会暂停。确定移除？"))update({accounts:draft.accounts.filter((_,j)=>j!==i)})}}><Trash2 className="size-4"/>移除</Button></div></div>
    </details>)}
    {draft.accounts.length===0&&<p className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">尚未绑定发信账号。可以先添加账号，再开启分流。</p>}</div>
    {draft.enabled&&<div className="space-y-4 border-t pt-4">
     <PoolField id={`${id}-strategy`} label="账号使用方式"><Select value={draft.strategy} onValueChange={v=>update({strategy:v as SmtpPoolSettings["strategy"]})}><SelectTrigger id={`${id}-strategy`}><SelectValue/></SelectTrigger><SelectContent><SelectItem value="rotate">按每轮人数轮换账号</SelectItem><SelectItem value="single">固定使用单个账号</SelectItem></SelectContent></Select></PoolField>
     {draft.strategy==="single"&&<PoolField id={`${id}-account`} label="选择固定账号"><Select value={draft.accountId} onValueChange={accountId=>update({accountId})}><SelectTrigger id={`${id}-account`}><SelectValue placeholder="请选择已保存的账号"/></SelectTrigger><SelectContent>{draft.accounts.map((a,i)=>({a,i})).filter(({a})=>a.enabled).map(({a,i})=><SelectItem key={a.id||`new:${i}`} value={a.id||`new:${i}`}>{a.name||`新账号 ${i+1}`}</SelectItem>)}</SelectContent></Select><p className="text-xs text-muted-foreground">固定模式始终使用选中的账号，不参与轮换。</p></PoolField>}
     <div className="space-y-3"><h3 className="text-sm font-semibold">哪些收件邮箱使用外部通道</h3><label className="flex min-h-11 cursor-pointer items-center gap-3 rounded-lg bg-muted/40 p-3"><Checkbox checked={draft.allProviders} onCheckedChange={v=>update({allProviders:v===true})}/><span className="text-sm font-medium">全部收件厂商（相当于全选）</span></label>
      {!draft.allProviders&&<><div className="grid gap-1 sm:grid-cols-2">{query.data?.providers.map(p=><label key={p.id} className="flex min-h-11 cursor-pointer items-center gap-3 rounded-lg px-3 hover:bg-muted/40"><Checkbox checked={draft.providers.includes(p.id)} onCheckedChange={v=>update({providers:v===true?[...draft.providers,p.id]:draft.providers.filter(x=>x!==p.id)})}/><span className="text-sm">{p.name}</span></label>)}</div><PoolField id={`${id}-domains`} label="补充指定收件域名（可选）"><Input id={`${id}-domains`} value={domains} placeholder="company.com, example.org" onChange={e=>{setDomains(e.target.value);setSaved(false)}}/></PoolField></>}
      <p className="text-sm leading-6 text-muted-foreground">按收件人地址匹配，手动发信与自动转发共用。未勾选的厂商走原服务器；同封邮件含多个厂商时分别分流。公共域名使用精确匹配，企业邮箱可补充域名。</p></div>
     <p className="rounded-lg bg-muted/40 p-3 text-xs leading-5 text-muted-foreground">轮换按首次分配的收件人数计数，重试沿用同一账号且不重复计数。达到该账号的每轮人数后切换下一个，最后一个完成后回到第一个；服务重启保留进度。这不是厂商每日配额，不能绕过限额或封禁。</p>
    </div>}
    <p className="text-xs leading-5 text-muted-foreground">外部发送使用选中账号的发件地址，原回复目标会保留。密码加密保存。外部通道失败会进入队列重试，不静默切回原服务器；请勿把不支持代发的个人邮箱当成任意域名中继。</p>
   </>}
   {save.isError&&<p role="alert" className="text-sm text-destructive">{save.error.message}</p>}{saved&&<p role="status" className="text-sm text-primary">配置已保存。尚未发送测试邮件。</p>}
   <Button type="button" className="min-h-11 w-full sm:w-auto" onClick={()=>{setSaved(false);save.mutate()}}>{save.isPending&&<Loader2 className="size-4 animate-spin"/>}{admin?"保存后台账号池与分流":"保存使用配置"}</Button>
   {admin&&<p className="text-xs text-muted-foreground">此按钮只保存账号池；服务开关与用户授权请使用页面底部的“保存设置”。</p>}
  </fieldset>}
 </section>
}
function PoolField({id,label,children}:{id:string;label:string;children:React.ReactNode}){return <div className="min-w-0 space-y-2"><Label htmlFor={id}>{label}</Label>{children}</div>}
