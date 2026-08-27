import * as React from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  ArrowSort20Regular,
  ContactCard20Regular,
  Delete20Regular,
  Edit20Regular,
  Mail20Regular,
  PersonAdd20Regular,
  Search20Regular,
  Star20Filled,
  Star20Regular,
} from "@fluentui/react-icons"

import type { Contact } from "@/lib/api-types"
import { api } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { useToast } from "@/hooks/use-toast"

const favoriteStorageKey = "newszxcn:favorite-contact-ids"

function readFavorites() {
  try {
    const value = JSON.parse(localStorage.getItem(favoriteStorageKey) || "[]")
    return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : []
  } catch {
    return []
  }
}

export function ContactsWorkspace({
  canManage,
  createOpen,
  onCreateOpenChange,
  onCompose,
}: {
  canManage: boolean
  createOpen: boolean
  onCreateOpenChange: (open: boolean) => void
  onCompose: (email: string) => void
}) {
  const qc = useQueryClient()
  const { toast } = useToast()
  const contacts = useQuery({ queryKey: ["contacts"], queryFn: api.contacts, enabled: canManage, retry: false })
  const [query, setQuery] = React.useState("")
  const [sortAscending, setSortAscending] = React.useState(true)
  const [view, setView] = React.useState<"all" | "favorites">("all")
  const [selectedId, setSelectedId] = React.useState("")
  const [favorites, setFavorites] = React.useState<string[]>(readFavorites)
  const [pendingDelete, setPendingDelete] = React.useState<Contact | null>(null)
  const [editingContact, setEditingContact] = React.useState<Contact | null>(null)

  const items = React.useMemo(() => {
    const keyword = query.trim().toLocaleLowerCase()
    return [...(contacts.data?.items || [])]
      .filter((item) => view === "all" || favorites.includes(item.id))
      .filter((item) => !keyword || `${item.name} ${item.email} ${item.note}`.toLocaleLowerCase().includes(keyword))
      .sort((a, b) => (sortAscending ? 1 : -1) * a.name.localeCompare(b.name, "zh-CN", { sensitivity: "base" }))
  }, [contacts.data?.items, favorites, query, sortAscending, view])

  React.useEffect(() => {
    if (items.some((item) => item.id === selectedId)) return
    setSelectedId(items[0]?.id || "")
  }, [items, selectedId])

  const selected = items.find((item) => item.id === selectedId)
  const remove = useMutation({
    mutationFn: api.deleteContact,
    onSuccess: async () => {
      setPendingDelete(null)
      await qc.invalidateQueries({ queryKey: ["contacts"] })
      toast({ title: "联系人已删除" })
    },
    onError: (error) => toast({ title: "删除失败", description: error.message, variant: "destructive" }),
  })

  function toggleFavorite(id: string) {
    setFavorites((current) => {
      const next = current.includes(id) ? current.filter((item) => item !== id) : [...current, id]
      localStorage.setItem(favoriteStorageKey, JSON.stringify(next))
      return next
    })
  }

  return (
    <section className="contacts-workspace" aria-label="联系人">
      <section className="contacts-nav-pane">
        <header><ContactCard20Regular /><div><h1>联系人</h1><p>{contacts.data?.items.length || 0} 位联系人</p></div></header>
        <nav aria-label="联系人导航">
          <Button type="button" variant="ghost" data-selected={view === "all"} onClick={() => setView("all")}><ContactCard20Regular /><span>所有联系人</span><b>{contacts.data?.items.length || 0}</b></Button>
          <Button type="button" variant="ghost" data-selected={view === "favorites"} onClick={() => setView("favorites")}><Star20Regular /><span>收藏联系人</span><b>{favorites.length}</b></Button>
        </nav>
        <div className="contacts-list-label">联系人列表</div>
        <div className="contacts-list-placeholder">个人联系人</div>
      </section>

      <section className="contacts-main-pane">
        <header className="contacts-list-toolbar">
          <label className="contacts-search"><Search20Regular /><span className="sr-only">搜索联系人</span><Input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索联系人" type="search" autoComplete="off" /></label>
          <TooltipProvider delayDuration={450}>
            <Tooltip><TooltipTrigger asChild><Button type="button" variant="ghost" size="icon" aria-label={sortAscending ? "按 Z 到 A 排序" : "按 A 到 Z 排序"} onClick={() => setSortAscending((value) => !value)}><ArrowSort20Regular /></Button></TooltipTrigger><TooltipContent>{sortAscending ? "A–Z 排序" : "Z–A 排序"}</TooltipContent></Tooltip>
          </TooltipProvider>
        </header>
        <div className="contacts-content">
          <div className="contacts-list" role="listbox" aria-label="联系人列表">
            {items.map((item) => (
              <Button key={item.id} type="button" variant="ghost" role="option" aria-selected={selectedId === item.id} onClick={() => setSelectedId(item.id)}>
                <span className="contact-avatar">{(item.name || item.email).slice(0, 1).toLocaleUpperCase()}</span>
                <span><strong>{item.name}</strong><small>{item.email}</small></span>
                {favorites.includes(item.id) && <Star20Filled aria-label="已收藏" />}
              </Button>
            ))}
            {!contacts.isLoading && items.length === 0 && <div className="contacts-empty-list"><ContactCard20Regular /><strong>{query ? "没有匹配的联系人" : "还没有联系人"}</strong><span>{query ? "请尝试其他搜索内容。" : "使用“新建联系人”添加第一位联系人。"}</span></div>}
            {contacts.isLoading && <div className="contacts-empty-list">正在加载联系人…</div>}
          </div>

          <article className="contact-detail-pane">
            {selected ? (
              <>
                <header><span className="contact-avatar is-large">{(selected.name || selected.email).slice(0, 1).toLocaleUpperCase()}</span><div><h2>{selected.name}</h2><p>{selected.email}</p></div></header>
                <div className="contact-detail-actions">
                  <Button type="button" onClick={() => onCompose(selected.email)}><Mail20Regular />发送邮件</Button>
                  <Button type="button" variant="outline" onClick={() => setEditingContact(selected)}><Edit20Regular />编辑</Button>
                  <Button type="button" variant="outline" onClick={() => toggleFavorite(selected.id)}>{favorites.includes(selected.id) ? <Star20Filled /> : <Star20Regular />}{favorites.includes(selected.id) ? "取消收藏" : "收藏"}</Button>
                  <Button type="button" variant="ghost" className="text-destructive" onClick={() => setPendingDelete(selected)}><Delete20Regular />删除</Button>
                </div>
                <dl><div><dt>邮箱</dt><dd>{selected.email}</dd></div><div><dt>备注</dt><dd>{selected.note || "暂无备注"}</dd></div></dl>
              </>
            ) : (
              <div className="contact-detail-empty"><ContactCard20Regular /><strong>选择一位联系人</strong><span>查看联系方式或直接发送邮件。</span></div>
            )}
          </article>
        </div>
      </section>

      <ContactCreateSheet open={createOpen || !!editingContact} contact={editingContact} canManage={canManage} onOpenChange={(open) => { if (!open) setEditingContact(null); onCreateOpenChange(open) }} />
      <ConfirmDialog open={!!pendingDelete} title="删除联系人？" description={pendingDelete ? `${pendingDelete.email} 将从联系人列表中移除。` : undefined} confirmText="删除联系人" destructive pending={remove.isPending} onOpenChange={(open) => { if (!open) setPendingDelete(null) }} onConfirm={() => pendingDelete && remove.mutate(pendingDelete.id)} />
    </section>
  )
}

