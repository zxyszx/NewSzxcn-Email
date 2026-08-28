
import * as React from "react"

export type Language = "zh-CN" | "zh-TW" | "en"

export const LANGUAGE_STORAGE_KEY = "lanqin:language"

export const languageOptions: { value: Language; label: string; shortLabel: string; htmlLang: string }[] = [
  { value: "zh-CN", label: "简体中文", shortLabel: "简", htmlLang: "zh-CN" },
  { value: "zh-TW", label: "繁體中文", shortLabel: "繁", htmlLang: "zh-TW" },
  { value: "en", label: "English", shortLabel: "EN", htmlLang: "en" },
]

const languageValues = new Set(languageOptions.map((item) => item.value))

export function getInitialLanguage(): Language {
  if (typeof window === "undefined") return "zh-CN"
  const stored = window.localStorage.getItem(LANGUAGE_STORAGE_KEY)
  if (stored && languageValues.has(stored as Language)) return stored as Language
  const browserLanguage = window.navigator.language.toLowerCase()
  if (browserLanguage.startsWith("zh-tw") || browserLanguage.startsWith("zh-hk") || browserLanguage.startsWith("zh-mo")) return "zh-TW"
  if (browserLanguage.startsWith("en")) return "en"
  return "zh-CN"
}

export function setStoredLanguage(language: Language) {
  window.localStorage.setItem(LANGUAGE_STORAGE_KEY, language)
  window.dispatchEvent(new CustomEvent("lanqin:language", { detail: language }))
}

export function useLanguage() {
  const [language, setLanguageState] = React.useState<Language>(getInitialLanguage)
  React.useEffect(() => {
    function sync() {
      setLanguageState(getInitialLanguage())
    }
    window.addEventListener("storage", sync)
    window.addEventListener("lanqin:language", sync)
    return () => {
      window.removeEventListener("storage", sync)
      window.removeEventListener("lanqin:language", sync)
    }
  }, [])
  const setLanguage = React.useCallback((nextLanguage: Language) => {
    setStoredLanguage(nextLanguage)
    setLanguageState(nextLanguage)
  }, [])
  return [language, setLanguage] as const
}

type Translation = { "zh-TW": string; en: string }

