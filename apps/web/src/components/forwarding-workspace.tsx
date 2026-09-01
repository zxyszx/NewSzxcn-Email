import * as React from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import {
  Add20Regular,
  ArrowForward20Regular,
  ArrowClockwise20Regular,
  CheckmarkCircle20Filled,
  CheckmarkCircle20Regular,
  ChevronLeft20Regular,
  ChevronRight20Regular,
  Clock20Regular,
  Delete20Regular,
  LockClosed20Regular,
  Mail20Regular,
  MailCheckmark20Regular,
  QuestionCircle20Regular,
  Search20Regular,
  Warning20Regular,
} from "@fluentui/react-icons";
import {
  api,
  type ForwardingSettings,
  type ForwardingVerifiedEmail,
  type Mailbox,
} from "@/lib/api";
import { cn, formatDateTime } from "@/lib/utils";
import { useToast } from "@/hooks/use-toast";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Sheet,
  SheetContent,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";

type ForwardingTab = "settings" | "verification";
type VerificationPurpose = {
  scope: "verification" | "account" | "mailbox";
  mailboxId?: string;
};
type Props = {
  mailboxes: Mailbox[];
  tab: ForwardingTab;
  onTabChange: (tab: ForwardingTab) => void;
};
const mailboxPageSize = 8;
const verificationPageSize = 8;

