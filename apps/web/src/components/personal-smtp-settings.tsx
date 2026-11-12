import * as React from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Send, Loader2, ChevronDown } from "lucide-react"
import { api } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Checkbox } from "@/components/ui/checkbox"
import { useToast } from "@/hooks/use-toast"

export function PersonalSmtpSettings() {
 const query = useQuery({queryKey:["personal-smtp-route"],queryFn:api.personalSmtpRoute})
 const qc=useQueryClient()
 const {toast}=useToast()
 const [enabled,setEnabled]=React.useState(false)
 const [gmailAddress,setGmailAddress]=React.useState("")
 const [appPassword,setAppPassword]=React.useState("")
 const [microsoft,setMicrosoft]=React.useState(true)
 const [yahoo,setYahoo]=React.useState(true)
 React.useEffect(()=>{
  if(!query.data)return
  setEnabled(query.data.enabled);setGmailAddress(query.data.gmailAddress)
  setMicrosoft(query.data.microsoft);setYahoo(query.data.yahoo)
 },[query.data])
 const save=useMutation({mutationFn:api.savePersonalSmtpRoute,onSuccess:(data)=>{
  setAppPassword("");qc.setQueryData(["personal-smtp-route"],data)
  toast({title:"个人发信路由已保存",description:"请在配置好 Gmail 后进行真实投递测试。"})
 },onError:(e)=>toast({title:"保存失败",description:e.message})})
 return <details className="group mb-4 overflow-hidden rounded-xl border bg-card">
  <summary className="min-h-11 cursor-pointer list-none space-y-2 p-4 sm:p-5 [&::-webkit-details-marker]:hidden">
   <h2 className="flex items-center gap-2 text-base font-semibold"><Send className="size-4"/>我的 Gmail 发信通道<span className="ml-auto text-xs font-normal text-muted-foreground">{query.data?.enabled ? "已启用" : "未启用"}</span><ChevronDown className="size-4 shrink-0 transition-transform group-open:rotate-180"/></h2>
   <p className="text-sm leading-6 text-muted-foreground">仅用于当前账号的手动发信和自动转发。按收件厂商选择通道，其他用户不会使用你的 Gmail。</p>
  </summary>
  {query.isLoading ? <p role="status" className="p-4 text-sm text-muted-foreground">加载发信设置…</p> : query.isError ? <div className="space-y-3 p-4"><p role="alert" className="text-sm text-destructive">无法读取个人发信设置</p><Button variant="outline" onClick={()=>{void query.refetch()}}>重试</Button></div> : <form className="space-y-4 p-4 sm:p-5" onSubmit={(e)=>{e.preventDefault();save.mutate({enabled,gmailAddress,appPassword,microsoft,yahoo})}}>
   <fieldset disabled={save.isPending} className="space-y-4">
    <div className="flex min-h-12 items-center justify-between gap-4"><Label htmlFor="personal-smtp-enabled">启用按收件厂商分流</Label><Switch id="personal-smtp-enabled" checked={enabled} onCheckedChange={setEnabled}/></div>
    <div className="grid gap-4 sm:grid-cols-2">
     <div className="space-y-2"><Label htmlFor="personal-gmail">个人 Gmail 地址</Label><Input id="personal-gmail" type="email" autoComplete="off" placeholder="yourname@gmail.com" value={gmailAddress} onChange={(e)=>setGmailAddress(e.target.value)} required={enabled}/></div>
     <div className="space-y-2"><Label htmlFor="personal-gmail-password">Google 应用专用密码{query.data?.passwordSet ? "（留空保留）" : ""}</Label><Input id="personal-gmail-password" type="password" autoComplete="new-password" placeholder="16 位应用专用密码" value={appPassword} onChange={(e)=>setAppPassword(e.target.value)} required={enabled&&!query.data?.passwordSet}/></div>
    </div>
    <p className="text-xs leading-5 text-muted-foreground">请在 Google 账号开启两步验证后生成应用专用密码，不要填写 Google 登录密码。更换 Gmail 地址时需要重新填写。</p>
    <div className="rounded-lg border divide-y">
     <label className="flex min-h-12 cursor-pointer items-center gap-3 px-3"><Checkbox checked={microsoft} onCheckedChange={(value)=>setMicrosoft(value===true)}/><span className="text-sm">微软邮箱 <span className="text-muted-foreground">Outlook / Hotmail / Live / MSN</span></span></label>
     <label className="flex min-h-12 cursor-pointer items-center gap-3 px-3"><Checkbox checked={yahoo} onCheckedChange={(value)=>setYahoo(value===true)}/><span className="text-sm">Yahoo 邮箱 <span className="text-muted-foreground">Yahoo / Ymail / Rocketmail</span></span></label>
    </div>
    <div className="rounded-lg bg-muted/50 p-3 text-sm leading-6 text-muted-foreground">命中规则：通过 smtp.gmail.com 加密发送，发件人显示你的 Gmail，回复地址保留原邮件的回复地址。未命中：使用原发信通道。仅匹配支持的公共邮箱域名，不包含企业自定义域名。Gmail 提交失败会报错并按队列规则重试，不切换通道。</div>
    <p className="text-xs leading-5 text-muted-foreground">收信（IMAP）和发信（SMTP）分别配置。Gmail 的发信限额及风控仍然适用；保存设置不会发送邮件。</p>
    {save.isError&&<p role="alert" className="text-sm text-destructive">{save.error.message}</p>}
    <Button type="submit" className="min-h-11 w-full sm:w-auto">{save.isPending&&<Loader2 className="size-4 animate-spin"/>}保存个人发信设置</Button>
   </fieldset>
  </form>}
 </details>
}