const exactTranslations: Record<string, Translation> = {
  "收件箱": { "zh-TW": "收件匣", en: "Inbox" },
  "已发送": { "zh-TW": "已傳送", en: "Sent" },
  "草稿箱": { "zh-TW": "草稿匣", en: "Drafts" },
  "归档": { "zh-TW": "封存", en: "Archive" },
  "垃圾邮件": { "zh-TW": "垃圾郵件", en: "Spam" },
  "回收站": { "zh-TW": "垃圾桶", en: "Trash" },
  "全部邮件": { "zh-TW": "全部郵件", en: "All mail" },
  "未读邮件": { "zh-TW": "未讀郵件", en: "Unread" },
  "星标邮件": { "zh-TW": "星號郵件", en: "Starred" },
  "有附件": { "zh-TW": "有附件", en: "Has attachments" },
  "切换语言": { "zh-TW": "切換語言", en: "Switch language" },
  "写邮件": { "zh-TW": "寫郵件", en: "Compose" },
  "邮件夹": { "zh-TW": "郵件匣", en: "Folders" },
  "外部邮箱": { "zh-TW": "外部信箱", en: "External mailbox" },
  "标签": { "zh-TW": "標籤", en: "Labels" },
  "暂无标签": { "zh-TW": "暫無標籤", en: "No labels" },
  "收起侧栏": { "zh-TW": "收合側欄", en: "Collapse sidebar" },
  "选择邮箱": { "zh-TW": "選擇信箱", en: "Select mailbox" },
  "未注册邮箱": { "zh-TW": "未註冊信箱", en: "Unregistered mailbox" },
  "没有可用邮箱": { "zh-TW": "沒有可用信箱", en: "No mailboxes available" },
  "邮箱地址已复制": { "zh-TW": "信箱地址已複製", en: "Mailbox address copied" },
  "打开导航": { "zh-TW": "開啟導覽", en: "Open navigation" },
  "邮箱导航": { "zh-TW": "信箱導覽", en: "Mailbox navigation" },
  "刷新邮件": { "zh-TW": "重新整理郵件", en: "Refresh mail" },
  "自动刷新中": { "zh-TW": "自動重新整理中", en: "Auto-refreshing" },
  "自动刷新中...": { "zh-TW": "自動重新整理中...", en: "Auto-refreshing..." },
  "自动刷新已开启": { "zh-TW": "自動重新整理已開啟", en: "Auto-refresh enabled" },
  "全部已读": { "zh-TW": "全部已讀", en: "Mark all read" },
  "搜索邮件": { "zh-TW": "搜尋郵件", en: "Search mail" },
  "搜索远端邮件": { "zh-TW": "搜尋遠端郵件", en: "Search remote mail" },
  "搜索发送队列": { "zh-TW": "搜尋傳送佇列", en: "Search send queue" },
  "搜索待发送": { "zh-TW": "搜尋待傳送", en: "Search scheduled mail" },
  "选择当前页邮件": { "zh-TW": "選擇目前頁面的郵件", en: "Select messages on this page" },
  "加载中...": { "zh-TW": "載入中...", en: "Loading..." },
  "加载更多": { "zh-TW": "載入更多", en: "Load more" },
  "选择一封邮件阅读": { "zh-TW": "選擇一封郵件閱讀", en: "Select a message to read" },
  "回复": { "zh-TW": "回覆", en: "Reply" },
  "转发": { "zh-TW": "轉寄", en: "Forward" },
  "投递时间线": { "zh-TW": "投遞時間軸", en: "Delivery timeline" },
  "取消归档": { "zh-TW": "取消封存", en: "Unarchive" },
  "删除": { "zh-TW": "刪除", en: "Delete" },
  "附件": { "zh-TW": "附件", en: "Attachments" },
  "确认": { "zh-TW": "確認", en: "Confirm" },
  "取消": { "zh-TW": "取消", en: "Cancel" },
  "关闭": { "zh-TW": "關閉", en: "Close" },
  "打开": { "zh-TW": "開啟", en: "Open" },
  "刷新": { "zh-TW": "重新整理", en: "Refresh" },
  "新建文件夹": { "zh-TW": "新增資料夾", en: "New folder" },
  "文件夹名称": { "zh-TW": "資料夾名稱", en: "Folder name" },
  "创建": { "zh-TW": "建立", en: "Create" },
  "创建中...": { "zh-TW": "建立中...", en: "Creating..." },
  "例如：客户、账单、项目归档": { "zh-TW": "例如：客戶、帳單、專案封存", en: "e.g. Clients, invoices, project archive" },
  "新建标签": { "zh-TW": "新增標籤", en: "New label" },
  "删除标签失败": { "zh-TW": "刪除標籤失敗", en: "Failed to delete label" },
  "添加标签失败": { "zh-TW": "新增標籤失敗", en: "Failed to add label" },
  "移除标签失败": { "zh-TW": "移除標籤失敗", en: "Failed to remove label" },
  "创建标签失败": { "zh-TW": "建立標籤失敗", en: "Failed to create label" },
  "添加标签": { "zh-TW": "新增標籤", en: "Add label" },
  "请先在侧栏新建标签": { "zh-TW": "請先在側欄新增標籤", en: "Create a label in the sidebar first" },
  "无": { "zh-TW": "無", en: "None" },
  "无主题": { "zh-TW": "無主旨", en: "No subject" },
  "(无主题)": { "zh-TW": "(無主旨)", en: "(No subject)" },
  "未知发件人": { "zh-TW": "未知寄件者", en: "Unknown sender" },
  "未知发件人地址": { "zh-TW": "未知寄件者地址", en: "Unknown sender address" },
  "发件人": { "zh-TW": "寄件者", en: "Sender" },
  "发件人地址": { "zh-TW": "寄件者地址", en: "Sender address" },
  "收件人": { "zh-TW": "收件者", en: "Recipients" },
  "抄送": { "zh-TW": "副本", en: "Cc" },
  "密送": { "zh-TW": "密件副本", en: "Bcc" },
  "投递邮箱": { "zh-TW": "投遞信箱", en: "Delivered mailbox" },
  "发送时间": { "zh-TW": "傳送時間", en: "Send time" },
  "接收时间": { "zh-TW": "接收時間", en: "Received time" },
  "未填写收件人": { "zh-TW": "未填寫收件者", en: "No recipients" },
  "邮件正文": { "zh-TW": "郵件內文", en: "Message body" },
  "邮件详情": { "zh-TW": "郵件詳情", en: "Message details" },
  "上一封": { "zh-TW": "上一封", en: "Previous" },
  "下一封": { "zh-TW": "下一封", en: "Next" },
  "更多操作": { "zh-TW": "更多操作", en: "More actions" },
  "邮件不存在": { "zh-TW": "郵件不存在", en: "Message not found" },
  "标为已读": { "zh-TW": "標為已讀", en: "Mark as read" },
  "标为未读": { "zh-TW": "標為未讀", en: "Mark as unread" },
  "添加星标": { "zh-TW": "加上星號", en: "Add star" },
  "取消星标": { "zh-TW": "移除星號", en: "Remove star" },
  "移入回收站": { "zh-TW": "移至垃圾桶", en: "Move to trash" },
  "移入垃圾邮件": { "zh-TW": "移至垃圾郵件", en: "Move to spam" },
  "批量操作": { "zh-TW": "批次操作", en: "Bulk actions" },
  "移动到": { "zh-TW": "移動到", en: "Move to" },
  "移到最上": { "zh-TW": "移到最上方", en: "Move to top" },
  "上移一位": { "zh-TW": "上移一位", en: "Move up" },
  "下移一位": { "zh-TW": "下移一位", en: "Move down" },
  "移到最下": { "zh-TW": "移到最下方", en: "Move to bottom" },
  "删除文件夹": { "zh-TW": "刪除資料夾", en: "Delete folder" },
  "远端文件夹没有邮件": { "zh-TW": "遠端資料夾沒有郵件", en: "No messages in remote folder" },
  "当前筛选条件下没有远端邮件": { "zh-TW": "目前篩選條件下沒有遠端郵件", en: "No remote messages match the current filters" },
  "没有待发送邮件": { "zh-TW": "沒有待傳送郵件", en: "No scheduled mail" },
  "当前搜索没有匹配的定时邮件": { "zh-TW": "目前搜尋沒有符合的定時郵件", en: "No scheduled messages match your search" },
  "发送队列为空": { "zh-TW": "傳送佇列為空", en: "Send queue is empty" },
  "当前搜索没有匹配的发送任务": { "zh-TW": "目前搜尋沒有符合的傳送任務", en: "No send tasks match your search" },
  "当前筛选条件下没有邮件": { "zh-TW": "目前篩選條件下沒有郵件", en: "No messages match the current filters" },
  "暂无星标邮件": { "zh-TW": "暫無星號郵件", en: "No starred mail" },
  "当前标签没有邮件": { "zh-TW": "目前標籤沒有郵件", en: "No messages with this label" },
  "收件箱暂时为空": { "zh-TW": "收件匣暫時為空", en: "Inbox is empty" },
  "还没有草稿": { "zh-TW": "還沒有草稿", en: "No drafts yet" },
  "还没有已发送邮件": { "zh-TW": "還沒有已傳送郵件", en: "No sent mail yet" },
  "回收站是空的": { "zh-TW": "垃圾桶是空的", en: "Trash is empty" },
  "暂无垃圾邮件": { "zh-TW": "暫無垃圾郵件", en: "No spam" },
  "当前文件夹没有邮件": { "zh-TW": "目前資料夾沒有郵件", en: "No messages in this folder" },
  "待发送": { "zh-TW": "待傳送", en: "Scheduled" },
  "发送队列": { "zh-TW": "傳送佇列", en: "Send queue" },
  "全部状态": { "zh-TW": "全部狀態", en: "All statuses" },
  "排队中": { "zh-TW": "佇列中", en: "Queued" },
  "发送中": { "zh-TW": "傳送中", en: "Sending" },
  "发送失败": { "zh-TW": "傳送失敗", en: "Failed" },
  "已投递": { "zh-TW": "已投遞", en: "Delivered" },
  "已取消": { "zh-TW": "已取消", en: "Canceled" },
  "等待发送": { "zh-TW": "等待傳送", en: "Pending" },
  "清除": { "zh-TW": "清除", en: "Clear" },
  "未记录收件人": { "zh-TW": "未記錄收件者", en: "No recorded recipients" },
  "时间线": { "zh-TW": "時間軸", en: "Timeline" },
  "重试": { "zh-TW": "重試", en: "Retry" },
  "暂无投递事件": { "zh-TW": "暫無投遞事件", en: "No delivery events" },
  "队列事件": { "zh-TW": "佇列事件", en: "Queue event" },
  "定时发送": { "zh-TW": "定時傳送", en: "Scheduled send" },
  "未知": { "zh-TW": "未知", en: "Unknown" },
  "同步": { "zh-TW": "同步", en: "Sync" },
  "直连": { "zh-TW": "直連", en: "Direct" },
  "还没有可用邮箱": { "zh-TW": "還沒有可用信箱", en: "No mailbox available" },
  "请在个人中心申请邮箱，或联系管理员为当前账号分配邮箱。": { "zh-TW": "請在個人中心申請信箱，或聯絡管理員為目前帳號分配信箱。", en: "Apply for a mailbox in Profile, or contact an administrator to assign one to this account." },
  "前往个人中心": { "zh-TW": "前往個人中心", en: "Go to profile" },
  "请前往邮箱管理，创建、申请或联系管理员分配邮箱。": { "zh-TW": "請前往信箱管理，建立、申請或聯絡管理員分配信箱。", en: "Open mailbox management to create, request, or ask an administrator to assign a mailbox." },
  "前往邮箱管理": { "zh-TW": "前往信箱管理", en: "Go to mailbox management" },
  "提示：尚未选择开放域名。请在“后台管理 → 系统设置 → 邮件”中至少勾选一个已启用域名。": { "zh-TW": "提示：尚未選擇開放網域。請在「後台管理 → 系統設定 → 郵件」中至少勾選一個已啟用網域。", en: "No domain is open for mailbox requests. Open Admin → System settings → Mail and select at least one active domain." },
  "提示：账号自助申请邮箱未开启。请在“后台管理 → 系统设置 → 邮件”中开启，并勾选开放域名。": { "zh-TW": "提示：帳號自助申請信箱尚未開啟。請在「後台管理 → 系統設定 → 郵件」中開啟，並勾選開放網域。", en: "Mailbox self-service is disabled. Enable it under Admin → System settings → Mail, then select the available domains." },
  "提示：当前账号暂不可创建新邮箱，请联系管理员开启账号自助申请邮箱。": { "zh-TW": "提示：目前帳號暫時無法建立新信箱，請聯絡管理員開啟帳號自助申請信箱。", en: "This account cannot create a mailbox. Ask an administrator to enable mailbox self-service." },
  "前往设置": { "zh-TW": "前往設定", en: "Open settings" },
  "无邮箱前台权限": { "zh-TW": "無信箱前台權限", en: "No mailbox access" },
  "当前账号未开启邮箱前台访问权限。": { "zh-TW": "目前帳號未開啟信箱前台存取權限。", en: "Mailbox access is not enabled for this account." },
  "无邮件查看权限": { "zh-TW": "無郵件檢視權限", en: "No mail read permission" },
  "当前账号可以访问邮箱前台，但未开启邮件查看权限。": { "zh-TW": "目前帳號可以存取信箱前台，但未開啟郵件檢視權限。", en: "This account can access mailbox UI, but mail reading is not enabled." },
  "无定时发送权限": { "zh-TW": "無定時傳送權限", en: "No scheduled send permission" },
  "当前账号不能查看或管理定时发送任务。": { "zh-TW": "目前帳號不能檢視或管理定時傳送任務。", en: "This account cannot view or manage scheduled send tasks." },
  "无发送队列权限": { "zh-TW": "無傳送佇列權限", en: "No send queue permission" },
  "当前账号不能查看发送队列。": { "zh-TW": "目前帳號不能檢視傳送佇列。", en: "This account cannot view the send queue." },
  "操作失败": { "zh-TW": "操作失敗", en: "Operation failed" },
  "请稍后重试": { "zh-TW": "請稍後重試", en: "Please try again later" },
  "已取消定时发送": { "zh-TW": "已取消定時傳送", en: "Scheduled send canceled" },
  "已移除失败记录": { "zh-TW": "已移除失敗記錄", en: "Failed record removed" },
  "文件夹已创建": { "zh-TW": "資料夾已建立", en: "Folder created" },
  "创建文件夹失败": { "zh-TW": "建立資料夾失敗", en: "Failed to create folder" },
  "文件夹排序失败": { "zh-TW": "資料夾排序失敗", en: "Failed to reorder folders" },
  "文件夹已删除": { "zh-TW": "資料夾已刪除", en: "Folder deleted" },
  "删除文件夹失败": { "zh-TW": "刪除資料夾失敗", en: "Failed to delete folder" },
  "已重新加入发送队列": { "zh-TW": "已重新加入傳送佇列", en: "Added back to send queue" },
  "重试失败": { "zh-TW": "重試失敗", en: "Retry failed" },
  "已取消发送任务": { "zh-TW": "已取消傳送任務", en: "Send task canceled" },
  "取消失败": { "zh-TW": "取消失敗", en: "Cancel failed" },
  "当前没有未读邮件": { "zh-TW": "目前沒有未讀郵件", en: "No unread messages" },
  "删除所选邮件？": { "zh-TW": "刪除所選郵件？", en: "Delete selected messages?" },
  "删除邮件": { "zh-TW": "刪除郵件", en: "Delete messages" },
  "批量操作失败": { "zh-TW": "批次操作失敗", en: "Bulk action failed" },
  "删除这封邮件？": { "zh-TW": "刪除這封郵件？", en: "Delete this message?" },
  "这封草稿已在待发送队列中": { "zh-TW": "這封草稿已在待傳送佇列中", en: "This draft is already scheduled" },
  "请先取消定时发送，再继续编辑。": { "zh-TW": "請先取消定時傳送，再繼續編輯。", en: "Cancel the scheduled send before editing." },
  "打开草稿失败": { "zh-TW": "開啟草稿失敗", en: "Failed to open draft" },
  "文件夹内的邮件会移回收件箱，不会被删除。": { "zh-TW": "資料夾內的郵件會移回收件匣，不會被刪除。", en: "Messages in this folder will be moved back to Inbox, not deleted." },
  "编辑草稿": { "zh-TW": "編輯草稿", en: "Edit draft" },
  "打开邮件": { "zh-TW": "開啟郵件", en: "Open message" },
  "发件邮箱": { "zh-TW": "寄件信箱", en: "From mailbox" },
  "未选择": { "zh-TW": "未選擇", en: "Not selected" },
  "分别发送": { "zh-TW": "分別傳送", en: "Send separately" },
  "主　题": { "zh-TW": "主　旨", en: "Subject" },
  "输入主题": { "zh-TW": "輸入主旨", en: "Enter subject" },
  "写信": { "zh-TW": "寫信", en: "Compose" },
  "正在保存草稿...": { "zh-TW": "正在儲存草稿...", en: "Saving draft..." },
  "草稿保存失败": { "zh-TW": "草稿儲存失敗", en: "Failed to save draft" },
  "发送": { "zh-TW": "傳送", en: "Send" },
  "发送中...": { "zh-TW": "傳送中...", en: "Sending..." },
  "发送成功": { "zh-TW": "傳送成功", en: "Sent" },
  "定时": { "zh-TW": "定時", en: "Schedule" },
  "继续": { "zh-TW": "繼續", en: "Continue" },
  "继续发送": { "zh-TW": "繼續傳送", en: "Continue sending" },
  "继续分别发送": { "zh-TW": "繼續分別傳送", en: "Continue sending separately" },
  "继续定时发送": { "zh-TW": "繼續定時傳送", en: "Continue scheduling" },
  "请选择发件邮箱": { "zh-TW": "請選擇寄件信箱", en: "Select a from mailbox" },
  "确认发送这封邮件？": { "zh-TW": "確認傳送這封郵件？", en: "Send this message?" },
  "确认定时发送？": { "zh-TW": "確認定時傳送？", en: "Schedule this message?" },
  "请选择发送时间": { "zh-TW": "請選擇傳送時間", en: "Select a send time" },
  "发送时间需要晚于当前时间": { "zh-TW": "傳送時間需要晚於目前時間", en: "Send time must be in the future" },
  "确认定时": { "zh-TW": "確認定時", en: "Confirm schedule" },
  "正在设置...": { "zh-TW": "正在設定...", en: "Scheduling..." },
  "输入正文": { "zh-TW": "輸入內文", en: "Write your message" },
  "正文": { "zh-TW": "內文", en: "Body" },
  "撤销": { "zh-TW": "復原", en: "Undo" },
  "重做": { "zh-TW": "重做", en: "Redo" },
  "插入": { "zh-TW": "插入", en: "Insert" },
  "链接": { "zh-TW": "連結", en: "Link" },
  "图片链接": { "zh-TW": "圖片連結", en: "Image link" },
  "分隔线": { "zh-TW": "分隔線", en: "Divider" },
  "日程": { "zh-TW": "行程", en: "Event" },
  "表情": { "zh-TW": "表情", en: "Emoji" },
  "格式": { "zh-TW": "格式", en: "Format" },
  "预览": { "zh-TW": "預覽", en: "Preview" },
  "签名": { "zh-TW": "簽名", en: "Signature" },
  "清除格式": { "zh-TW": "清除格式", en: "Clear formatting" },
  "加粗": { "zh-TW": "粗體", en: "Bold" },
  "斜体": { "zh-TW": "斜體", en: "Italic" },
  "下划线": { "zh-TW": "底線", en: "Underline" },
  "删除线": { "zh-TW": "刪除線", en: "Strikethrough" },
  "文字颜色": { "zh-TW": "文字顏色", en: "Text color" },
  "高亮": { "zh-TW": "醒目提示", en: "Highlight" },
  "无序列表": { "zh-TW": "項目符號清單", en: "Bulleted list" },
  "有序列表": { "zh-TW": "編號清單", en: "Numbered list" },
  "减少缩进": { "zh-TW": "減少縮排", en: "Decrease indent" },
  "增加缩进": { "zh-TW": "增加縮排", en: "Increase indent" },
  "左对齐": { "zh-TW": "靠左對齊", en: "Align left" },
  "居中": { "zh-TW": "置中", en: "Center" },
  "右对齐": { "zh-TW": "靠右對齊", en: "Align right" },
  "引用": { "zh-TW": "引用", en: "Quote" },
  "代码块": { "zh-TW": "程式碼區塊", en: "Code block" },
  "邮件预览": { "zh-TW": "郵件預覽", en: "Message preview" },
  "编辑链接": { "zh-TW": "編輯連結", en: "Edit link" },
  "插入链接": { "zh-TW": "插入連結", en: "Insert link" },
  "编辑图片": { "zh-TW": "編輯圖片", en: "Edit image" },
  "插入图片": { "zh-TW": "插入圖片", en: "Insert image" },
  "链接地址": { "zh-TW": "連結地址", en: "Link URL" },
  "图片地址": { "zh-TW": "圖片地址", en: "Image URL" },
  "显示文字": { "zh-TW": "顯示文字", en: "Display text" },
  "默认使用链接地址": { "zh-TW": "預設使用連結地址", en: "Defaults to the URL" },
  "替代文字": { "zh-TW": "替代文字", en: "Alt text" },
  "图片说明": { "zh-TW": "圖片說明", en: "Image description" },
  "更新": { "zh-TW": "更新", en: "Update" },
  "默认字体": { "zh-TW": "預設字體", en: "Default font" },
  "微软雅黑": { "zh-TW": "微軟正黑體", en: "Microsoft YaHei" },
  "小号": { "zh-TW": "小號", en: "Small" },
  "中号": { "zh-TW": "中號", en: "Medium" },
  "大号": { "zh-TW": "大號", en: "Large" },
  "默认": { "zh-TW": "預設", en: "Default" },
  "红色": { "zh-TW": "紅色", en: "Red" },
  "蓝色": { "zh-TW": "藍色", en: "Blue" },
  "绿色": { "zh-TW": "綠色", en: "Green" },
  "紫色": { "zh-TW": "紫色", en: "Purple" },
  "黄色": { "zh-TW": "黃色", en: "Yellow" },
  "粉色": { "zh-TW": "粉色", en: "Pink" },
  "无高亮": { "zh-TW": "無醒目提示", en: "No highlight" },
  "新建日程": { "zh-TW": "新增行程", en: "New event" },
  "输入日程主题": { "zh-TW": "輸入行程主旨", en: "Enter event title" },
  "请输入日程主题": { "zh-TW": "請輸入行程主旨", en: "Enter an event title" },
  "开始": { "zh-TW": "開始", en: "Start" },
  "全天": { "zh-TW": "全天", en: "All day" },
  "持续": { "zh-TW": "持續", en: "Duration" },
  "自定义": { "zh-TW": "自訂", en: "Custom" },
  "提醒": { "zh-TW": "提醒", en: "Reminder" },
  "重复": { "zh-TW": "重複", en: "Repeat" },
  "农历": { "zh-TW": "農曆", en: "Lunar" },
  "位置": { "zh-TW": "位置", en: "Location" },
  "请输入位置": { "zh-TW": "請輸入位置", en: "Enter location" },
  "描述": { "zh-TW": "描述", en: "Description" },
  "输入描述": { "zh-TW": "輸入描述", en: "Enter description" },
  "确定": { "zh-TW": "確定", en: "OK" },
  "准时": { "zh-TW": "準時", en: "On time" },
  "永不": { "zh-TW": "永不", en: "Never" },
  "每天": { "zh-TW": "每天", en: "Daily" },
  "每周": { "zh-TW": "每週", en: "Weekly" },
  "每月": { "zh-TW": "每月", en: "Monthly" },
  "每年": { "zh-TW": "每年", en: "Yearly" },
  "明早 9 点": { "zh-TW": "明早 9 點", en: "Tomorrow 9 AM" },
  "下周一 9 点": { "zh-TW": "下週一 9 點", en: "Next Monday 9 AM" },
  "时间": { "zh-TW": "時間", en: "Time" },
  // Profile and administration surfaces
  "账号设置": { "zh-TW": "帳號設定", en: "Account settings" },
  "邮箱管理": { "zh-TW": "信箱管理", en: "Mailbox management" },
  "联系人管理": { "zh-TW": "聯絡人管理", en: "Contacts" },
  "待清理邮件": { "zh-TW": "待清理郵件", en: "Cleanup queue" },
  "收信规则": { "zh-TW": "收信規則", en: "Mail rules" },
  "被拦截邮件": { "zh-TW": "被攔截郵件", en: "Blocked mail" },
  "数据统计": { "zh-TW": "資料統計", en: "Analytics" },
  "开发者": { "zh-TW": "開發者", en: "Developer" },
  "后台管理": { "zh-TW": "後台管理", en: "Admin" },
  "管理": { "zh-TW": "管理", en: "Manage" },
  "退出登录": { "zh-TW": "登出", en: "Sign out" },
  "账号": { "zh-TW": "帳號", en: "Account" },
  "邮件": { "zh-TW": "郵件", en: "Mail" },
  "通知与客户端": { "zh-TW": "通知與用戶端", en: "Notifications & clients" },
  "安全": { "zh-TW": "安全性", en: "Security" },
  "账号信息": { "zh-TW": "帳號資訊", en: "Account information" },
  "主登录邮箱": { "zh-TW": "主登入信箱", en: "Primary login email" },
  "显示名称": { "zh-TW": "顯示名稱", en: "Display name" },
  "邮件列表显示": { "zh-TW": "郵件清單顯示", en: "Message list display" },
  "标准布局": { "zh-TW": "標準版面", en: "Standard" },
  "紧凑布局": { "zh-TW": "緊湊版面", en: "Compact" },
  "存储容量": { "zh-TW": "儲存容量", en: "Storage" },
  "邮件清理": { "zh-TW": "郵件清理", en: "Mail cleanup" },
  "不限": { "zh-TW": "不限", en: "Unlimited" },
  "账号配额": { "zh-TW": "帳號配額", en: "Account limits" },
  "实时按当前账号配置计算": { "zh-TW": "依目前帳號設定即時計算", en: "Calculated from current account settings" },
  "邮箱创建": { "zh-TW": "建立信箱", en: "Mailbox creation" },
  "发信频率": { "zh-TW": "寄信頻率", en: "Sending rate" },
  "协议访问频率": { "zh-TW": "協定存取頻率", en: "Protocol access rate" },
  "附件与应用密码": { "zh-TW": "附件與應用程式密碼", en: "Attachments & app passwords" },
  "保存": { "zh-TW": "儲存", en: "Save" },
  "保存中...": { "zh-TW": "儲存中...", en: "Saving..." },
  "账户设置": { "zh-TW": "帳號設定", en: "Account settings" },
  "个人资料已保存": { "zh-TW": "個人資料已儲存", en: "Profile saved" },
  "密码已更新": { "zh-TW": "密碼已更新", en: "Password updated" },
  "修改密码": { "zh-TW": "修改密碼", en: "Change password" },
  "双因素认证": { "zh-TW": "雙因素驗證", en: "Two-factor authentication" },
  "双因素认证已启用": { "zh-TW": "雙因素驗證已啟用", en: "Two-factor authentication enabled" },
  "双因素认证已关闭": { "zh-TW": "雙因素驗證已關閉", en: "Two-factor authentication disabled" },
  "应用密码": { "zh-TW": "應用程式密碼", en: "App passwords" },
  "标签管理": { "zh-TW": "標籤管理", en: "Label management" },
  "新建签名": { "zh-TW": "新增簽名", en: "New signature" },
  "系统设置": { "zh-TW": "系統設定", en: "System settings" },
  "仪表盘": { "zh-TW": "儀表板", en: "Dashboard" },
  "权限配置": { "zh-TW": "權限設定", en: "Permissions" },
  "域名管理": { "zh-TW": "網域管理", en: "Domains" },
  "邮件转发": { "zh-TW": "郵件轉寄", en: "Forwarding" },
  "备份与恢复": { "zh-TW": "備份與還原", en: "Backup & restore" },
  "基础设置": { "zh-TW": "基本設定", en: "General settings" },
  "发信通道": { "zh-TW": "寄信通道", en: "Sending channel" },
  "存储设置": { "zh-TW": "儲存設定", en: "Storage settings" },
  "邮件设置": { "zh-TW": "郵件設定", en: "Mail settings" },
  "外部 IMAP 接入": { "zh-TW": "外部 IMAP 連線", en: "External IMAP" },
  "安全设置": { "zh-TW": "安全設定", en: "Security settings" },
  "模板": { "zh-TW": "範本", en: "Templates" },
  "公网主机名": { "zh-TW": "公網主機名稱", en: "Public hostname" },
  "访问地址": { "zh-TW": "存取網址", en: "Base URL" },
  "登录有效期小时": { "zh-TW": "登入有效期（小時）", en: "Session lifetime (hours)" },
  "Maildir 扫描秒数": { "zh-TW": "Maildir 掃描秒數", en: "Maildir scan interval (seconds)" },
  "允许 HTTP 调试": { "zh-TW": "允許 HTTP 偵錯", en: "Allow HTTP debugging" },
  "自动刷新": { "zh-TW": "自動重新整理", en: "Auto refresh" },
  "刷新间隔秒数": { "zh-TW": "重新整理間隔（秒）", en: "Refresh interval (seconds)" },
  "账号自助申请邮箱": { "zh-TW": "帳號自助申請信箱", en: "Mailbox self-service" },
  "开放域名": { "zh-TW": "開放網域", en: "Available domains" },
  "禁止前缀": { "zh-TW": "禁止前綴", en: "Reserved prefixes" },
  "启用外部 IMAP": { "zh-TW": "啟用外部 IMAP", en: "Enable external IMAP" },
  "保存设置": { "zh-TW": "儲存設定", en: "Save settings" },
  "重新读取": { "zh-TW": "重新讀取", en: "Reload" },
  "页面数据读取失败": { "zh-TW": "頁面資料讀取失敗", en: "Unable to load page data" },
  "数据读取失败": { "zh-TW": "資料讀取失敗", en: "Unable to load data" },
  "无人收件": { "zh-TW": "無人收件", en: "Catch-all delivery" },
  "当前登录": { "zh-TW": "目前登入", en: "Current sign-in" },
  "密码管理": { "zh-TW": "密碼管理", en: "Password management" },
  "当前密码": { "zh-TW": "目前密碼", en: "Current password" },
  "新密码": { "zh-TW": "新密碼", en: "New password" },
  "确认新密码": { "zh-TW": "確認新密碼", en: "Confirm new password" },
  "两步验证": { "zh-TW": "兩步驟驗證", en: "Two-step verification" },
  "已启用": { "zh-TW": "已啟用", en: "Enabled" },
  "未启用": { "zh-TW": "未啟用", en: "Disabled" },
  "生成中...": { "zh-TW": "產生中...", en: "Generating..." },
  "启用两步验证": { "zh-TW": "啟用兩步驟驗證", en: "Enable two-step verification" },
  "验证码": { "zh-TW": "驗證碼", en: "Verification code" },
  "确认启用": { "zh-TW": "確認啟用", en: "Confirm enable" },
  "关闭两步验证": { "zh-TW": "關閉兩步驟驗證", en: "Disable two-step verification" },
  "签名管理": { "zh-TW": "簽名管理", en: "Signature management" },
  "标签名称": { "zh-TW": "標籤名稱", en: "Label name" },
  "标签颜色": { "zh-TW": "標籤顏色", en: "Label color" },
  "创建签名": { "zh-TW": "建立簽名", en: "Create signature" },
  "保存修改": { "zh-TW": "儲存變更", en: "Save changes" },
  "验证邮箱": { "zh-TW": "驗證信箱", en: "Verified mailboxes" },
  "启用转发": { "zh-TW": "啟用轉寄", en: "Enable forwarding" },
  "待验证": { "zh-TW": "待驗證", en: "Pending verification" },
  "已验证": { "zh-TW": "已驗證", en: "Verified" },
  "最近验证": { "zh-TW": "最近驗證", en: "Last verified" },
  "邮箱总数": { "zh-TW": "信箱總數", en: "Total mailboxes" },
  "已验证地址": { "zh-TW": "已驗證地址", en: "Verified addresses" },
  "账号级": { "zh-TW": "帳號層級", en: "Account-level" },
  "邮箱单独": { "zh-TW": "信箱個別", en: "Mailbox-only" },
  "继承账号级转发": { "zh-TW": "繼承帳號層級轉寄", en: "Inherit account forwarding" },
  "转发中": { "zh-TW": "轉寄中", en: "Forwarding" },
  "绑定": { "zh-TW": "繫結", en: "Bind" },
  "处理中": { "zh-TW": "處理中", en: "Processing" },
  "详情": { "zh-TW": "詳情", en: "Details" },
  "当前状态": { "zh-TW": "目前狀態", en: "Current status" },
  "今日发送": { "zh-TW": "今日寄出", en: "Sent today" },
  "今日接收": { "zh-TW": "今日收到", en: "Received today" },
  "队列邮件": { "zh-TW": "佇列郵件", en: "Queued mail" },
  "存储用量": { "zh-TW": "儲存用量", en: "Storage used" },
  "公网地址": { "zh-TW": "公網地址", en: "Public address" },
  "SMTP 地址": { "zh-TW": "SMTP 地址", en: "SMTP address" },
  "注册": { "zh-TW": "註冊", en: "Registration" },
  "自助申请邮箱": { "zh-TW": "自助申請信箱", en: "Mailbox self-service" },
  "正常": { "zh-TW": "正常", en: "Healthy" },
  "部分正常": { "zh-TW": "部分正常", en: "Partially healthy" },
  "未检测": { "zh-TW": "未檢測", en: "Not checked" },
  "尚未检测": { "zh-TW": "尚未檢測", en: "Not checked yet" },
  "需检查": { "zh-TW": "需檢查", en: "Needs attention" },
  "已开放": { "zh-TW": "已開放", en: "Open" },
  "状态更新失败": { "zh-TW": "狀態更新失敗", en: "Failed to update status" },
  "添加邮件域名": { "zh-TW": "新增郵件網域", en: "Add mail domain" },
  "完成 DNS 检测": { "zh-TW": "完成 DNS 檢測", en: "Run DNS check" },
  "创建邮箱": { "zh-TW": "建立信箱", en: "Create mailbox" },
  "确认发信链路": { "zh-TW": "確認寄信鏈路", en: "Verify sending path" },
  "完成收发测试": { "zh-TW": "完成收發測試", en: "Complete send/receive test" },
  "DNS 正常": { "zh-TW": "DNS 正常", en: "DNS healthy" },
  "DNS 异常": { "zh-TW": "DNS 異常", en: "DNS issue" },
}