export function ForwardingWorkspace({ mailboxes, tab, onTabChange }: Props) {
  const qc = useQueryClient();
  const { toast } = useToast();
  const [params, setParams] = useSearchParams();
  const [mailboxSearch, setMailboxSearch] = React.useState(
    params.get("q") || "",
  );
  const [verificationSearch, setVerificationSearch] = React.useState("");
  const [selectedScope, setSelectedScope] = React.useState(
    params.get("scope") || "all",
  );
  const [draftTargets, setDraftTargets] = React.useState<string[]>([]);
  const [targetSearch, setTargetSearch] = React.useState("");
  const [addOpen, setAddOpen] = React.useState(params.get("drawer") === "add");
  const [helpOpen, setHelpOpen] = React.useState(false);
  const [verificationDraft, setVerificationDraft] = React.useState("");
  const [verificationNote, setVerificationNote] = React.useState("");
  const [verificationStage, setVerificationStage] = React.useState<
    "entry" | "sent" | "success"
  >("entry");
  const [verificationItem, setVerificationItem] =
    React.useState<ForwardingVerifiedEmail | null>(null);
  const [verificationPurpose, setVerificationPurpose] =
    React.useState<VerificationPurpose>({ scope: "verification" });
  const [pendingDelete, setPendingDelete] =
    React.useState<ForwardingVerifiedEmail | null>(null);
  const [pageTransitioning, setPageTransitioning] = React.useState(false);
  const mailboxListRef = React.useRef<HTMLDivElement>(null);
  const verificationListRef = React.useRef<HTMLDivElement>(null!);
  const pollingStartedAt = React.useRef(Date.now());

  const forwarding = useQuery({
    queryKey: ["forwarding-settings"],
    queryFn: api.forwardingSettings,
  });
  const settings = forwarding.data;
  const verifiedItems = React.useMemo(
    () => (settings?.verifiedEmails || []).filter((item) => item.verified),
    [settings?.verifiedEmails],
  );
  const pendingItems = React.useMemo(
    () => (settings?.verifiedEmails || []).filter((item) => !item.verified),
    [settings?.verifiedEmails],
  );
  const verifiedEmails = React.useMemo(
    () =>
      verifiedItems
        .map((item) => item.email)
        .sort((a, b) => a.localeCompare(b)),
    [verifiedItems],
  );
  const accountTargets = React.useMemo(
    () => forwardingTargets(settings),
    [settings],
  );
  const mailboxTargets = React.useMemo(() => {
    const map = new Map<string, string[]>();
    for (const rule of settings?.mailboxRules || [])
      map.set(
        rule.mailboxId,
        rule.targetEmails?.length
          ? rule.targetEmails
          : rule.targetEmail
            ? [rule.targetEmail]
            : [],
      );
    return map;
  }, [settings?.mailboxRules]);
  const selectedMailbox =
    selectedScope === "all"
      ? null
      : mailboxes.find((item) => item.id === selectedScope) || null;
  const savedTargets = selectedMailbox
    ? mailboxTargets.get(selectedMailbox.id) || []
    : accountTargets;

  React.useEffect(
    () => setDraftTargets(savedTargets),
    [selectedScope, savedTargets.join("\n")],
  );
  React.useEffect(() => {
    if (!verificationItem) return;
    const current = settings?.verifiedEmails.find(
      (item) => item.id === verificationItem.id,
    );
    if (!current) return;
    setVerificationItem(current);
    if (current.verified && verificationStage !== "success") {
      setVerificationStage("success");
      setDraftTargets((targets) =>
        targets.includes(current.email) ? targets : [...targets, current.email],
      );
      toast({
        title: "邮箱验证成功",
        description: `${current.email} 已可以用于邮件转发。`,
      });
    }
  }, [
    settings?.verifiedEmails,
    toast,
    verificationItem?.id,
    verificationStage,
  ]);

  const hasPending =
    pendingItems.length > 0 ||
    (settings?.pendingBindings || []).some(
      (item) => item.status === "pending_verification",
    );
  React.useEffect(() => {
    const refresh = () => {
      if (document.visibilityState === "visible") void forwarding.refetch();
    };
    const visible = () => {
      if (document.visibilityState === "visible") refresh();
    };
    window.addEventListener("newszxcn:refresh-forwarding", refresh);
    document.addEventListener("visibilitychange", visible);
    if (!hasPending)
      return () => {
        window.removeEventListener("newszxcn:refresh-forwarding", refresh);
        document.removeEventListener("visibilitychange", visible);
      };
    pollingStartedAt.current = Date.now();
    let timer = 0;
    const schedule = () => {
      timer = window.setTimeout(
        async () => {
          if (document.visibilityState === "visible")
            await forwarding.refetch();
          schedule();
        },
        Date.now() - pollingStartedAt.current < 60_000 ? 5_000 : 15_000,
      );
    };
    schedule();
    return () => {
      window.removeEventListener("newszxcn:refresh-forwarding", refresh);
      document.removeEventListener("visibilitychange", visible);
      window.clearTimeout(timer);
    };
  }, [forwarding.refetch, hasPending]);

  const cache = React.useCallback(
    (next: ForwardingSettings) =>
      qc.setQueryData(["forwarding-settings"], next),
    [qc],
  );
  const saveScope = useMutation({
    mutationFn: () =>
      selectedMailbox
        ? api.updateMailboxForwarding(selectedMailbox.id, draftTargets)
        : api.updateAccountForwarding(draftTargets),
    onSuccess: (next) => {
      cache(next);
      toast({ title: draftTargets.length ? "转发设置已保存" : "转发已停用" });
    },
    onError: (error) =>
      toast({ title: "保存失败", description: error.message }),
  });
  const addVerified = useMutation({
    mutationFn: (email: string) =>
      verificationPurpose.scope === "verification"
        ? api.addForwardingVerifiedEmail(email)
        : api.createForwardingPendingBinding({
            email,
            scope: verificationPurpose.scope,
            mailboxId: verificationPurpose.mailboxId,
          }),
    onSuccess: (next, email) => {
      cache(next);
      const item =
        next.verifiedEmails.find(
          (entry) => entry.email.toLowerCase() === email.toLowerCase(),
        ) || null;
      setVerificationItem(item);
      setVerificationStage(item?.verified ? "success" : "sent");
      toast({
        title:
          item?.deliveryStatus === "failed"
            ? "验证邮件发送失败"
            : item?.verified
              ? "邮箱已验证"
              : "验证邮件已发送",
        description: item?.deliveryError || "请检查目标邮箱的收件箱和垃圾邮件",
      });
    },
    onError: (error) =>
      toast({ title: "无法添加验证邮箱", description: error.message }),
  });
  const resendVerified = useMutation({
    mutationFn: (item: ForwardingVerifiedEmail) =>
      api.resendForwardingVerifiedEmail(item.id),
    onSuccess: (next, item) => {
      cache(next);
      setVerificationItem(
        next.verifiedEmails.find((entry) => entry.id === item.id) || item,
      );
      toast({ title: "验证邮件已重新发送" });
    },
    onError: (error) =>
      toast({ title: "重新发送失败", description: error.message }),
  });
  const deleteVerified = useMutation({
    mutationFn: (item: ForwardingVerifiedEmail) =>
      api.deleteForwardingVerifiedEmail(item.id),
    onSuccess: (next) => {
      cache(next);
      setPendingDelete(null);
      closeVerification();
      toast({ title: "验证邮箱已删除" });
    },
    onError: (error) =>
      toast({ title: "删除失败", description: error.message }),
  });

  const mailboxFilter = mailboxSearch.trim().toLowerCase();
  const filteredMailboxes = mailboxes.filter(
    (item) =>
      !mailboxFilter || item.address.toLowerCase().includes(mailboxFilter),
  );
  const verificationFilter = verificationSearch.trim().toLowerCase();
  const filteredVerification = [...pendingItems, ...verifiedItems].filter(
    (item) =>
      !verificationFilter ||
      item.email.toLowerCase().includes(verificationFilter),
  );
  const requestedPage = Math.max(
    1,
    Number.parseInt(params.get("page") || "1", 10) || 1,
  );
  const total =
    tab === "settings" ? filteredMailboxes.length : filteredVerification.length;
  const pageSize = tab === "settings" ? mailboxPageSize : verificationPageSize;
  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  const currentPage = Math.min(requestedPage, totalPages);
  const start = (currentPage - 1) * pageSize;
  const pageMailboxes = filteredMailboxes.slice(start, start + mailboxPageSize);
  const pageVerification = filteredVerification.slice(
    start,
    start + verificationPageSize,
  );
  const latestVerifiedAt = verifiedItems
    .map((item) => item.verifiedAt || item.createdAt)
    .sort((a, b) => Date.parse(b) - Date.parse(a))[0];
  const ownAddresses = new Set(
    mailboxes.map((item) => item.address.toLowerCase()),
  );
  const emailDraft = verificationDraft.trim().toLowerCase();
  const verificationError = !emailDraft
    ? ""
    : !looksLikeEmail(emailDraft)
      ? "请输入有效的邮箱地址"
      : ownAddresses.has(emailDraft)
        ? "不能把当前账户自己的邮箱添加为转发目标"
        : settings?.verifiedEmails.some(
              (item) => item.email.toLowerCase() === emailDraft,
            )
          ? "该邮箱已经添加"
          : "";
  const changed = draftTargets.join("\n") !== savedTargets.join("\n");
  const availableTargets = verifiedEmails.filter(
    (email) =>
      !draftTargets.includes(email) &&
      (!targetSearch ||
        email.toLowerCase().includes(targetSearch.toLowerCase())),
  );

  React.useEffect(() => {
    if (requestedPage > totalPages)
      updateParams({ page: String(totalPages) }, true);
  }, [requestedPage, totalPages]);
  function updateParams(
    changes: Record<string, string | null>,
    replace = false,
  ) {
    const next = new URLSearchParams(params);
    for (const [key, value] of Object.entries(changes))
      value ? next.set(key, value) : next.delete(key);
    setParams(next, { replace });
  }
  function setPage(page: number) {
    const nextPage = Math.max(1, Math.min(totalPages, page));
    if (nextPage === currentPage || pageTransitioning) return;
    setPageTransitioning(true);
    window.setTimeout(() => {
      updateParams({ page: String(nextPage) });
      requestAnimationFrame(() => {
        (tab === "settings"
          ? mailboxListRef.current
          : verificationListRef.current
        )?.scrollTo({ top: 0 });
        setPageTransitioning(false);
      });
    }, 160);
  }
  function updateMailboxSearch(value: string) {
    setMailboxSearch(value);
    updateParams({ page: "1", q: value || null }, true);
  }
  function selectScope(scope: string) {
    setSelectedScope(scope);
    setTargetSearch("");
    updateParams({ scope: scope === "all" ? null : scope }, true);
  }
  function openVerification(
    purpose: VerificationPurpose = { scope: "verification" },
    preset = "",
  ) {
    setVerificationPurpose(purpose);
    setVerificationDraft(preset);
    setVerificationNote("");
    setVerificationStage("entry");
    setVerificationItem(null);
    setAddOpen(true);
    updateParams({ drawer: "add" }, true);
  }
  function closeVerification() {
    setAddOpen(false);
    updateParams({ drawer: null }, true);
  }
  function usageCount(email: string) {
    const value = email.toLowerCase();
    let count = accountTargets.some((target) => target.toLowerCase() === value)
      ? 1
      : 0;
    for (const targets of mailboxTargets.values())
      if (targets.some((target) => target.toLowerCase() === value)) count += 1;
    return count;
  }

  return (
    <section className="forwarding-workspace" aria-label="邮件转发">
      <ForwardingToolbar
        tab={tab}
        pending={pendingItems.length}
        fetching={forwarding.isFetching}
        onTab={onTabChange}
        onHelp={() => setHelpOpen(true)}
        onRefresh={() => void forwarding.refetch()}
      />
      <div className="forwarding-tab-content">
        {forwarding.isError ? (
          <ErrorState
            message={forwarding.error.message}
            onRetry={() => void forwarding.refetch()}
          />
        ) : forwarding.isLoading ? (
          <ForwardingLoading />
        ) : tab === "verification" ? (
          <VerificationManagement
            items={pageVerification}
            total={filteredVerification.length}
            verifiedCount={verifiedItems.length}
            pendingCount={pendingItems.length}
            latestVerifiedAt={latestVerifiedAt}
            search={verificationSearch}
            onSearch={setVerificationSearch}
            onAdd={() => openVerification()}
            onDelete={setPendingDelete}
            onResend={(item) => resendVerified.mutate(item)}
            usageCount={usageCount}
            busy={resendVerified.isPending || deleteVerified.isPending}
            listRef={verificationListRef}
            pageTransitioning={pageTransitioning}
            currentPage={currentPage}
            totalPages={totalPages}
            onPage={setPage}
          />
        ) : verifiedItems.length === 0 ? (
          <ForwardingOnboarding
            pendingItems={pendingItems}
            onAdd={() => openVerification()}
            onRefresh={() => void forwarding.refetch()}
            onCancel={setPendingDelete}
            onResend={(item) => resendVerified.mutate(item)}
            resendPending={resendVerified.isPending}
          />
        ) : (
          <div className="forwarding-management">
            <div className="forwarding-glass-card forwarding-master-pane">
              <div className="forwarding-master-header">
                <div>
                  <h2>我的邮箱</h2>
                  <p>搜索并选择需要设置转发的邮箱。</p>
                </div>
                <div className="forwarding-search">
                  <Search20Regular />
                  <Input
                    type="search"
                    value={mailboxSearch}
                    onChange={(event) =>
                      updateMailboxSearch(event.target.value)
                    }
                    placeholder="搜索邮箱"
                    aria-label="搜索邮箱"
                    autoComplete="off"
                  />
                </div>
              </div>
              <button
                type="button"
                className={cn(
                  "forwarding-scope-row is-all",
                  selectedScope === "all" && "is-selected",
                )}
                onClick={() => selectScope("all")}
              >
                <span className="forwarding-scope-icon">
                  <Mail20Regular />
                </span>
                <span>
                  <strong>全部邮箱</strong>
                  <small>应用于当前账户下的所有邮箱</small>
                </span>
                <ScopeStatus targets={accountTargets} />
              </button>
              <div
                ref={mailboxListRef}
                className={cn(
                  "forwarding-scope-list",
                  pageTransitioning && "is-transitioning",
                )}
              >
                {pageMailboxes.map((mailbox) => (
                  <button
                    key={mailbox.id}
                    type="button"
                    className={cn(
                      "forwarding-scope-row",
                      selectedScope === mailbox.id && "is-selected",
                    )}
                    onClick={() => selectScope(mailbox.id)}
                  >
                    <span className="forwarding-mailbox-avatar">
                      {mailbox.address.slice(0, 1).toUpperCase()}
                    </span>
                    <span>
                      <strong>{mailbox.address}</strong>
                      <small>
                        {mailboxTargets.get(mailbox.id)?.length
                          ? `${mailboxTargets.get(mailbox.id)?.length} 个独立目标`
                          : accountTargets.length
                            ? "继承全局设置"
                            : "未启用"}
                      </small>
                    </span>
                    <ScopeStatus
                      targets={mailboxTargets.get(mailbox.id) || []}
                      inherited={accountTargets.length > 0}
                    />
                  </button>
                ))}
                {!filteredMailboxes.length && (
                  <div className="forwarding-list-empty">
                    <Search20Regular />
                    <strong>没有匹配的邮箱</strong>
                  </div>
                )}
              </div>
              <PaginationBar
                total={filteredMailboxes.length}
                noun="邮箱"
                currentPage={currentPage}
                totalPages={totalPages}
                onPageChange={setPage}
              />
            </div>
            <ForwardingDetailPane
              mailbox={selectedMailbox}
              inheritedTargets={accountTargets}
              targets={draftTargets}
              verifiedTargets={availableTargets}
              allVerifiedTargets={verifiedEmails}
              targetSearch={targetSearch}
              changed={changed}
              saving={saveScope.isPending}
              onTargetSearch={setTargetSearch}
              onToggle={(checked) =>
                setDraftTargets(
                  checked
                    ? draftTargets.length
                      ? draftTargets
                      : verifiedEmails.slice(0, 1)
                    : [],
                )
              }
              onAddTarget={(email) => {
                setDraftTargets((targets) =>
                  targets.includes(email) ? targets : [...targets, email],
                );
                setTargetSearch("");
              }}
              onRemoveTarget={(email) =>
                setDraftTargets((targets) =>
                  targets.filter((target) => target !== email),
                )
              }
              onVerifyNew={(email) =>
                openVerification(
                  selectedMailbox
                    ? { scope: "mailbox", mailboxId: selectedMailbox.id }
                    : { scope: "account" },
                  email,
                )
              }
              onSave={() => saveScope.mutate()}
            />
          </div>
        )}
      </div>
      <VerificationSheet
        open={addOpen}
        item={verificationItem}
        stage={verificationStage}
        email={verificationDraft}
        note={verificationNote}
        error={verificationError}
        sending={addVerified.isPending}
        refreshing={forwarding.isFetching}
        resendPending={resendVerified.isPending}
        autoBind={verificationPurpose.scope !== "verification"}
        bindingLabel={
          verificationPurpose.scope === "account"
            ? "全部邮箱（账号级转发）"
            : verificationPurpose.scope === "mailbox"
              ? mailboxes.find(
                  (mailbox) => mailbox.id === verificationPurpose.mailboxId,
                )?.address || "当前邮箱"
              : "仅加入已验证邮箱列表"
        }
        onEmail={setVerificationDraft}
        onNote={setVerificationNote}
        onClose={closeVerification}
        onSend={() => addVerified.mutate(emailDraft)}
        onRefresh={() => void forwarding.refetch()}
        onResend={() =>
          verificationItem && resendVerified.mutate(verificationItem)
        }
        onChangeAddress={() => {
          setVerificationStage("entry");
          setVerificationItem(null);
        }}
        onCancel={() =>
          verificationItem
            ? setPendingDelete(verificationItem)
            : closeVerification()
        }
        onStart={() => {
          if (verificationItem)
            setDraftTargets((targets) =>
              targets.includes(verificationItem.email)
                ? targets
                : [...targets, verificationItem.email],
            );
          setTargetSearch("");
          onTabChange("settings");
          closeVerification();
        }}
      />
      <Sheet open={helpOpen} onOpenChange={setHelpOpen}>
        <SheetContent side="right" className="forwarding-help-sheet">
          <SheetHeader>
            <SheetTitle>邮件转发说明</SheetTitle>
          </SheetHeader>
          <div className="forwarding-help-content">
            <p>先验证接收邮箱，再选择全部邮箱或某个具体邮箱设置目标。</p>
            <p>全局转发适用于全部邮箱；单邮箱目标会在全局目标之外追加。</p>
            <p>同一目标同时出现在两种规则中时只转发一次。</p>
          </div>
        </SheetContent>
      </Sheet>
      <ConfirmDialog
        open={!!pendingDelete}
        title={pendingDelete?.verified ? "删除验证邮箱？" : "取消邮箱验证？"}
        description={
          pendingDelete
            ? usageCount(pendingDelete.email) > 0
              ? `该邮箱正在被 ${usageCount(pendingDelete.email)} 条转发规则使用。删除后相关规则将停止转发。`
              : `确定删除 ${pendingDelete.email}？`
            : undefined
        }
        confirmText="删除"
        destructive
        pending={deleteVerified.isPending}
        onOpenChange={(open) => {
          if (!open) setPendingDelete(null);
        }}
        onConfirm={() => pendingDelete && deleteVerified.mutate(pendingDelete)}
      />
    </section>
  );
}

