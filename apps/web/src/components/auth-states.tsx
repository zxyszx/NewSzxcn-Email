import { Button } from "@/components/ui/button"

export function AuthLoading() {
  return <main className="grid min-h-screen place-items-center text-muted-foreground">加载中...</main>
}

export function AuthError({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <main className="grid min-h-screen place-items-center bg-background px-4">
      <div className="w-full max-w-sm space-y-4 text-center">
        <div className="text-sm font-medium">服务暂时不可用</div>
        <div className="text-sm text-muted-foreground">{message}</div>
        <Button type="button" variant="outline" onClick={onRetry}>重新连接</Button>
      </div>
    </main>
  )
}