const templateTranslations: { pattern: RegExp; "zh-TW": string; en: string }[] = [
  { pattern: /^已刷新 (.+)$/, "zh-TW": "已重新整理 $1", en: "Refreshed $1" },
  { pattern: /^已选 (\d+) 封$/, "zh-TW": "已選 $1 封", en: "$1 selected" },
  { pattern: /^(\d+) \/ (\d+) 封邮件$/, "zh-TW": "$1 / $2 封郵件", en: "$1 / $2 messages" },
  { pattern: /^(\d+) \/ (\d+) 封$/, "zh-TW": "$1 / $2 封", en: "$1 / $2 messages" },
  { pattern: /^(\d+) \/ (\d+) 封定时邮件$/, "zh-TW": "$1 / $2 封定時郵件", en: "$1 / $2 scheduled messages" },
  { pattern: /^(\d+) \/ (\d+) 个发送任务$/, "zh-TW": "$1 / $2 個傳送任務", en: "$1 / $2 send tasks" },
  { pattern: /^收到 (\d+) 封新邮件$/, "zh-TW": "收到 $1 封新郵件", en: "$1 new messages" },
  { pattern: /^新邮件：(.+)$/, "zh-TW": "新郵件：$1", en: "New message: $1" },
  { pattern: /^(.+) 等发来新邮件$/, "zh-TW": "$1 等寄來新郵件", en: "$1 and others sent new mail" },
  { pattern: /^已标记 (\d+) 封邮件为已读$/, "zh-TW": "已標記 $1 封郵件為已讀", en: "Marked $1 messages as read" },
  { pattern: /^已处理 (\d+) 封邮件$/, "zh-TW": "已處理 $1 封郵件", en: "Processed $1 messages" },
  { pattern: /^将删除当前选中的 (\d+) 封邮件，此操作无法从邮件列表中恢复。$/, "zh-TW": "將刪除目前選取的 $1 封郵件，此操作無法從郵件清單中復原。", en: "This will delete the $1 selected messages. This cannot be undone from the message list." },
  { pattern: /^邮件“(.+)”将被删除。$/, "zh-TW": "郵件「$1」將被刪除。", en: "Message “$1” will be deleted." },
  { pattern: /^删除文件夹“(.+)”？$/, "zh-TW": "刪除資料夾「$1」？", en: "Delete folder “$1”?" },
  { pattern: /^删除标签 (.+)$/, "zh-TW": "刪除標籤 $1", en: "Delete label $1" },
  { pattern: /^移除标签 (.+)$/, "zh-TW": "移除標籤 $1", en: "Remove label $1" },
  { pattern: /^移除 (.+)$/, "zh-TW": "移除 $1", en: "Remove $1" },
  { pattern: /^已将 (\d+) 封邮件移回收件箱$/, "zh-TW": "已將 $1 封郵件移回收件匣", en: "Moved $1 messages back to Inbox" },
  { pattern: /^发给 (.+)$/, "zh-TW": "寄給 $1", en: "To $1" },
  { pattern: /^来源：(.+)$/, "zh-TW": "來源：$1", en: "Source: $1" },
  { pattern: /^尝试：(\d+)\/(\d+)$/, "zh-TW": "嘗試：$1/$2", en: "Attempts: $1/$2" },
  { pattern: /^下次：(.+)$/, "zh-TW": "下次：$1", en: "Next: $1" },
  { pattern: /^投递于 (.+)$/, "zh-TW": "投遞於 $1", en: "Delivered at $1" },
  { pattern: /^尝试次数：(\d+)$/, "zh-TW": "嘗試次數：$1", en: "Attempts: $1" },
  { pattern: /^已分别发送 (\d+) 封邮件$/, "zh-TW": "已分別傳送 $1 封郵件", en: "Sent $1 messages separately" },
  { pattern: /^已定时发送 (.+)$/, "zh-TW": "已定時傳送 $1", en: "Scheduled for $1" },
  { pattern: /^草稿已保存 (.+)$/, "zh-TW": "草稿已儲存 $1", en: "Draft saved $1" },
  { pattern: /^发送时间：(.+)$/, "zh-TW": "傳送時間：$1", en: "Send time: $1" },
  { pattern: /^当前单个附件上限 (.+)$/, "zh-TW": "目前單個附件上限 $1", en: "Current per-attachment limit: $1" },
  { pattern: /^(\d+)分钟$/, "zh-TW": "$1分鐘", en: "$1 min" },
  { pattern: /^(\d+)分钟前$/, "zh-TW": "$1分鐘前", en: "$1 min before" },
  { pattern: /^(\d+) 分钟后$/, "zh-TW": "$1 分鐘後", en: "In $1 min" },
  { pattern: /^(\d+)小时$/, "zh-TW": "$1小時", en: "$1 hr" },
  { pattern: /^(\d+)小时前$/, "zh-TW": "$1小時前", en: "$1 hr before" },
  { pattern: /^(\d+) 小时后$/, "zh-TW": "$1 小時後", en: "In $1 hr" },
  { pattern: /^(\d+)天$/, "zh-TW": "$1天", en: "$1 day" },
  { pattern: /^(\d+)天前$/, "zh-TW": "$1天前", en: "$1 day before" },
  { pattern: /^(.+)前$/, "zh-TW": "$1前", en: "$1 before" },
]