function ForwardingToolbar({
  tab,
  pending,
  fetching,
  onTab,
  onHelp,
  onRefresh,
}: {
  tab: ForwardingTab;
  pending: number;
  fetching: boolean;
  onTab: (tab: ForwardingTab) => void;
  onHelp: () => void;
  onRefresh: () => void;
}) {
  return (
    <div className="forwarding-toolbar">
      <div className="forwarding-tabs" role="tablist" aria-label="邮件转发页面">
        <Button
          type="button"
          variant="ghost"
          role="tab"
          aria-selected={tab === "settings"}
          onClick={() => onTab("settings")}
        >
          <ArrowForward20Regular />
          转发设置
        </Button>
        <Button
          type="button"
          variant="ghost"
          role="tab"
          aria-selected={tab === "verification"}
          onClick={() => onTab("verification")}
        >
          <CheckmarkCircle20Regular />
          验证邮箱{pending > 0 && <span>{pending}</span>}
        </Button>
      </div>
      <TooltipProvider delayDuration={350}>
        <div className="forwarding-toolbar-actions">
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                aria-label="邮件转发帮助"
                onClick={onHelp}
              >
                <QuestionCircle20Regular />
              </Button>
            </TooltipTrigger>
            <TooltipContent>帮助</TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                aria-label="刷新转发状态"
                disabled={fetching}
                onClick={onRefresh}
              >
                <ArrowClockwise20Regular
                  className={cn(fetching && "animate-spin")}
                />
              </Button>
            </TooltipTrigger>
            <TooltipContent>刷新状态</TooltipContent>
          </Tooltip>
        </div>
      </TooltipProvider>
    </div>
  );
}

