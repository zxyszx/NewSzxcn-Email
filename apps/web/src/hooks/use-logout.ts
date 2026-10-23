import { useCallback } from "react"
import { useNavigate } from "react-router-dom"
import { useQueryClient } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { useToast } from "@/hooks/use-toast"

export function useLogout() {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const { toast } = useToast()
  return useCallback(async () => {
    try {
      await api.logout()
      qc.clear()
      navigate("/login", { replace: true })
    } catch (error) {
      toast({ title: "退出失败，会话仍有效", description: error instanceof Error ? error.message : "请检查网络后重试", variant: "destructive" })
    }
  }, [qc, navigate, toast])
}