export function translateUiText(value: string, language: Language): string {
  if (language === "zh-CN" || !value) return value
  const match = value.match(/^(\s*)([\s\S]*?)(\s*)$/)
  const leading = match?.[1] || ""
  const text = match?.[2] || value
  const trailing = match?.[3] || ""
  if (!text) return value
  const translated = translateCore(text, language)
  return `${leading}${translated}${trailing}`
}

function translateCore(text: string, language: Exclude<Language, "zh-CN">): string {
  const exact = exactTranslations[text]
  if (exact) return exact[language]
  for (const rule of templateTranslations) {
    if (rule.pattern.test(text)) return text.replace(rule.pattern, rule[language])
  }
  return text
}

const textSources = new WeakMap<Text, string>()
const textLastApplied = new WeakMap<Text, string>()
const attrSources = new WeakMap<Element, Partial<Record<string, string>>>()
const attrLastApplied = new WeakMap<Element, Partial<Record<string, string>>>()
const translatableAttributes = ["placeholder", "title", "aria-label"] as const
let translateTimer: number | undefined

export function LanguageDomSync() {
  const [language] = useLanguage()
  React.useEffect(() => {
    document.documentElement.lang = languageOptions.find((item) => item.value === language)?.htmlLang || language
    scheduleLocalize(language)
    const observer = new MutationObserver(() => scheduleLocalize(language))
    observer.observe(document.documentElement, { childList: true, subtree: true, characterData: true, attributes: true, attributeFilter: [...translatableAttributes] })
    return () => {
      observer.disconnect()
      if (translateTimer) window.clearTimeout(translateTimer)
    }
  }, [language])
  return null
}