function ForwardingOnboarding({
  pendingItems,
  onAdd,
  onRefresh,
  onCancel,
  onResend,
  resendPending,
}: {
  pendingItems: ForwardingVerifiedEmail[];
  onAdd: () => void;
  onRefresh: () => void;
  onCancel: (item: ForwardingVerifiedEmail) => void;
  onResend: (item: ForwardingVerifiedEmail) => void;
  resendPending: boolean;
}) {
  const current = pendingItems[0];
  const active = current ? 2 : 1;
  return (
    <article className="forwarding-glass-card forwarding-onboarding">
      <div className="forwarding-onboarding-copy">
        <span className="forwarding-onboarding-icon">
          <ArrowForward20Regular />
        </span>
        <h1>邮件转发</h1>
        <p>
          先验证一个接收邮箱，再将 NewSzxcn
          邮箱与它绑定。验证完成后会自动打开“我的邮箱”列表。
        </p>
      </div>
      <div className="forwarding-steps">
        {[
          ["添加转发邮箱", "输入用于接收转发邮件的目标邮箱地址。"],
          [
            "完成邮箱验证",
            "我们会发送一封验证邮件，请在目标邮箱中点击确认链接。",
          ],
          [
            "从我的邮箱中绑定",
            "验证成功后，搜索并选择需要转发的 NewSzxcn 邮箱。",
          ],
        ].map(([title, text], index) => {
          const step = index + 1;
          return (
            <div
              key={title}
              className={cn(
                "forwarding-step",
                step === active && "is-active",
                step < active && "is-complete",
              )}
            >
              <span>{step < active ? <CheckmarkCircle20Filled /> : step}</span>
              <div>
                <strong>{title}</strong>
                <p>{text}</p>
              </div>
            </div>
          );
        })}
      </div>
      {current ? (
        <div className="forwarding-waiting">
          <span>
            <Clock20Regular />
          </span>
          <div>
            <strong>验证邮件已发送</strong>
            <p>请前往 {current.email}，打开验证邮件并点击“确认验证”。</p>
            <small>
              发送时间：
              {formatDateTime(current.verificationSentAt || current.createdAt)}{" "}
              · 等待验证
            </small>
          </div>
          <div>
            <ResendButton
              item={current}
              pending={resendPending}
              onResend={() => onResend(current)}
            />
            <Button
              type="button"
              variant="ghost"
              onClick={() => onCancel(current)}
            >
              取消验证
            </Button>
          </div>
        </div>
      ) : (
        <Button
          type="button"
          size="lg"
          className="forwarding-onboarding-primary"
          onClick={onAdd}
        >
          <Add20Regular />
          添加转发邮箱
        </Button>
      )}
      <div className="forwarding-onboarding-secondary-actions">
        <Button
          type="button"
          variant="ghost"
          className="forwarding-onboarding-refresh"
          onClick={onRefresh}
        >
          <ArrowClockwise20Regular />
          我已经添加，刷新验证状态
        </Button>
      </div>
    </article>
  );
}

