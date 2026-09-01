import React from "react"
import { Navigate, useLocation } from "react-router-dom"
import { useMe } from "@/hooks/use-me"
import { AuthLoading, AuthError } from "@/components/auth-states"
import { isUnauthorizedError } from "@/lib/api"

export function AuthGuard({ children }: { children: React.ReactNode }) {
  const me = useMe()
  const location = useLocation()

  if (me.isLoading) return <AuthLoading />
  if (me.isError && !isUnauthorizedError(me.error)) return <AuthError message={me.error.message} onRetry={() => me.refetch()} />
  if (me.isError || !me.data?.user) {
    const from = `${location.pathname}${location.search}${location.hash}`
    return <Navigate to="/login" replace state={{ from }} />
  }

  return <>{children}</>
}