function scheduleLocalize(language: Language) {
  if (typeof window === "undefined") return
  if (translateTimer) window.clearTimeout(translateTimer)
  translateTimer = window.setTimeout(() => localizeDocument(language), 0)
}

function localizeDocument(language: Language) {
  if (!document.body) return
  localizeElement(document.body, language)
}

function localizeElement(root: Element, language: Language) {
  if (shouldSkipElement(root)) return
  localizeAttributes(root, language)
  for (const child of Array.from(root.childNodes)) {
    if (child.nodeType === Node.TEXT_NODE) localizeTextNode(child as Text, language)
    else if (child.nodeType === Node.ELEMENT_NODE) localizeElement(child as Element, language)
  }
}

function localizeTextNode(node: Text, language: Language) {
  if (!node.parentElement || shouldSkipElement(node.parentElement)) return
  const current = node.textContent || ""
  if (!current.trim()) return
  let source = textSources.get(node)
  const last = textLastApplied.get(node)
  if (!source || (last !== undefined && current !== last && shouldTranslate(current))) {
    source = current
    textSources.set(node, source)
  }
  if (!source || !shouldTranslate(source)) return
  const next = translateUiText(source, language)
  textLastApplied.set(node, next)
  if (current !== next) node.textContent = next
}

function localizeAttributes(element: Element, language: Language) {
  if (shouldSkipElement(element)) return
  for (const attr of translatableAttributes) {
    const current = element.getAttribute(attr)
    if (!current || !current.trim()) continue
    let sources = attrSources.get(element)
    if (!sources) {
      sources = {}
      attrSources.set(element, sources)
    }
    let applied = attrLastApplied.get(element)
    if (!applied) {
      applied = {}
      attrLastApplied.set(element, applied)
    }
    if (!sources[attr] || (applied[attr] !== undefined && current !== applied[attr] && shouldTranslate(current))) sources[attr] = current
    const source = sources[attr]
    if (!source || !shouldTranslate(source)) continue
    const next = translateUiText(source, language)
    applied[attr] = next
    if (current !== next) element.setAttribute(attr, next)
  }
}

function shouldSkipElement(element: Element) {
  const tag = element.tagName.toLowerCase()
  if (["script", "style", "code", "pre", "textarea", "option"].includes(tag)) return true
  return Boolean(element.closest("[data-lanqin-i18n-ignore], [contenteditable='true'], .ProseMirror"))
}

function shouldTranslate(value: string) {
  const text = value.trim()
  if (!text) return false
  return Boolean(exactTranslations[text]) || templateTranslations.some((rule) => rule.pattern.test(text))
}