function ForwardingDetailPane({
  mailbox,
  inheritedTargets,
  targets,
  verifiedTargets,
  allVerifiedTargets,
  targetSearch,
  changed,
  saving,
  onTargetSearch,
  onToggle,
  onAddTarget,
  onRemoveTarget,
  onVerifyNew,
  onSave,
}: {
  mailbox: Mailbox | null;
  inheritedTargets: string[];
  targets: string[];
  verifiedTargets: string[];
  allVerifiedTargets: string[];
  targetSearch: string;
  changed: boolean;
  saving: boolean;
  onTargetSearch: (value: string) => void;
  onToggle: (checked: boolean) => void;
  onAddTarget: (email: string) => void;
  onRemoveTarget: (email: string) => void;
  onVerifyNew: (email: string) => void;
  onSave: () => void;
}) {
  const searchRootRef = React.useRef<HTMLDivElement>(null);
  const [searchOpen, setSearchOpen] = React.useState(false);
  const normalized = targetSearch.trim().toLowerCase();
  const newEmail =
    looksLikeEmail(normalized) &&
    !allVerifiedTargets.some((email) => email.toLowerCase() === normalized);
  const selectedVerifiedEmail =
    looksLikeEmail(normalized) &&
    allVerifiedTargets.some((email) => email.toLowerCase() === normalized) &&
    !verifiedTargets.some((email) => email.toLowerCase() === normalized);
  React.useEffect(() => {
    if (!searchOpen) return;
    const closeOnOutside = (event: PointerEvent) => {
      if (!(event.target instanceof Node)) return;
      if (!searchRootRef.current?.contains(event.target)) setSearchOpen(false);
    };
    document.addEventListener("pointerdown", closeOnOutside);
    return () => document.removeEventListener("pointerdown", closeOnOutside);
  }, [searchOpen]);
  function addVerifiedTarget(email: string) {
    onAddTarget(email);
    setSearchOpen(false);
  }
  function verifyNewTarget(email: string) {
    onVerifyNew(email);
    setSearchOpen(false);
  }
  return (
    <article className="forwarding-glass-card forwarding-detail-pane">
      <header className="forwarding-detail-header">
        <div>
          <h2>{mailbox ? mailbox.address : "全部邮箱转发"}</h2>
          <p>
            {mailbox
              ? "为此邮箱设置独立转发目标。"
              : "此规则应用于当前账户下的全部 NewSzxcn 邮箱。"}
          </p>
        </div>
        <label>
          <span>{targets.length ? "已启用" : "未启用"}</span>
          <Switch checked={targets.length > 0} onCheckedChange={onToggle} />
        </label>
      </header>
      <div className="forwarding-detail-scroll">
        {mailbox && (
          <section className="forwarding-detail-section">
            <div className="forwarding-section-title">
              <div>
                <h3>继承的全局转发</h3>
                <p>全局目标在单邮箱设置中只读。</p>
              </div>
              <LockClosed20Regular />
            </div>
            <div className="forwarding-target-list">
              {inheritedTargets.length ? (
                inheritedTargets.map((email) => (
                  <TargetRow key={email} email={email} readOnly />
                ))
              ) : (
                <p className="forwarding-detail-empty">
                  当前没有启用全局转发。
                </p>
              )}
            </div>
          </section>
        )}
        <section className="forwarding-detail-section">
          <div className="forwarding-section-title">
            <div>
              <h3>{mailbox ? "此邮箱的独立转发" : "转发目标"}</h3>
              <p>只有已验证邮箱才能启用。</p>
            </div>
          </div>
          <div className="forwarding-target-list">
            {targets.length ? (
              targets.map((email) => (
                <TargetRow
                  key={email}
                  email={email}
                  onRemove={() => onRemoveTarget(email)}
                />
              ))
            ) : (
              <p className="forwarding-detail-empty">尚未添加转发目标。</p>
            )}
          </div>
        </section>
        <section className="forwarding-detail-section">
          <div className="forwarding-section-title">
            <div>
              <h3>添加目标</h3>
              <p>选择已验证邮箱，或输入新地址发起验证。</p>
            </div>
          </div>
          <div ref={searchRootRef} className="forwarding-target-search-wrap">
            <div className="forwarding-target-search">
              <Search20Regular />
              <Input
                value={targetSearch}
                onFocus={() => setSearchOpen(true)}
                onChange={(event) => {
                  onTargetSearch(event.target.value);
                  setSearchOpen(true);
                }}
                onKeyDown={(event) => {
                  if (event.key === "Escape") {
                    event.preventDefault();
                    setSearchOpen(false);
                    event.currentTarget.blur();
                  }
                  if (event.key === "Enter") {
                    if (newEmail) {
                      event.preventDefault();
                      verifyNewTarget(normalized);
                    } else if (verifiedTargets[0]) {
                      event.preventDefault();
                      addVerifiedTarget(verifiedTargets[0]);
                    }
                  }
                }}
                placeholder="输入或搜索转发邮箱"
                role="combobox"
                aria-expanded={searchOpen}
                aria-controls="forwarding-target-options"
                aria-autocomplete="list"
              />
            </div>
            {searchOpen && (
              <div
                id="forwarding-target-options"
                className="forwarding-target-results"
                role="listbox"
                aria-label="转发邮箱搜索结果"
              >
                {verifiedTargets.length > 0 && (
                  <div className="forwarding-target-result-label">
                    已验证邮箱
                  </div>
                )}
                {verifiedTargets.slice(0, 5).map((email) => (
                  <button
                    type="button"
                    role="option"
                    aria-selected="false"
                    key={email}
                    onClick={() => addVerifiedTarget(email)}
                  >
                    <span>
                      <strong>{email}</strong>
                      <small>已验证 · 点击添加</small>
                    </span>
                    <Add20Regular />
                  </button>
                ))}
                {newEmail && (
                  <>
                    <div className="forwarding-target-result-label">
                      新邮箱地址
                    </div>
                    <button
                      type="button"
                      role="option"
                      aria-selected="false"
                      onClick={() => verifyNewTarget(normalized)}
                    >
                      <span>
                        <strong>验证并添加 {normalized}</strong>
                        <small>车友确认后自动启用转发</small>
                      </span>
                      <MailCheckmark20Regular />
                    </button>
                  </>
                )}
                {!verifiedTargets.length && !newEmail && (
                  <div className="forwarding-target-result-empty">
                    <strong>
                      {selectedVerifiedEmail
                        ? "该邮箱已在当前转发目标中"
                        : normalized
                          ? "没有匹配的已验证邮箱"
                          : "暂无可添加的已验证邮箱"}
                    </strong>
                    <small>
                      {normalized
                        ? "输入完整的新邮箱地址，可发送验证并自动绑定。"
                        : "可以直接输入车友邮箱地址发起验证。"}
                    </small>
                  </div>
                )}
              </div>
            )}
          </div>
        </section>
        <section className="forwarding-detail-section forwarding-options">
          <div className="forwarding-policy-row">
            <LockClosed20Regular />
            <span>
              <strong>保留原邮件副本</strong>
              <small>原邮件继续保存在 NewSzxcn 邮箱中。</small>
            </span>
            <Badge variant="secondary">始终启用</Badge>
          </div>
          <div className="forwarding-policy-row">
            <LockClosed20Regular />
            <span>
              <strong>转发失败处理</strong>
              <small>保留原邮件并记录失败原因。</small>
            </span>
            <Badge variant="secondary">系统策略</Badge>
          </div>
        </section>
      </div>
      <footer className="forwarding-detail-footer">
        <Button type="button" disabled={!changed || saving} onClick={onSave}>
          {saving ? "保存中" : changed ? "保存更改" : "已保存"}
        </Button>
      </footer>
    </article>
  );
}