function ContactCreateSheet({ open, contact, canManage, onOpenChange }: { open: boolean; contact: Contact | null; canManage: boolean; onOpenChange: (open: boolean) => void }) {
  const qc = useQueryClient()
  const { toast } = useToast()
  const [name, setName] = React.useState("")
  const [email, setEmail] = React.useState("")
  const [note, setNote] = React.useState("")
  const create = useMutation({
    mutationFn: () => api.createContact({ name: name.trim(), email: email.trim(), note: note.trim() }),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["contacts"] })
      toast({ title: contact ? "联系人已更新" : "联系人已保存" })
      onOpenChange(false)
    },
    onError: (error) => toast({ title: "保存失败", description: error.message, variant: "destructive" }),
  })

  React.useEffect(() => {
    if (!open) {
      setName("")
      setEmail("")
      setNote("")
      return
    }
    setName(contact?.name || "")
    setEmail(contact?.email || "")
    setNote(contact?.note || "")
  }, [contact, open])

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="contact-create-sheet w-[min(420px,calc(100vw-16px))] max-w-none p-0 sm:max-w-[420px]" overlayClassName="contact-create-overlay" aria-describedby={undefined}>
        <SheetTitle className="sr-only">{contact ? "编辑联系人" : "新建联系人"}</SheetTitle>
        <form onSubmit={(event) => { event.preventDefault(); if (email.trim() && !create.isPending) create.mutate() }}>
          <header><span>{contact ? <Edit20Regular /> : <PersonAdd20Regular />}</span><div><h2>{contact ? "编辑联系人" : "新建联系人"}</h2><p>联系人只对当前账户可见。</p></div></header>
          <div className="contact-create-fields">
            <div><Label htmlFor="contact-name">姓名</Label><Input id="contact-name" value={name} onChange={(event) => setName(event.target.value)} placeholder="姓名" maxLength={100} /></div>
            <div><Label htmlFor="contact-email">邮箱</Label><Input id="contact-email" value={email} onChange={(event) => setEmail(event.target.value)} placeholder="name@example.com" type="email" required autoComplete="email" disabled={!!contact} /></div>
            <div><Label htmlFor="contact-note">备注</Label><Textarea id="contact-note" value={note} onChange={(event) => setNote(event.target.value)} placeholder="添加备注" maxLength={500} /></div>
          </div>
          <footer><Button type="button" variant="outline" onClick={() => onOpenChange(false)}>取消</Button><Button type="submit" disabled={!canManage || !email.trim() || create.isPending}>{create.isPending ? "保存中…" : contact ? "保存更改" : "保存联系人"}</Button></footer>
        </form>
      </SheetContent>
    </Sheet>
  )
}
