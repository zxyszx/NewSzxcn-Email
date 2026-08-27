import * as React from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import {
  Add20Regular,
  ArrowForward20Regular,
  ArrowClockwise20Regular,
  ArrowSort20Regular,
  CheckmarkCircle20Filled,
  CheckmarkCircle20Regular,
  ChevronLeft20Regular,
  ChevronRight20Regular,
  Clock20Regular,
  Delete20Regular,
  LockClosed20Regular,
  Mail20Regular,
  MailCheckmark20Regular,
  MoreHorizontal20Regular,
  QuestionCircle20Regular,
  Search20Regular,
  Warning20Regular,
} from "@fluentui/react-icons";
import {
  api,
  type ForwardingPendingBinding,
  type ForwardingMailboxSummary,
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
type BindingPurpose = {
  scope: "account" | "mailbox";
  mailboxId?: string;
};
type BindingStatus = "unbound" | "pending" | "bound" | "failed";
type VerificationView =
  | "all"
  | "pending"
  | "verified"
  | "failed"
  | "recent"
  | "global-forwarding"
  | "mailbox-forwarding";
type Props = {
  mailboxes: Mailbox[];
  tab: ForwardingTab;
  onTabChange: (tab: ForwardingTab) => void;
  showToolbar?: boolean;
};
const mailboxPageSize = 8;
const verificationPageSize = 8;

export function ForwardingWorkspace({
  mailboxes,
  tab,
  onTabChange,
  showToolbar = true,
}: Props) {
  const qc = useQueryClient();
  const { toast } = useToast();
  const [params, setParams] = useSearchParams();
  const [mailboxSearch, setMailboxSearch] = React.useState(
    params.get("q") || "",
  );
  const [mailboxSortAscending, setMailboxSortAscending] =
    React.useState(true);
  const [verificationSearch, setVerificationSearch] = React.useState(
    params.get("vq") || "",
  );
  const [verificationAddDraft, setVerificationAddDraft] = React.useState("");
  const [selectedScope, setSelectedScope] = React.useState("");
  const [targetSearch, setTargetSearch] = React.useState("");
  const [lookupPending, setLookupPending] = React.useState(false);
  const [addOpen, setAddOpen] = React.useState(params.get("drawer") === "add");
  const [helpOpen, setHelpOpen] = React.useState(false);
  const [verificationDraft, setVerificationDraft] = React.useState("");
  const [verificationNote, setVerificationNote] = React.useState("");
  const [verificationStage, setVerificationStage] = React.useState<
    "entry" | "sent" | "success"
  >("entry");
  const [verificationItem, setVerificationItem] =
    React.useState<ForwardingVerifiedEmail | null>(null);
  const verificationPurpose: VerificationPurpose = { scope: "verification" };
  const [pendingDelete, setPendingDelete] =
    React.useState<ForwardingVerifiedEmail | null>(null);
  const [pendingUnbind, setPendingUnbind] =
    React.useState<BindingPurpose | null>(null);
  const [pageTransitioning, setPageTransitioning] = React.useState(false);
  const mailboxListRef = React.useRef<HTMLDivElement>(null);
  const verificationListRef = React.useRef<HTMLDivElement>(null!);
  const verificationScrollTopRef = React.useRef(0);
  const pollingStartedAt = React.useRef(Date.now());

  React.useEffect(() => {
    if (tab !== "verification") return;
    setVerificationSearch(params.get("vq") || "");
  }, [params, tab]);
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
  const failedItems = React.useMemo(
    () => pendingItems.filter((item) => item.deliveryStatus === "failed"),
    [pendingItems],
  );
  const awaitingItems = React.useMemo(
    () => pendingItems.filter((item) => item.deliveryStatus !== "failed"),
    [pendingItems],
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
  const requestedMailbox = params.get("mailbox") || "";
  const selectedMailbox =
    selectedScope === "all"
      ? null
      : mailboxes.find((item) => item.id === selectedScope) || null;
  React.useEffect(() => {
    if (tab !== "settings") return;
    if (!requestedMailbox) {
      setSelectedScope("");
      return;
    }
    if (requestedMailbox === "all") {
      setSelectedScope("all");
      return;
    }
    const mailbox = mailboxes.find(
      (item) => item.address.toLowerCase() === requestedMailbox.toLowerCase(),
    );
    setSelectedScope(mailbox?.id || "");
  }, [mailboxes, requestedMailbox, tab]);
  React.useEffect(() => {
    if (tab !== "verification") return;
    requestAnimationFrame(() => {
      verificationListRef.current?.scrollTo({
        top: verificationScrollTopRef.current,
      });
    });
  }, [tab]);
  React.useEffect(() => {
    if (!verificationItem) return;
    const current = settings?.verifiedEmails.find(
      (item) => item.id === verificationItem.id,
    );
    if (!current) return;
    setVerificationItem(current);
    if (current.verified && verificationStage !== "success") {
      setVerificationStage("success");
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
  const addVerifiedInline = useMutation({
    mutationFn: (email: string) => api.addForwardingVerifiedEmail(email),
    onSuccess: (next, email) => {
      cache(next);
      setVerificationAddDraft("");
      const item = next.verifiedEmails.find(
        (entry) => entry.email.toLowerCase() === email.toLowerCase(),
      );
      toast({
        title:
          item?.deliveryStatus === "failed"
            ? "验证邮件发送失败"
            : item?.verified
              ? "邮箱已验证"
              : "验证邮件已发送",
        description:
          item?.deliveryError || "请检查目标邮箱的收件箱和垃圾邮件",
      });
    },
    onError: (error) =>
      toast({ title: "无法添加验证邮箱", description: error.message }),
  });
  const quickBind = useMutation({
    mutationFn: ({ email, purpose }: { email: string; purpose: BindingPurpose }) =>
      api.createForwardingPendingBinding({
        email,
        scope: purpose.scope,
        mailboxId: purpose.mailboxId,
      }),
    onSuccess: (next, { email, purpose }) => {
      cache(next);
      setTargetSearch("");
      const active = forwardingScopeTargets(next, purpose).some(
        (target) => target.toLowerCase() === email.toLowerCase(),
      );
      const verification = next.verifiedEmails.find(
        (item) => item.email.toLowerCase() === email.toLowerCase(),
      );
      toast({
        title:
          verification?.deliveryStatus === "failed"
            ? "验证邮件发送失败"
            : active
              ? "绑定成功"
              : "验证邮件已发送",
        description:
          verification?.deliveryError ||
          (active
            ? `${email} 已绑定到${purpose.scope === "account" ? "全部邮箱" : "当前邮箱"}`
            : "等待对方确认，验证成功后将自动完成绑定。"),
      });
    },
    onError: (error) =>
      toast({ title: "绑定失败", description: error.message }),
  });
  const unbindScope = useMutation({
    mutationFn: (purpose: BindingPurpose) =>
      purpose.scope === "account"
        ? api.updateAccountForwarding([])
        : api.updateMailboxForwarding(purpose.mailboxId || "", []),
    onSuccess: (next, purpose) => {
      cache(next);
      setPendingUnbind(null);
      toast({
        title:
          purpose.scope === "account"
            ? "已解除全部邮箱绑定"
            : accountTargets.length
              ? "已恢复全部邮箱设置"
              : "已解除单邮箱绑定",
      });
    },
    onError: (error) =>
      toast({ title: "解绑失败", description: error.message }),
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
  const filteredMailboxes = [
    ...new Map(
      mailboxes.map((item) => [item.address.toLowerCase(), item]),
    ).values(),
  ]
    .filter(
      (item) => {
        const targets = mailboxTargets.get(item.id) || [];
        return (
          !mailboxFilter ||
          item.address.toLowerCase().includes(mailboxFilter) ||
          targets.some((target) => target.toLowerCase().includes(mailboxFilter)) ||
          accountTargets.some((target) => target.toLowerCase().includes(mailboxFilter))
        );
      },
    )
    .sort((a, b) =>
      mailboxSortAscending
        ? a.address.localeCompare(b.address)
        : b.address.localeCompare(a.address),
    );
  const verificationFilter = verificationSearch.trim().toLowerCase();
  const filteredVerification = [...pendingItems, ...verifiedItems]
    .filter((item) => !verificationFilter || item.email.toLowerCase().includes(verificationFilter))
    .sort((a, b) => {
      const rank = (item: ForwardingVerifiedEmail) => item.verified ? 2 : item.deliveryStatus === "failed" ? 0 : 1;
      return rank(a) - rank(b) || a.email.localeCompare(b.email);
    });
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
  const normalizedTargetSearch = targetSearch.trim().toLowerCase();

  React.useEffect(() => {
    if (!looksLikeEmail(normalizedTargetSearch)) {
      setLookupPending(false);
      return;
    }
    let cancelled = false;
    const timer = window.setTimeout(async () => {
      setLookupPending(true);
      await forwarding.refetch();
      if (!cancelled) setLookupPending(false);
    }, 280);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [normalizedTargetSearch, forwarding.refetch]);

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
    setSelectedScope("");
    setTargetSearch("");
    window.setTimeout(() => {
      updateParams({ page: String(nextPage), mailbox: null, scope: null });
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
    setSelectedScope("");
    setTargetSearch("");
    updateParams(
      { page: "1", q: value || null, mailbox: null, scope: null },
      true,
    );
  }
  function updateVerificationSearch(value: string) {
    setVerificationSearch(value);
    updateParams({ page: "1", vq: value || null }, true);
  }
  function manageMailbox(mailboxAddress: string) {
    const mailbox = mailboxes.find(
      (item) => item.address.toLowerCase() === mailboxAddress.toLowerCase(),
    );
    if (!mailbox) return;
    verificationScrollTopRef.current =
      verificationListRef.current?.scrollTop || 0;
    const orderedMailboxes = [
      ...new Map(
        mailboxes.map((item) => [item.address.toLowerCase(), item]),
      ).values(),
    ].sort((a, b) => a.address.localeCompare(b.address));
    const mailboxIndex = orderedMailboxes.findIndex(
      (item) => item.id === mailbox.id,
    );
    const page = Math.floor(Math.max(0, mailboxIndex) / mailboxPageSize) + 1;
    setMailboxSearch("");
    setMailboxSortAscending(true);
    setSelectedScope(mailbox.id);
    setTargetSearch("");
    updateParams({
      tab: "settings",
      mailbox: mailbox.address,
      drawer: "manage",
      scope: null,
      q: null,
      page: String(page),
    });
    requestAnimationFrame(() => {
      document
        .querySelector(`[data-forwarding-mailbox-id="${mailbox.id}"]`)
        ?.scrollIntoView({ block: "nearest" });
    });
  }
  function manageGlobalForwarding() {
    verificationScrollTopRef.current =
      verificationListRef.current?.scrollTop || 0;
    setSelectedScope("all");
    setTargetSearch("");
    updateParams({
      tab: "settings",
      mailbox: "all",
      drawer: "manage",
      scope: null,
      q: null,
      page: "1",
    });
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
  function bindingsFor(purpose: BindingPurpose) {
    return (settings?.pendingBindings || []).filter(
      (item) =>
        item.scope === purpose.scope &&
        (purpose.scope === "account"
          ? !item.mailboxId
          : item.mailboxId === purpose.mailboxId),
    );
  }
  function pendingBindingsFor(purpose: BindingPurpose) {
    return bindingsFor(purpose).filter(
      (item) => item.status !== "cancelled" && item.status !== "active",
    );
  }
  function bindingStatusFor(
    purpose: BindingPurpose,
    targets: string[],
    inheritedTargets: string[] = [],
  ): BindingStatus {
    if (targets.length) return "bound";
    const pending = pendingBindingsFor(purpose)[0];
    if (pending?.status === "pending_verification") return "pending";
    if (
      pending?.status === "activation_failed" ||
      pending?.status === "expired"
    )
      return "failed";
    return inheritedTargets.length ? "bound" : "unbound";
  }

  const accountPurpose: BindingPurpose = { scope: "account" };
  const selectedPurpose: BindingPurpose | null = selectedScope
    ? selectedScope === "all"
      ? accountPurpose
      : { scope: "mailbox", mailboxId: selectedScope }
    : null;
  const selectedExplicitTargets = selectedPurpose
    ? selectedPurpose.scope === "account"
      ? accountTargets
      : mailboxTargets.get(selectedPurpose.mailboxId || "") || []
    : [];
  const selectedInheritedTargets =
    selectedPurpose?.scope === "mailbox" && !selectedExplicitTargets.length
      ? accountTargets
      : [];
  const selectedVerification = settings?.verifiedEmails.find(
    (item) => item.id === params.get("id"),
  );

  return (
    <section className="forwarding-workspace" aria-label="邮件转发">
      {showToolbar && (
        <ForwardingToolbar
          tab={tab}
          pending={awaitingItems.length}
          fetching={forwarding.isFetching}
          onTab={onTabChange}
          onHelp={() => setHelpOpen(true)}
          onRefresh={() => void forwarding.refetch()}
        />
      )}
      <div className="forwarding-tab-content">
        {forwarding.isError ? (
          <ErrorState
            message={forwarding.error.message}
            onRetry={() => void forwarding.refetch()}
          />
        ) : forwarding.isLoading ? (
          <ForwardingLoading />
        ) : tab === "verification" ? (
          <VerificationManagementDense
            items={pageVerification}
            total={filteredVerification.length}
            verifiedCount={verifiedItems.length}
            pendingCount={awaitingItems.length}
            failedCount={failedItems.length}
            latestVerifiedAt={verifiedItems.map((item) => item.verifiedAt).filter((value): value is string => !!value).sort((a, b) => Date.parse(b) - Date.parse(a))[0]}
            search={verificationSearch}
            onSearch={updateVerificationSearch}
            addDraft={verificationAddDraft}
            onAddDraft={setVerificationAddDraft}
            allItems={settings?.verifiedEmails || []}
            ownAddresses={ownAddresses}
            onAdd={(email) => addVerifiedInline.mutate(email)}
            onDelete={setPendingDelete}
            onResend={(item) => resendVerified.mutate(item)}
            onOpenDetail={(item) => updateParams({ id: item.id, drawer: "detail" })}
            busy={resendVerified.isPending || deleteVerified.isPending || addVerifiedInline.isPending}
            listRef={verificationListRef}
            pageTransitioning={pageTransitioning}
            currentPage={currentPage}
            totalPages={totalPages}
            onPage={setPage}
          />
        ) : (
          <ForwardingManagementList
            mailboxes={mailboxes}
            visibleMailboxes={pageMailboxes}
            total={filteredMailboxes.length}
            accountTargets={accountTargets}
            mailboxTargets={mailboxTargets}
            search={mailboxSearch}
            onSearch={updateMailboxSearch}
            onManageGlobal={manageGlobalForwarding}
            onManageMailbox={(mailbox) => manageMailbox(mailbox.address)}
            statusFor={(mailbox, targets, inheritedTargets) => bindingStatusFor({ scope: "mailbox", mailboxId: mailbox.id }, targets, inheritedTargets)}
            listRef={mailboxListRef}
            pageTransitioning={pageTransitioning}
            currentPage={currentPage}
            totalPages={totalPages}
            onPage={setPage}
          />
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
          setTargetSearch("");
          onTabChange("settings");
          closeVerification();
        }}
      />
      <Sheet
        open={params.get("drawer") === "manage" && !!selectedPurpose}
        onOpenChange={(open) => {
          if (!open) updateParams({ drawer: null });
        }}
      >
        <SheetContent side="right" className="forwarding-manage-sheet" overlayClassName="forwarding-manage-overlay" aria-describedby={undefined}>
          <SheetHeader className="forwarding-manage-header">
            <SheetTitle>{selectedMailbox?.address || "全部邮箱转发"}</SheetTitle>
            <p className="forwarding-manage-address">
              {selectedMailbox ? "管理此邮箱的独立转发目标与全局继承关系。" : "管理当前账号全部邮箱使用的全局转发目标。"}
            </p>
          </SheetHeader>
          {selectedPurpose && (
            <div className="forwarding-manage-body">
              <QuickBindPanel
                id="forwarding-drawer-binding-controls"
                label={selectedMailbox?.address || "全部邮箱"}
                scope={selectedPurpose.scope}
                search={targetSearch}
                targets={selectedExplicitTargets}
                inheritedTargets={selectedInheritedTargets}
                hasGlobalBinding={selectedPurpose.scope === "mailbox" && accountTargets.length > 0}
                verifiedEmails={verifiedEmails}
                pendingBindings={pendingBindingsFor(selectedPurpose)}
                ownAddresses={ownAddresses}
                busy={quickBind.isPending}
                querying={lookupPending}
                onSearch={setTargetSearch}
                onBind={(email) => quickBind.mutate({ email, purpose: selectedPurpose })}
                onUnbind={() => selectedPurpose.scope === "account" ? setPendingUnbind(selectedPurpose) : unbindScope.mutate(selectedPurpose)}
              />
            </div>
          )}
        </SheetContent>
      </Sheet>
      <VerificationDetailSheet
        item={params.get("drawer") === "detail" ? selectedVerification : undefined}
        usageCount={selectedVerification ? usageCount(selectedVerification.email) : 0}
        busy={resendVerified.isPending || deleteVerified.isPending}
        onClose={() => updateParams({ drawer: null, id: null })}
        onResend={(item) => resendVerified.mutate(item)}
        onDelete={setPendingDelete}
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
      <ConfirmDialog
        open={!!pendingUnbind}
        title="解除全部邮箱绑定？"
        description="解除后将停止账号级全局转发，但不会删除任何单邮箱的独立转发设置。"
        confirmText="解除绑定"
        destructive
        pending={unbindScope.isPending}
        onOpenChange={(open) => {
          if (!open) setPendingUnbind(null);
        }}
        onConfirm={() => pendingUnbind && unbindScope.mutate(pendingUnbind)}
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

export function ForwardingBindingPane({
  id,
  mailbox,
  status,
  explicitTargets,
  inheritedTargets,
  bindings,
  verifiedItems,
  compactPane,
  children,
}: {
  id: string;
  mailbox: Mailbox | null;
  status: BindingStatus;
  explicitTargets: string[];
  inheritedTargets: string[];
  bindings: ForwardingPendingBinding[];
  verifiedItems: ForwardingVerifiedEmail[];
  compactPane: "mailboxes" | "binding" | "summary";
  children: React.ReactNode;
}) {
  const targets = explicitTargets.length ? explicitTargets : inheritedTargets;
  const pending = bindings.find(
    (item) =>
      item.status === "pending_verification" ||
      item.status === "activation_failed" ||
      item.status === "expired",
  );
  const activeBinding = bindings.find((item) => item.status === "active");
  const verifiedAt = targets
    .map(
      (target) =>
        verifiedItems.find(
          (item) => item.email.toLowerCase() === target.toLowerCase(),
        )?.verifiedAt,
    )
    .filter((value): value is string => !!value)
    .sort((a, b) => Date.parse(b) - Date.parse(a))[0];

  return (
    <>
      <article
        id={id}
        className={cn(
          "mail-glass-list-surface forwarding-binding-pane",
          compactPane !== "binding" && "is-compact-hidden",
        )}
      >
        <header>
          <div>
            <small>当前范围</small>
            <h2>{mailbox?.address || "全部邮箱"}</h2>
          </div>
          <BindingStatusLabel status={status} />
        </header>
        <div className="forwarding-binding-actions">{children}</div>
      </article>
      <article
        className={cn(
          "mail-glass-reader-surface forwarding-binding-summary",
          compactPane !== "summary" && "is-compact-hidden",
        )}
      >
        <header>
          <div>
            <small>规则信息</small>
            <h2>绑定详情</h2>
          </div>
        </header>
        <dl>
          <div>
            <dt>当前邮箱</dt>
            <dd>{mailbox?.address || "全部邮箱"}</dd>
          </div>
          <div>
            <dt>转发目标</dt>
            <dd>{targets.length ? targets.join("、") : pending?.email || "尚未绑定"}</dd>
          </div>
          <div>
            <dt>来源</dt>
            <dd>
              {mailbox
                ? explicitTargets.length
                  ? "单邮箱独立规则"
                  : inheritedTargets.length
                    ? "全部邮箱规则"
                    : "未设置"
                : "全部邮箱规则"}
            </dd>
          </div>
          <div>
            <dt>最近验证</dt>
            <dd>{verifiedAt ? formatDateTime(verifiedAt) : "暂无记录"}</dd>
          </div>
          <div>
            <dt>最近绑定</dt>
            <dd>
              {activeBinding?.activatedAt
                ? formatDateTime(activeBinding.activatedAt)
                : "暂无记录"}
            </dd>
          </div>
        </dl>
      </article>
    </>
  );
}

export function ForwardingDetailPane({
  mailbox,
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
          <h2>{mailbox ? mailbox.address : "账号级转发"}</h2>
          <p>
            {mailbox
              ? "仅管理此邮箱的独立转发目标。账号级转发请在“全部邮箱”中设置。"
              : "此规则应用于当前账户下的全部 NewSzxcn 邮箱。"}
          </p>
        </div>
        <label>
          <span
            className={cn(
              "forwarding-detail-binding-status",
              targets.length ? "is-bound" : "is-unbound",
            )}
          >
            <i aria-hidden="true" />
            {targets.length ? "已绑定" : "未绑定"}
          </span>
          <Switch checked={targets.length > 0} onCheckedChange={onToggle} />
        </label>
      </header>
      <div className="forwarding-detail-scroll">
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
  onRemove,
}: {
  email: string;
  onRemove: () => void;
}) {
  return (
    <div className="forwarding-target-row">
      <span>
        <MailCheckmark20Regular />
      </span>
      <div>
        <strong>{email}</strong>
        <small>已验证 · 已启用</small>
      </div>
      <Button
        type="button"
        variant="ghost"
        size="icon"
        aria-label={`移除 ${email}`}
        onClick={onRemove}
      >
        <Delete20Regular />
      </Button>
    </div>
  );
}
export function ScopeStatus({
  status,
  expanded,
}: {
  status: BindingStatus;
  expanded: boolean;
}) {
  return (
    <span className="forwarding-scope-status">
      <BindingStatusLabel status={status} compact />
      <ChevronRight20Regular className={cn(expanded && "is-expanded")} />
    </span>
  );
}

function BindingStatusLabel({
  status,
  compact = false,
}: {
  status: BindingStatus;
  compact?: boolean;
}) {
  return (
    <span
      className={cn(
        "forwarding-binding-status",
        `is-${status}`,
        compact && "is-compact",
      )}
    >
      {status === "failed" ? (
        <Warning20Regular aria-hidden="true" />
      ) : (
        <i aria-hidden="true" />
      )}
      <small className="management-status-label">
        {status === "bound"
          ? "已绑定"
          : status === "pending"
            ? "等待验证"
            : status === "failed"
              ? "验证失败"
              : "未绑定"}
      </small>
    </span>
  );
}

function QuickBindPanel({
  id,
  label,
  scope,
  search,
  targets,
  inheritedTargets,
  hasGlobalBinding,
  verifiedEmails,
  pendingBindings,
  ownAddresses,
  busy,
  querying,
  onSearch,
  onBind,
  onUnbind,
}: {
  id: string;
  label: string;
  scope: "account" | "mailbox";
  search: string;
  targets: string[];
  inheritedTargets: string[];
  hasGlobalBinding: boolean;
  verifiedEmails: string[];
  pendingBindings: ForwardingPendingBinding[];
  ownAddresses: Set<string>;
  busy: boolean;
  querying: boolean;
  onSearch: (value: string) => void;
  onBind: (email: string) => void;
  onUnbind: () => void;
}) {
  const searchRef = React.useRef<HTMLInputElement>(null);
  const normalized = search.trim().toLowerCase();
  const boundAddresses = new Set(targets.map((email) => email.toLowerCase()));
  const exactVerified = verifiedEmails.find(
    (email) => email.toLowerCase() === normalized,
  );
  const alreadyBound = Boolean(normalized && boundAddresses.has(normalized));
  const ownAddress = Boolean(normalized && ownAddresses.has(normalized));
  const validNewAddress =
    looksLikeEmail(normalized) && !exactVerified && !alreadyBound && !ownAddress;
  const pending =
    pendingBindings.find((item) => item.email.toLowerCase() === normalized) ||
    (!normalized ? pendingBindings[0] : undefined);
  const pendingMatchesSearch = Boolean(
    pending && pending.email.toLowerCase() === normalized,
  );
  const currentTargets = targets.length ? targets : inheritedTargets;

  function bindBestMatch() {
    if (alreadyBound || ownAddress) return;
    if (exactVerified) onBind(exactVerified);
    else if (validNewAddress) onBind(normalized);
  }

  return (
    <div id={id} className="forwarding-quick-bind">
      <div className="forwarding-quick-bind-heading">
        <span>绑定转发邮箱</span>
      </div>
      {currentTargets.length > 0 && (
        <div className="forwarding-quick-bind-current">
          <div>
            <small>
              {inheritedTargets.length
                ? "当前继承"
                : scope === "account"
                  ? "全局转发目标"
                  : "独立转发目标"}
            </small>
            <strong>{currentTargets.join("、")}</strong>
            {scope === "account" && <p>应用范围：当前账号下全部邮箱</p>}
            {inheritedTargets.length > 0 && <p>继承全部邮箱设置</p>}
          </div>
          <div>
            {inheritedTargets.length > 0 ? (
              <>
                <Button type="button" size="sm" variant="outline" disabled>
                  使用全局设置
                </Button>
                <Button
                  type="button"
                  size="sm"
                  onClick={() => searchRef.current?.focus()}
                >
                  设置独立转发
                </Button>
              </>
            ) : (
              <>
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  onClick={() => searchRef.current?.focus()}
                >
                  更换目标
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  disabled={busy}
                  onClick={onUnbind}
                >
                  {scope === "account"
                    ? "解除全局绑定"
                    : hasGlobalBinding
                      ? "恢复全局设置"
                      : "解除绑定"}
                </Button>
              </>
            )}
          </div>
        </div>
      )}
      {pending && (
        <div
          className={cn(
            "forwarding-quick-bind-pending",
            (pending.status === "activation_failed" ||
              pending.status === "expired") &&
              "is-error",
          )}
        >
          {pending.status === "pending_verification" ? (
            <Clock20Regular />
          ) : (
            <Warning20Regular />
          )}
          <span>
            <strong>{pending.email}</strong>
            <small>
              {pending.status === "expired"
                ? "验证链接已过期，请重新发送验证邮件"
                : pending.status === "activation_failed"
                  ? pending.failureReason || "验证完成，但自动绑定失败"
                  : `验证邮件已发送至 ${pending.email}，确认后将自动绑定`}
            </small>
          </span>
          <div>
            <Button
              type="button"
              size="sm"
              variant="outline"
              disabled={busy}
              onClick={() => onBind(pending.email)}
            >
              重新发送验证邮件
            </Button>
            <Button
              type="button"
              size="sm"
              variant="ghost"
              onClick={() => {
                onSearch("");
                searchRef.current?.focus();
              }}
            >
              更换邮箱
            </Button>
          </div>
        </div>
      )}
      <div className="forwarding-quick-bind-search">
        <Search20Regular />
        <Input
          ref={searchRef}
          value={search}
          placeholder="输入要绑定的邮箱账号"
          aria-label={`为${label}搜索转发邮箱`}
          onChange={(event) => onSearch(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              event.preventDefault();
              bindBestMatch();
            }
          }}
        />
      </div>
      <div className="forwarding-quick-bind-results">
        {querying && looksLikeEmail(normalized) && (
          <div className="forwarding-quick-bind-message is-querying">
            <ArrowClockwise20Regular className="animate-spin" />
            正在查询验证状态…
          </div>
        )}
        {!querying && exactVerified && !alreadyBound && (
          <div className="forwarding-quick-bind-result is-verified">
            <span>
              <strong>{exactVerified}</strong>
              <small>该邮箱已完成验证，可以直接绑定</small>
            </span>
            <Button
              type="button"
              size="sm"
              disabled={busy}
              onClick={() => onBind(exactVerified)}
            >
              立即绑定
            </Button>
          </div>
        )}
        {!querying && validNewAddress && (
          <div className="forwarding-quick-bind-result is-unverified">
            <span>
              <strong>{normalized}</strong>
              <small>该邮箱尚未验证，绑定前需要完成邮箱验证</small>
            </span>
            <Button
              type="button"
              size="sm"
              disabled={busy}
              onClick={() => onBind(normalized)}
            >
              {busy
                ? "处理中"
                : pendingMatchesSearch
                  ? "重新发送验证邮件"
                  : "发送验证并绑定"}
            </Button>
          </div>
        )}
        {alreadyBound && (
          <div className="forwarding-quick-bind-message is-success">
            <CheckmarkCircle20Regular />
            该邮箱已绑定到当前范围
          </div>
        )}
        {ownAddress && (
          <div className="forwarding-quick-bind-message is-error">
            <Warning20Regular />
            不能绑定当前账户自己的邮箱
          </div>
        )}
        {normalized &&
          !looksLikeEmail(normalized) &&
          !querying && (
            <div className="forwarding-quick-bind-message">
              继续输入完整邮箱地址以查询验证状态
            </div>
          )}
        {!normalized && !pending && (
          <div className="forwarding-quick-bind-message">
            输入邮箱账号，系统会先查询是否已经验证
          </div>
        )}
      </div>
    </div>
  );
}

function ForwardingManagementList({
  mailboxes,
  visibleMailboxes,
  total,
  accountTargets,
  mailboxTargets,
  search,
  onSearch,
  onManageGlobal,
  onManageMailbox,
  statusFor,
  listRef,
  pageTransitioning,
  currentPage,
  totalPages,
  onPage,
}: {
  mailboxes: Mailbox[];
  visibleMailboxes: Mailbox[];
  total: number;
  accountTargets: string[];
  mailboxTargets: Map<string, string[]>;
  search: string;
  onSearch: (value: string) => void;
  onManageGlobal: () => void;
  onManageMailbox: (mailbox: Mailbox) => void;
  statusFor: (mailbox: Mailbox, targets: string[], inheritedTargets: string[]) => BindingStatus;
  listRef: React.Ref<HTMLDivElement>;
  pageTransitioning: boolean;
  currentPage: number;
  totalPages: number;
  onPage: (page: number) => void;
}) {
  const independentRuleCount = mailboxes.filter((mailbox) => (mailboxTargets.get(mailbox.id) || []).length > 0).length;
  const enabledCount = mailboxes.filter((mailbox) => accountTargets.length > 0 || (mailboxTargets.get(mailbox.id) || []).length > 0).length;
  const unconfiguredCount = Math.max(0, mailboxes.length - enabledCount);
  return (
    <div className="management-list-page forwarding-dense-page">
      <div className="management-stat-strip" aria-label="邮件转发概览">
        <div className="management-summary" aria-label="邮件转发统计">
          <div className="management-stat"><span className="management-stat-icon is-purple"><ArrowForward20Regular /></span><span>启用转发<strong>{enabledCount}</strong></span></div><i aria-hidden="true" />
          <div className="management-stat"><span className="management-stat-icon is-blue"><Mail20Regular /></span><span>全部邮箱规则<strong>{accountTargets.length ? 1 : 0}</strong></span></div><i aria-hidden="true" />
          <div className="management-stat"><span className="management-stat-icon is-green"><MailCheckmark20Regular /></span><span>单邮箱规则<strong>{independentRuleCount}</strong></span></div><i aria-hidden="true" />
          <div className="management-stat"><span className="management-stat-icon is-orange"><Clock20Regular /></span><span>未设置<strong>{unconfiguredCount}</strong></span></div>
        </div>
        <div className="management-primary-action">
          <strong>全部邮箱转发</strong>
          <div>
            <span className={cn("management-status-label", accountTargets.length ? "is-active" : "is-muted")}>{accountTargets.length ? accountTargets[0] : "尚未设置"}</span>
            <Button type="button" onClick={onManageGlobal}>{accountTargets.length ? "管理" : "设置全部邮箱转发"}</Button>
          </div>
        </div>
      </div>
      <section className="management-list-panel" aria-labelledby="forwarding-management-title">
        <header className="management-list-title"><h2 id="forwarding-management-title">邮件转发管理</h2><label className="management-search"><Search20Regular /><Input value={search} onChange={(event) => onSearch(event.target.value)} placeholder="搜索本地邮箱或转发目标" type="search" aria-label="搜索本地邮箱或转发目标" /></label></header>
        <div ref={listRef} className={cn("management-list-body", pageTransitioning && "is-transitioning")}>
          <article className="forwarding-dense-global-row">
            <span className="management-row-icon is-purple"><ArrowForward20Regular /></span>
            <div><strong>全部邮箱转发</strong></div>
            <div><strong>{accountTargets[0] || "尚未设置转发目标"}</strong><small>自动应用于全部本地邮箱和未来新建邮箱</small></div>
            <BindingStatusLabel status={accountTargets.length ? "bound" : "unbound"} />
            <Button type="button" variant="outline" onClick={onManageGlobal}>管理</Button>
            <Button type="button" variant="ghost" size="icon" aria-label="全部邮箱转发更多操作" onClick={onManageGlobal}><MoreHorizontal20Regular /></Button>
          </article>
          <div className="management-group-heading is-success"><strong>单邮箱转发</strong><span>{independentRuleCount}</span></div>
          {visibleMailboxes.length ? visibleMailboxes.map((mailbox) => {
            const targets = mailboxTargets.get(mailbox.id) || [];
            const inheritedTargets = targets.length ? [] : accountTargets;
            const status = statusFor(mailbox, targets, inheritedTargets);
            return (
              <article className="forwarding-dense-row" key={mailbox.id}>
                <span className={cn("management-row-icon", status === "bound" ? "is-green" : "is-orange")}><Mail20Regular /></span>
                <div><strong>{mailbox.address}</strong></div>
                <div>
                  <strong>{targets.length ? `转发至 ${targets[0]}${targets.length > 1 ? ` · 另有 ${targets.length - 1} 个目标` : ""}` : accountTargets.length ? "仅使用全部邮箱规则" : "未设置独立转发目标"}</strong>
                </div>
                <span className="forwarding-inheritance">{accountTargets.length ? "继承全部邮箱规则" : "无全局规则"}</span>
                <BindingStatusLabel status={status} />
                <Button type="button" variant="outline" onClick={() => onManageMailbox(mailbox)}>管理</Button>
                <Button type="button" variant="ghost" size="icon" aria-label={`${mailbox.address} 更多操作`} onClick={() => onManageMailbox(mailbox)}><MoreHorizontal20Regular /></Button>
              </article>
            );
          }) : <div className="management-compact-empty">没有匹配的邮箱或转发目标</div>}
        </div>
        <PaginationBar total={total} noun="邮箱" currentPage={currentPage} totalPages={totalPages} onPageChange={onPage} />
      </section>
    </div>
  );
}

function VerificationManagementDense({
  items,
  total,
  verifiedCount,
  pendingCount,
  failedCount,
  latestVerifiedAt,
  search,
  onSearch,
  addDraft,
  onAddDraft,
  allItems,
  ownAddresses,
  onAdd,
  onDelete,
  onResend,
  onOpenDetail,
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
  failedCount: number;
  latestVerifiedAt?: string;
  search: string;
  onSearch: (value: string) => void;
  addDraft: string;
  onAddDraft: (value: string) => void;
  allItems: ForwardingVerifiedEmail[];
  ownAddresses: Set<string>;
  onAdd: (email: string) => void;
  onDelete: (item: ForwardingVerifiedEmail) => void;
  onResend: (item: ForwardingVerifiedEmail) => void;
  onOpenDetail: (item: ForwardingVerifiedEmail) => void;
  busy: boolean;
  listRef: React.Ref<HTMLDivElement>;
  pageTransitioning: boolean;
  currentPage: number;
  totalPages: number;
  onPage: (page: number) => void;
}) {
  const failed = items.filter((item) => !item.verified && item.deliveryStatus === "failed");
  const pending = items.filter((item) => !item.verified && item.deliveryStatus !== "failed");
  const verified = items.filter((item) => item.verified);
  const normalizedDraft = addDraft.trim().toLowerCase();
  const existing = allItems.find((item) => item.email.toLowerCase() === normalizedDraft);
  const isOwnAddress = ownAddresses.has(normalizedDraft);
  const canAdd = looksLikeEmail(normalizedDraft) && !existing && !isOwnAddress;
  return (
    <div className="management-list-page verification-dense-page">
      <div className="management-stat-strip" aria-label="验证邮箱概览">
        <div className="management-summary" aria-label="验证邮箱统计">
          <div className="management-stat"><span className="management-stat-icon is-green"><MailCheckmark20Regular /></span><span>已验证<strong>{verifiedCount}</strong></span></div><i aria-hidden="true" />
          <div className="management-stat"><span className="management-stat-icon is-orange"><Clock20Regular /></span><span>待验证<strong>{pendingCount}</strong></span></div><i aria-hidden="true" />
          <div className="management-stat"><span className="management-stat-icon is-red"><Warning20Regular /></span><span>验证失败<strong>{failedCount}</strong></span></div><i aria-hidden="true" />
          <div className="management-stat"><span className="management-stat-icon is-purple"><CheckmarkCircle20Regular /></span><span>最近验证<strong>{latestVerifiedAt ? new Date(latestVerifiedAt).toLocaleDateString() : "暂无"}</strong></span></div>
        </div>
        <div className="verification-inline-add">
          <strong>添加验证邮箱</strong>
          <div>
            <label className="management-search"><MailCheckmark20Regular /><Input value={addDraft} onChange={(event) => onAddDraft(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter" && canAdd) { event.preventDefault(); onAdd(normalizedDraft); } }} placeholder="输入验证邮箱账号" type="email" aria-label="输入验证邮箱账号" /></label>
            {existing && <span className={cn("verification-inline-status", existing.verified ? "is-verified" : existing.deliveryStatus === "failed" ? "is-failed" : "is-pending")}>{existing.verified ? "已验证" : existing.deliveryStatus === "failed" ? "已添加 · 发送失败" : "已添加 · 待验证"}</span>}
            {isOwnAddress && <span className="verification-inline-status is-failed">不能添加自己的邮箱</span>}
            {!existing && !isOwnAddress && <Button type="button" disabled={!canAdd || busy} onClick={() => onAdd(normalizedDraft)}>{busy ? "添加中…" : "添加验证邮箱"}</Button>}
          </div>
        </div>
      </div>
      <section className="management-list-panel" aria-labelledby="verification-management-title">
        <header className="management-list-title"><h2 id="verification-management-title">验证邮箱管理</h2><label className="management-search"><Search20Regular /><Input value={search} onChange={(event) => onSearch(event.target.value)} placeholder="搜索验证邮箱" type="search" aria-label="搜索验证邮箱" /></label></header>
        <div ref={listRef} className={cn("management-list-body verification-grouped-list", pageTransitioning && "is-transitioning")}>
          <VerificationGroupHeading tone="error" label="验证失败" count={failedCount} />
          {failed.length ? failed.map((item) => <VerificationDenseRow key={item.id} item={item} busy={busy} onOpenDetail={onOpenDetail} onResend={onResend} onDelete={onDelete} />) : <div className="management-compact-empty">暂无验证失败的邮箱</div>}
          <VerificationGroupHeading tone="pending" label="待验证" count={pendingCount} />
          {pending.length ? pending.map((item) => <VerificationDenseRow key={item.id} item={item} busy={busy} onOpenDetail={onOpenDetail} onResend={onResend} onDelete={onDelete} />) : <div className="management-compact-empty">暂无等待确认的验证邮箱</div>}
          <VerificationGroupHeading tone="success" label="已验证" count={verifiedCount} />
          {verified.length ? verified.map((item) => <VerificationDenseRow key={item.id} item={item} busy={busy} onOpenDetail={onOpenDetail} onResend={onResend} onDelete={onDelete} />) : <div className="management-compact-empty">暂无已验证邮箱</div>}
        </div>
        <PaginationBar total={total} noun="验证邮箱" currentPage={currentPage} totalPages={totalPages} onPageChange={onPage} />
      </section>
    </div>
  );
}

function VerificationGroupHeading({ tone, label, count }: { tone: "error" | "pending" | "success"; label: string; count: number }) {
  return <div className={cn("management-group-heading", `is-${tone}`)}><strong>{label}</strong><span>{count}</span></div>;
}

function VerificationDenseRow({ item, busy, onOpenDetail, onResend, onDelete }: {
  item: ForwardingVerifiedEmail;
  busy: boolean;
  onOpenDetail: (item: ForwardingVerifiedEmail) => void;
  onResend: (item: ForwardingVerifiedEmail) => void;
  onDelete: (item: ForwardingVerifiedEmail) => void;
}) {
  const failed = !item.verified && item.deliveryStatus === "failed";
  return (
    <article className={cn("verification-dense-row", item.verified ? "is-verified" : failed ? "is-failed" : "is-pending")}>
      <span className="verification-dense-icon">{item.verified ? <CheckmarkCircle20Regular /> : failed ? <Warning20Regular /> : <Clock20Regular />}</span>
      <Button type="button" variant="ghost" className="verification-dense-email" onClick={() => onOpenDetail(item)}>{item.email}</Button>
      <span className="verification-dense-message"><span className={cn("management-status-label", item.verified ? "is-active" : failed ? "is-error" : "is-warning")}>{item.verified ? "已验证" : failed ? "验证失败" : "待验证"}</span><small>{item.verified ? `验证于 ${formatDateTime(item.verifiedAt || item.createdAt)}` : failed ? item.deliveryError || "验证邮件发送失败" : "等待用户点击验证链接"}</small></span>
      <span className="verification-dense-time">{item.verified ? "" : `最近尝试 ${formatDateTime(item.verificationSentAt || item.createdAt)}`}</span>
      {!item.verified && <Button type="button" variant="outline" disabled={busy} onClick={() => onResend(item)}><ArrowClockwise20Regular />重新发送</Button>}
      <Button type="button" variant="outline" className="is-destructive" disabled={busy} onClick={() => onDelete(item)}><Delete20Regular />删除</Button>
    </article>
  );
}

function VerificationDetailSheet({ item, usageCount, busy, onClose, onResend, onDelete }: {
  item?: ForwardingVerifiedEmail;
  usageCount: number;
  busy: boolean;
  onClose: () => void;
  onResend: (item: ForwardingVerifiedEmail) => void;
  onDelete: (item: ForwardingVerifiedEmail) => void;
}) {
  return (
    <Sheet open={!!item} onOpenChange={(open) => { if (!open) onClose(); }}>
      <SheetContent side="right" className="verification-detail-sheet" overlayClassName="forwarding-manage-overlay" aria-describedby={undefined}>
        <SheetHeader className="forwarding-manage-header"><SheetTitle>{item?.email || "验证邮箱详情"}</SheetTitle></SheetHeader>
        {item && <div className="verification-detail-sheet-body">
          <span className={cn("forwarding-verification-detail-status", item.verified ? "is-verified" : item.deliveryStatus === "failed" ? "is-failed" : "is-pending")}>
            {item.verified ? <CheckmarkCircle20Regular /> : item.deliveryStatus === "failed" ? <Warning20Regular /> : <Clock20Regular />}
            {item.verified ? "已验证" : item.deliveryStatus === "failed" ? "发送失败" : "等待验证"}
          </span>
          <dl className="forwarding-verification-detail-fields">
            <div><dt>验证状态</dt><dd>{item.verified ? "已完成" : "未完成"}</dd></div>
            <div><dt>最近发送</dt><dd>{item.verificationSentAt ? formatDateTime(item.verificationSentAt) : "暂无"}</dd></div>
            <div><dt>关联规则</dt><dd>{usageCount ? `${usageCount} 条` : "尚未使用"}</dd></div>
            <div><dt>创建时间</dt><dd>{formatDateTime(item.createdAt)}</dd></div>
            <div><dt>验证过期</dt><dd>{item.verificationExpiresAt ? formatDateTime(item.verificationExpiresAt) : "暂无"}</dd></div>
          </dl>
          {item.deliveryError && <p className="verification-detail-error">{item.deliveryError}</p>}
          <div className="forwarding-verification-detail-actions">
            {!item.verified && <Button type="button" disabled={busy} onClick={() => onResend(item)}><ArrowClockwise20Regular />重新发送验证邮件</Button>}
            <Button type="button" variant="outline" disabled={busy} onClick={() => onDelete(item)}><Delete20Regular />删除</Button>
          </div>
        </div>}
      </SheetContent>
    </Sheet>
  );
}

export function VerificationManagement({
  items,
  allItems,
  total,
  verifiedCount,
  pendingCount,
  failedCount,
  search,
  onSearch,
  statusFilter,
  urlHasView,
  onStatusFilter,
  selectedItem,
  selectedMailbox,
  accountTargets,
  onSelectItem,
  onSelectMailbox,
  onAdd,
  onDelete,
  onResend,
  summaries,
  summaryTotal,
  summarySearch,
  summarySortAscending,
  summaryPage,
  summaryTotalPages,
  onSummarySearch,
  onSummarySort,
  onSummaryPage,
  onManageMailbox,
  onManageGlobal,
  busy,
  listRef,
  pageTransitioning,
  currentPage,
  totalPages,
  onPage,
}: {
  items: ForwardingVerifiedEmail[];
  allItems: ForwardingVerifiedEmail[];
  total: number;
  verifiedCount: number;
  pendingCount: number;
  failedCount: number;
  search: string;
  onSearch: (value: string) => void;
  statusFilter: VerificationView;
  urlHasView: boolean;
  onStatusFilter: (value: VerificationView) => void;
  selectedItem?: ForwardingVerifiedEmail;
  selectedMailbox?: ForwardingMailboxSummary;
  accountTargets: string[];
  onSelectItem: (item: ForwardingVerifiedEmail) => void;
  onSelectMailbox: (mailbox: ForwardingMailboxSummary) => void;
  onAdd: () => void;
  onDelete: (item: ForwardingVerifiedEmail) => void;
  onResend: (item: ForwardingVerifiedEmail) => void;
  summaries: ForwardingMailboxSummary[];
  summaryTotal: number;
  summarySearch: string;
  summarySortAscending: boolean;
  summaryPage: number;
  summaryTotalPages: number;
  onSummarySearch: (value: string) => void;
  onSummarySort: () => void;
  onSummaryPage: (page: number) => void;
  onManageMailbox: (mailboxAddress: string) => void;
  onManageGlobal: () => void;
  busy: boolean;
  listRef: React.Ref<HTMLDivElement>;
  pageTransitioning: boolean;
  currentPage: number;
  totalPages: number;
  onPage: (page: number) => void;
}) {
  const [expandedItem, setExpandedItem] = React.useState("");
  const [mobilePane, setMobilePane] = React.useState<
    "categories" | "list" | "detail"
  >("categories");
  const isVerificationView = ![
    "global-forwarding",
    "mailbox-forwarding",
  ].includes(statusFilter);
  const viewTitle: Record<VerificationView, string> = {
    all: "全部邮箱",
    pending: "待验证",
    verified: "已验证",
    failed: "验证失败",
    recent: "最近验证",
    "global-forwarding": "全部邮箱转发",
    "mailbox-forwarding": "单邮箱转发",
  };
  React.useEffect(() => {
    if (window.matchMedia("(min-width: 800px)").matches) return;
    if (selectedItem || selectedMailbox) {
      setMobilePane("detail");
      return;
    }
    setMobilePane(urlHasView ? "list" : "categories");
  }, [selectedItem?.id, selectedMailbox?.mailboxId, urlHasView]);
  return (
    <div className="forwarding-verification-page" data-mobile-pane={mobilePane}>
      <nav className="mail-glass-folder-surface forwarding-verification-categories" aria-label="邮箱验证分类">
        <header>
          <h2>邮箱验证</h2>
        </header>
        <div className="forwarding-verification-category-scroll">
          <section>
            <h3>验证状态</h3>
            {([
              ["all", "全部邮箱", allItems.length, <Mail20Regular />],
              ["pending", "待验证", pendingCount, <Clock20Regular />],
              ["verified", "已验证", verifiedCount, <CheckmarkCircle20Regular />],
              ["failed", "验证失败", failedCount, <Warning20Regular />],
              ["recent", "最近验证", undefined, <MailCheckmark20Regular />],
            ] as const).map(([value, label, count, icon]) => (
              <button
                key={value}
                type="button"
                className={cn("forwarding-verification-category", statusFilter === value && "is-selected")}
                aria-current={statusFilter === value ? "page" : undefined}
                onClick={() => {
                  onStatusFilter(value);
                  setMobilePane("list");
                }}
              >
                {icon}
                <span>{label}</span>
                {count !== undefined && <strong>{count}</strong>}
              </button>
            ))}
          </section>
          <section>
            <h3>转发规则</h3>
            {([
              ["global-forwarding", "全部邮箱转发", accountTargets.length, <ArrowForward20Regular />],
              ["mailbox-forwarding", "单邮箱转发", summaryTotal, <Mail20Regular />],
            ] as const).map(([value, label, count, icon]) => (
              <button
                key={value}
                type="button"
                className={cn("forwarding-verification-category", statusFilter === value && "is-selected")}
                aria-current={statusFilter === value ? "page" : undefined}
                onClick={() => {
                  onStatusFilter(value);
                  setMobilePane(value === "global-forwarding" ? "detail" : "list");
                }}
              >
                {icon}
                <span>{label}</span>
                <strong>{count}</strong>
              </button>
            ))}
          </section>
        </div>
      </nav>
        <article className={cn("mail-glass-list-surface forwarding-verification-card", total === 0 && "is-empty")}>
          <div className="forwarding-card-heading forwarding-list-heading">
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="forwarding-verification-mobile-back"
              aria-label="返回邮箱验证分类"
              onClick={() => setMobilePane("categories")}
            >
              <ChevronLeft20Regular />
            </Button>
            <h2>{viewTitle[statusFilter]}</h2>
            <div className="forwarding-verification-actions">
              {statusFilter !== "global-forwarding" && (
                <div className="forwarding-search">
                  <Search20Regular />
                  <Input
                    value={statusFilter === "mailbox-forwarding" ? summarySearch : search}
                    onChange={(event) =>
                      statusFilter === "mailbox-forwarding"
                        ? onSummarySearch(event.target.value)
                        : onSearch(event.target.value)
                    }
                    placeholder={statusFilter === "mailbox-forwarding" ? "搜索邮箱或转发目标" : "搜索邮箱"}
                    aria-label={statusFilter === "mailbox-forwarding" ? "搜索单邮箱转发规则" : "搜索验证邮箱"}
                  />
                </div>
              )}
              <Button
                type="button"
                variant="ghost"
                size="icon"
                aria-label={summarySortAscending ? "切换为 Z 到 A" : "切换为 A 到 Z"}
                onClick={onSummarySort}
              >
                <ArrowSort20Regular />
              </Button>
              {isVerificationView && (
                <Button type="button" className="forwarding-verification-add" onClick={onAdd}>
                  <Add20Regular />
                  添加验证邮箱
                </Button>
              )}
            </div>
          </div>
          <div
            ref={listRef}
            className={cn(
              "forwarding-verification-list",
              pageTransitioning && "is-transitioning",
            )}
          >
            {statusFilter === "global-forwarding" ? (
              <button
                type="button"
                className="forwarding-email-row is-selected"
                onClick={() => setMobilePane("detail")}
              >
                <span className="forwarding-email-status is-verified">
                  <ArrowForward20Regular />
                </span>
                <div>
                  <strong>全部邮箱转发</strong>
                  <p>{accountTargets.length ? `已绑定 ${accountTargets.length} 个目标` : "尚未绑定转发目标"}</p>
                </div>
                <ChevronRight20Regular />
              </button>
            ) : statusFilter === "mailbox-forwarding" ? (
              summaries.length ? (
                summaries.map((summary) => (
                  <button
                    key={summary.mailboxId}
                    type="button"
                    className={cn(
                      "forwarding-email-row forwarding-rule-list-row",
                      selectedMailbox?.mailboxId === summary.mailboxId && "is-selected",
                    )}
                    onClick={() => {
                      onSelectMailbox(summary);
                      setMobilePane("detail");
                    }}
                  >
                    <span className="forwarding-email-status is-verified">
                      <Mail20Regular />
                    </span>
                    <div>
                      <strong>{summary.mailboxAddress}</strong>
                      <p>
                        {summary.targets[0]?.email
                          ? `转发至 ${summary.targets[0].email}${summary.independentTargets > 1 ? ` · 另有 ${summary.independentTargets - 1} 个目标` : ""}`
                          : "尚未设置独立目标"}
                      </p>
                    </div>
                    <ChevronRight20Regular />
                  </button>
                ))
              ) : (
                <div className="forwarding-verification-empty">
                  <Mail20Regular />
                  <strong>暂无单邮箱独立转发规则</strong>
                </div>
              )
            ) : items.length ? (
              items.map((item) => {
                const mailboxBindings = item.mailboxBindings || [];
                const associationCount =
                  mailboxBindings.length + (item.globalBinding ? 1 : 0);
                const expanded = expandedItem === item.id;
                return (
                  <div key={item.id} className="forwarding-email-entry">
                    <div
                      role="button"
                      tabIndex={0}
                      className={cn(
                        "forwarding-email-row",
                        selectedItem?.id === item.id && "is-selected",
                      )}
                      onClick={() => {
                        onSelectItem(item);
                        setMobilePane("detail");
                      }}
                      onKeyDown={(event) => {
                        if (event.key === "Enter" || event.key === " ") {
                          event.preventDefault();
                          onSelectItem(item);
                          setMobilePane("detail");
                        }
                      }}
                    >
                      <span
                        className={cn(
                          "forwarding-email-status",
                          item.verified
                            ? "is-verified"
                            : item.deliveryStatus === "failed"
                              ? "is-failed"
                              : "is-pending",
                        )}
                      >
                        {item.verified ? (
                          <CheckmarkCircle20Regular />
                        ) : item.deliveryStatus === "failed" ? (
                          <Warning20Regular />
                        ) : (
                          <Clock20Regular />
                        )}
                      </span>
                      <div>
                        <strong>{item.email}</strong>
                        <p>
                          {item.verified
                            ? item.globalBinding
                              ? mailboxBindings.length
                                ? `已绑定全部邮箱 · 已绑定 ${mailboxBindings.length} 个单邮箱`
                                : "已绑定全部邮箱"
                              : mailboxBindings.length
                                ? `已绑定 ${mailboxBindings.length} 个单邮箱`
                                : "尚未使用"
                            : item.deliveryStatus === "failed"
                              ? item.deliveryError || "验证邮件发送失败"
                              : `等待验证 · 发送于 ${formatDateTime(item.verificationSentAt || item.createdAt)}`}
                        </p>
                      </div>
                      <div className="forwarding-row-actions">
                        <time dateTime={item.verificationSentAt || item.createdAt}>
                          {new Date(item.verificationSentAt || item.createdAt).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}
                        </time>
                        {item.verified && associationCount > 0 && (
                          <Button
                            type="button"
                            variant="ghost"
                            size="sm"
                            aria-expanded={expanded}
                            onClick={() =>
                              setExpandedItem(expanded ? "" : item.id)
                            }
                          >
                            关联 {associationCount}
                            <ChevronRight20Regular
                              className={cn(expanded && "is-expanded")}
                            />
                          </Button>
                        )}
                        {!item.verified && (
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            disabled={busy}
                            aria-label={`重新发送 ${item.email} 的验证邮件`}
                            onClick={(event) => {
                              event.stopPropagation();
                              onResend(item);
                            }}
                          >
                            <ArrowClockwise20Regular />
                          </Button>
                        )}
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon"
                          disabled={busy}
                          aria-label={`删除 ${item.email}`}
                          onClick={(event) => {
                            event.stopPropagation();
                            onDelete(item);
                          }}
                        >
                          <Delete20Regular />
                        </Button>
                      </div>
                    </div>
                    {expanded && (
                      <div className="forwarding-association-list">
                        {item.globalBinding && (
                          <AssociationRow
                            label="全部邮箱"
                            source="全部邮箱规则"
                            onManage={onManageGlobal}
                          />
                        )}
                        {mailboxBindings.map((mailboxAddress) => (
                          <AssociationRow
                            key={mailboxAddress}
                            label={mailboxAddress}
                            source="单邮箱独立规则"
                            onManage={() => onManageMailbox(mailboxAddress)}
                          />
                        ))}
                      </div>
                    )}
                  </div>
                );
              })
            ) : (
              <div className="forwarding-verification-empty">
                <MailCheckmark20Regular />
                <strong>还没有验证邮箱</strong>
                <p>添加并验证后，才能作为邮件转发目标。</p>
              </div>
            )}
          </div>
          {statusFilter === "mailbox-forwarding" && summaryTotal > 0 ? (
            <PaginationBar
              total={summaryTotal}
              noun="规则"
              currentPage={summaryPage}
              totalPages={summaryTotalPages}
              onPageChange={onSummaryPage}
            />
          ) : isVerificationView && total > 0 ? (
            <PaginationBar
              total={total}
              noun="验证邮箱"
              currentPage={currentPage}
              totalPages={totalPages}
              onPageChange={onPage}
            />
          ) : null}
        </article>

      <article className="mail-glass-reader-surface forwarding-verification-detail">
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="forwarding-verification-mobile-back"
          aria-label="返回验证邮箱列表"
          onClick={() => setMobilePane("list")}
        >
          <ChevronLeft20Regular />
        </Button>
        {selectedItem ? (
          <div className="forwarding-verification-detail-content">
            <header>
              <h2>{selectedItem.email}</h2>
              <span className={cn(
                "forwarding-verification-detail-status",
                selectedItem.verified
                  ? "is-verified"
                  : selectedItem.deliveryStatus === "failed"
                    ? "is-failed"
                    : "is-pending",
              )}>
                {selectedItem.verified ? <CheckmarkCircle20Regular /> : selectedItem.deliveryStatus === "failed" ? <Warning20Regular /> : <Clock20Regular />}
                {selectedItem.verified ? "已验证" : selectedItem.deliveryStatus === "failed" ? "发送失败" : "等待验证"}
              </span>
            </header>
            <dl className="forwarding-verification-detail-fields">
              <div><dt>验证状态</dt><dd>{selectedItem.verified ? "已完成" : "未完成"}</dd></div>
              <div><dt>最近发送</dt><dd>{selectedItem.verificationSentAt ? formatDateTime(selectedItem.verificationSentAt) : "暂无"}</dd></div>
              <div><dt>关联规则</dt><dd>{selectedItem.globalBinding || selectedItem.mailboxBindings?.length ? `已关联 ${(selectedItem.mailboxBindings?.length || 0) + (selectedItem.globalBinding ? 1 : 0)} 项` : "尚未使用"}</dd></div>
              <div><dt>创建时间</dt><dd>{formatDateTime(selectedItem.createdAt)}</dd></div>
              <div><dt>验证过期</dt><dd>{selectedItem.verificationExpiresAt ? formatDateTime(selectedItem.verificationExpiresAt) : "暂无"}</dd></div>
            </dl>
            <div className="forwarding-verification-detail-actions">
              {!selectedItem.verified && (
                <Button type="button" disabled={busy} onClick={() => onResend(selectedItem)}>
                  <ArrowClockwise20Regular />
                  重新发送验证邮件
                </Button>
              )}
              <Button type="button" variant="outline" disabled={busy} onClick={() => onDelete(selectedItem)}>
                <Delete20Regular />
                删除
              </Button>
            </div>
            <section className="forwarding-verification-linked-rules">
              <h3>关联的转发规则</h3>
              {!selectedItem.globalBinding && !selectedItem.mailboxBindings?.length ? (
                <div className="forwarding-verification-linked-empty">
                  <Mail20Regular />
                  <span>尚未绑定全部邮箱或单邮箱转发</span>
                </div>
              ) : (
                <div className="forwarding-verification-linked-list">
                  {selectedItem.globalBinding && <AssociationRow label="全部邮箱转发" source="全局转发目标" onManage={onManageGlobal} />}
                  {(selectedItem.mailboxBindings || []).map((address) => (
                    <AssociationRow key={address} label={address} source="单邮箱独立规则" onManage={() => onManageMailbox(address)} />
                  ))}
                </div>
              )}
            </section>
          </div>
        ) : selectedMailbox ? (
          <div className="forwarding-verification-detail-content">
            <header>
              <h2>{selectedMailbox.mailboxAddress}</h2>
              <span className="forwarding-verification-detail-status is-verified"><CheckmarkCircle20Regular />{selectedMailbox.enabled ? "已启用" : "已停用"}</span>
            </header>
            <dl className="forwarding-verification-detail-fields">
              <div><dt>独立目标</dt><dd>{selectedMailbox.independentTargets} 个</dd></div>
              <div><dt>继承全局</dt><dd>{selectedMailbox.inheritedTargets} 个</dd></div>
              <div><dt>转发至</dt><dd>{selectedMailbox.targets.map((target) => target.email).join("、") || "尚未设置"}</dd></div>
            </dl>
            <div className="forwarding-verification-detail-actions">
              <Button type="button" onClick={() => onManageMailbox(selectedMailbox.mailboxAddress)}>管理转发设置</Button>
            </div>
          </div>
        ) : statusFilter === "global-forwarding" ? (
          <div className="forwarding-verification-detail-content">
            <header><h2>全部邮箱转发</h2></header>
            <dl className="forwarding-verification-detail-fields">
              <div><dt>转发目标</dt><dd>{accountTargets.join("、") || "尚未绑定"}</dd></div>
              <div><dt>应用范围</dt><dd>当前账号下全部邮箱</dd></div>
            </dl>
            <div className="forwarding-verification-detail-actions"><Button type="button" onClick={onManageGlobal}>管理全部邮箱转发</Button></div>
          </div>
        ) : (
          <div className="forwarding-verification-detail-empty">
            <Mail20Regular />
            <strong>选择一个邮箱或转发规则查看详情</strong>
          </div>
        )}
      </article>
    </div>
  );
}

function AssociationRow({
  label,
  source,
  onManage,
}: {
  label: string;
  source: string;
  onManage: () => void;
}) {
  return (
    <div className="forwarding-association-row">
      <Mail20Regular />
      <span>
        <strong>{label}</strong>
        <small>{source} · 状态：启用</small>
      </span>
      <Button type="button" size="sm" variant="ghost" onClick={onManage}>
        管理
      </Button>
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
function forwardingScopeTargets(
  settings: ForwardingSettings,
  purpose: BindingPurpose,
) {
  if (purpose.scope === "account") return forwardingTargets(settings);
  const rule = settings.mailboxRules.find(
    (item) => item.mailboxId === purpose.mailboxId,
  );
  return rule?.targetEmails?.length
    ? rule.targetEmails
    : rule?.targetEmail
      ? [rule.targetEmail]
      : [];
}
function looksLikeEmail(value: string) {
  return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value.trim());
}