function TargetRow({
  email,
  readOnly,
  onRemove,
}: {
  email: string;
  readOnly?: boolean;
  onRemove?: () => void;
}) {
  return (
    <div className="forwarding-target-row">
      <span>
        <MailCheckmark20Regular />
      </span>
      <div>
        <strong>{email}</strong>
        <small>{readOnly ? "全局规则 · 已启用" : "已验证 · 已启用"}</small>
      </div>
      {readOnly ? (
        <Badge variant="outline">
          <LockClosed20Regular />
          只读
        </Badge>
      ) : (
        <Button
          type="button"
          variant="ghost"
          size="icon"
          aria-label={`移除 ${email}`}
          onClick={onRemove}
        >
          <Delete20Regular />
        </Button>
      )}
    </div>
  );
}
function ScopeStatus({
  targets,
  inherited,
}: {
  targets: string[];
  inherited?: boolean;
}) {
  return (
    <span className="forwarding-scope-status">
      <small>
        {targets.length
          ? `${targets.length} 个目标`
          : inherited
            ? "继承"
            : "未启用"}
      </small>
      <span className={cn((targets.length || inherited) && "is-active")} />
    </span>
  );
}

function VerificationManagement({
  items,
  total,
  verifiedCount,
  pendingCount,
  latestVerifiedAt,
  search,
  onSearch,
  onAdd,
  onDelete,
  onResend,
  usageCount,
  busy,
  listRef,
  pageTransitioning,
  currentPage,
  totalPages,
  onPage,
}: {
  items: ForwardingVerifiedEmail[];
  total: number;
  verifiedCount: number;
  pendingCount: number;
  latestVerifiedAt?: string;
  search: string;
  onSearch: (value: string) => void;
  onAdd: () => void;
  onDelete: (item: ForwardingVerifiedEmail) => void;
  onResend: (item: ForwardingVerifiedEmail) => void;
  usageCount: (email: string) => number;
  busy: boolean;
  listRef: React.Ref<HTMLDivElement>;
  pageTransitioning: boolean;
  currentPage: number;
  totalPages: number;
  onPage: (page: number) => void;
}) {
  return (
    <div className="forwarding-verification-page">
      <div className="forwarding-stats">
        <StatCard
          icon={<CheckmarkCircle20Regular />}
          label="已验证"
          value={verifiedCount}
          tone="success"
        />
        <StatCard
          icon={<Clock20Regular />}
          label="待验证"
          value={pendingCount}
          tone="warning"
        />
        <StatCard
          icon={<MailCheckmark20Regular />}
          label="最近验证"
          value={
            latestVerifiedAt
              ? new Date(latestVerifiedAt).toLocaleDateString()
              : "暂无"
          }
          tone="accent"
        />
      </div>
      <article
        className={cn(
          "forwarding-glass-card forwarding-verification-card",
          total === 0 && "is-empty",
        )}
      >
        <div className="forwarding-card-heading forwarding-list-heading">
          <div>
            <h2>验证邮箱</h2>
            <p>管理所有用于接收邮件的外部邮箱。</p>
          </div>
          <div className="forwarding-verification-actions">
            {total >= 8 && (
              <div className="forwarding-search">
                <Search20Regular />
                <Input
                  type="search"
                  value={search}
                  onChange={(event) => onSearch(event.target.value)}
                  placeholder="搜索邮箱"
                  aria-label="搜索验证邮箱"
                  autoComplete="off"
                />
              </div>
            )}
            <Button type="button" onClick={onAdd}>
              <Add20Regular />
              添加验证邮箱
            </Button>
          </div>
        </div>
        <div
          ref={listRef}
          className={cn(
            "forwarding-verification-list",
            pageTransitioning && "is-transitioning",
          )}
        >
          {items.length ? (
            items.map((item) => (
              <div key={item.id} className="forwarding-email-row">
                <span
                  className={cn(
                    "forwarding-email-status",
                    item.verified ? "is-verified" : "is-pending",
                  )}
                >
                  {item.verified ? (
                    <CheckmarkCircle20Regular />
                  ) : (
                    <Clock20Regular />
                  )}
                </span>
                <div>
                  <strong>{item.email}</strong>
                  <p>
                    {item.verified
                      ? `验证于 ${formatDateTime(item.verifiedAt || item.createdAt)} · ${usageCount(item.email)} 条规则使用`
                      : item.deliveryStatus === "failed"
                        ? item.deliveryError || "验证邮件发送失败"
                        : `等待验证 · 发送于 ${formatDateTime(item.verificationSentAt || item.createdAt)}`}
                  </p>
                </div>
                <div className="forwarding-row-actions">
                  {!item.verified && (
                    <ResendButton
                      item={item}
                      pending={busy}
                      onResend={() => onResend(item)}
                    />
                  )}
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    disabled={busy}
                    aria-label={`删除 ${item.email}`}
                    onClick={() => onDelete(item)}
                  >
                    <Delete20Regular />
                  </Button>
                </div>
              </div>
            ))
          ) : (
            <div className="forwarding-verification-empty">
              <MailCheckmark20Regular />
              <strong>还没有验证邮箱</strong>
              <p>添加并验证后，才能作为邮件转发目标。</p>
              <Button type="button" onClick={onAdd}>
                <Add20Regular />
                添加验证邮箱
              </Button>
            </div>
          )}
        </div>
        {total > 0 && (
          <PaginationBar
            total={total}
            noun="验证邮箱"
            currentPage={currentPage}
            totalPages={totalPages}
            onPageChange={onPage}
          />
        )}
      </article>
    </div>
  );
}

function VerificationSheet({
  open,
  item,
  stage,
  email,
  note,
  error,
  sending,
  refreshing,
  resendPending,
  autoBind,
  bindingLabel,
  onEmail,
  onNote,
  onClose,
  onSend,
  onRefresh,
  onResend,
  onChangeAddress,
  onCancel,
  onStart,
}: {
  open: boolean;
  item: ForwardingVerifiedEmail | null;
  stage: "entry" | "sent" | "success";
  email: string;
  note: string;
  error: string;
  sending: boolean;
  refreshing: boolean;
  resendPending: boolean;
  autoBind: boolean;
  bindingLabel: string;
  onEmail: (value: string) => void;
  onNote: (value: string) => void;
  onClose: () => void;
  onSend: () => void;
  onRefresh: () => void;
  onResend: () => void;
  onChangeAddress: () => void;
  onCancel: () => void;
  onStart: () => void;
}) {
  return (
    <Sheet
      open={open}
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
    >
      <SheetContent
        side="right"
        className="forwarding-verification-sheet"
        overlayClassName="forwarding-drawer-overlay"
      >
        <SheetHeader>
          <SheetTitle>
            {stage === "entry"
              ? "添加转发邮箱"
              : stage === "success"
                ? "邮箱验证成功"
                : "验证邮件已发送"}
          </SheetTitle>
        </SheetHeader>
        {stage === "entry" ? (
          <form
            className="forwarding-verification-form"
            onSubmit={(event) => {
              event.preventDefault();
              if (!error && email.trim()) onSend();
            }}
          >
            <div>
              <Label htmlFor="forwarding-target-email">邮箱地址</Label>
              <Input
                id="forwarding-target-email"
                type="email"
                value={email}
                onChange={(event) => onEmail(event.target.value)}
                placeholder="name@example.com"
                autoComplete="off"
                spellCheck={false}
                autoFocus
              />
              <small className={cn(error && "is-error")}>
                {error || "验证邮件将发送到此地址，验证成功前不会执行转发。"}
              </small>
            </div>
            <div>
              <Label htmlFor="forwarding-target-note">备注名称（可选）</Label>
              <Input
                id="forwarding-target-note"
                value={note}
                onChange={(event) => onNote(event.target.value)}
                placeholder="例如：家人邮箱"
                autoComplete="off"
              />
            </div>
            <div className="forwarding-verification-binding-preview">
              <div>
                <small>车友邮箱</small>
                <strong>{email.trim() || "输入邮箱后显示"}</strong>
              </div>
              <div>
                <small>{autoBind ? "确认后自动绑定" : "验证用途"}</small>
                <strong>{bindingLabel}</strong>
              </div>
              <p>
                {autoBind
                  ? "系统将发送确认邮件。对方确认后，此转发目标会自动启用，无需再次搜索或保存。"
                  : "验证成功后，该地址会加入已验证邮箱列表。"}
              </p>
            </div>
            <SheetFooter>
              <Button type="button" variant="outline" onClick={onClose}>
                取消
              </Button>
              <Button
                type="submit"
                disabled={!!error || !email.trim() || sending}
              >
                {sending ? "发送中" : "发送验证邮件"}
              </Button>
            </SheetFooter>
          </form>
        ) : stage === "success" ? (
          <div className="forwarding-sheet-state is-success">
            <span>
              <CheckmarkCircle20Filled />
            </span>
            <h3>邮箱验证成功</h3>
            <p>
              {autoBind
                ? `${item?.email} 已验证，并已自动绑定到 ${bindingLabel}。`
                : `${item?.email} 已加入验证目标列表。`}
            </p>
            <Button type="button" onClick={onStart}>
              开始设置转发
            </Button>
          </div>
        ) : (
          <div className="forwarding-sheet-state">
            <span>
              <MailCheckmark20Regular />
            </span>
            <h3>验证邮件已发送</h3>
            <p>请前往 {item?.email || email}，打开验证邮件并点击“确认验证”。</p>
            {autoBind && (
              <div className="forwarding-verification-auto-bind">
                <small>确认后自动绑定</small>
                <strong>{bindingLabel}</strong>
                <p>无需再次搜索、勾选或保存。</p>
              </div>
            )}
            {item && (
              <dl>
                <div>
                  <dt>发送时间</dt>
                  <dd>
                    {formatDateTime(item.verificationSentAt || item.createdAt)}
                  </dd>
                </div>
                <div>
                  <dt>有效期</dt>
                  <dd>
                    {item.verificationExpiresAt
                      ? formatDateTime(item.verificationExpiresAt)
                      : "24 小时"}
                  </dd>
                </div>
                <div>
                  <dt>当前状态</dt>
                  <dd>等待验证</dd>
                </div>
              </dl>
            )}
            <div className="forwarding-sheet-actions">
              {item && (
                <ResendButton
                  item={item}
                  pending={resendPending}
                  onResend={onResend}
                />
              )}
              <Button type="button" variant="outline" onClick={onChangeAddress}>
                更换邮箱
              </Button>
              <Button type="button" variant="ghost" onClick={onCancel}>
                取消验证
              </Button>
            </div>
            <Button
              type="button"
              className="w-full"
              disabled={refreshing}
              onClick={onRefresh}
            >
              <ArrowClockwise20Regular
                className={cn(refreshing && "animate-spin")}
              />
              刷新状态
            </Button>
          </div>
        )}
      </SheetContent>
    </Sheet>
  );
}

function StatCard({
  icon,
  label,
  value,
  tone,
}: {
  icon: React.ReactNode;
  label: string;
  value: string | number;
  tone: "success" | "warning" | "accent";
}) {
  return (
    <div className={cn("forwarding-stat-card", `is-${tone}`)}>
      <span>{icon}</span>
      <div>
        <small>{label}</small>
        <strong>{value}</strong>
      </div>
    </div>
  );
}
function PaginationBar({
  total,
  noun,
  currentPage,
  totalPages,
  onPageChange,
}: {
  total: number;
  noun: string;
  currentPage: number;
  totalPages: number;
  onPageChange: (page: number) => void;
}) {
  return (
    <nav className="forwarding-pagination" aria-label={`${noun}分页`}>
      <span>
        共 {total} 个{noun} · 第 {currentPage} / {totalPages} 页
      </span>
      <div>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          aria-label="上一页"
          disabled={currentPage <= 1}
          onClick={() => onPageChange(currentPage - 1)}
        >
          <ChevronLeft20Regular />
        </Button>
        {Array.from({ length: totalPages }, (_, index) => index + 1).map(
          (page) => (
            <Button
              key={page}
              type="button"
              variant="ghost"
              size="icon"
              aria-label={`第 ${page} 页`}
              aria-current={page === currentPage ? "page" : undefined}
              className={cn(page === currentPage && "is-current")}
              onClick={() => onPageChange(page)}
            >
              {page}
            </Button>
          ),
        )}
        <Button
          type="button"
          variant="ghost"
          size="icon"
          aria-label="下一页"
          disabled={currentPage >= totalPages}
          onClick={() => onPageChange(currentPage + 1)}
        >
          <ChevronRight20Regular />
        </Button>
      </div>
    </nav>
  );
}
function ResendButton({
  item,
  pending,
  onResend,
}: {
  item: ForwardingVerifiedEmail;
  pending: boolean;
  onResend: () => void;
}) {
  const [now, setNow] = React.useState(Date.now());
  const sentAt = Date.parse(item.verificationSentAt || item.createdAt);
  const remaining = Math.max(0, Math.ceil((sentAt + 60_000 - now) / 1000));
  React.useEffect(() => {
    if (!remaining) return;
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [remaining]);
  return (
    <Button
      type="button"
      variant="outline"
      size="sm"
      disabled={pending || remaining > 0}
      onClick={onResend}
    >
      {remaining > 0 ? `${remaining}s 后重发` : "重新发送"}
    </Button>
  );
}
function ForwardingLoading() {
  return (
    <div className="forwarding-loading">
      <div className="forwarding-glass-card p-6">
        <Skeleton className="h-6 w-40" />
        <Skeleton className="mt-4 h-4 w-72 max-w-full" />
      </div>
      <div className="forwarding-glass-card p-6">
        <Skeleton className="h-full min-h-64 w-full" />
      </div>
    </div>
  );
}
function ErrorState({
  message,
  onRetry,
}: {
  message: string;
  onRetry: () => void;
}) {
  return (
    <div className="forwarding-glass-card forwarding-error-state">
      <Warning20Regular />
      <strong>无法加载转发设置</strong>
      <p>{message}</p>
      <Button type="button" variant="outline" onClick={onRetry}>
        重试
      </Button>
    </div>
  );
}
function forwardingTargets(settings?: ForwardingSettings) {
  return settings?.accountTargetEmails?.length
    ? settings.accountTargetEmails
    : settings?.accountTargetEmail
      ? [settings.accountTargetEmail]
      : [];
}
function looksLikeEmail(value: string) {
  return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value.trim());
}
