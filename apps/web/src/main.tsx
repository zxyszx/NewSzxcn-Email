import "@/lib/polyfills"
import React from "react"
import ReactDOM from "react-dom/client"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { Navigate, RouterProvider, createBrowserRouter } from "react-router-dom"
import { Toaster } from "@/components/ui/toaster"
import { LanguageDomSync } from "@/lib/language"
import { applyTheme, getInitialTheme } from "@/lib/theme"
import { ProtectedLayout } from "@/components/protected-layout"
import { AdminOnly } from "@/components/admin-only"
import "./index.css"

const LoginPage = React.lazy(() => import("@/pages/login").then((module) => ({ default: module.LoginPage })))
const RegisterPage = React.lazy(() => import("@/pages/register").then((module) => ({ default: module.RegisterPage })))
const MailPage = React.lazy(() => import("@/pages/mail").then((module) => ({ default: module.MailPage })))
const AdminPage = React.lazy(() => import("@/pages/admin").then((module) => ({ default: module.AdminPage })))
const ProfilePage = React.lazy(() => import("@/pages/profile").then((module) => ({ default: module.ProfilePage })))
const NotFoundPage = React.lazy(() => import("@/pages/not-found").then((module) => ({ default: module.NotFoundPage })))

applyTheme(getInitialTheme())

const queryClient = new QueryClient({ defaultOptions: { queries: { refetchOnWindowFocus: false, staleTime: 10_000 } } })
const router = createBrowserRouter([
  { path: "/login", element: <LoginPage /> },
  { path: "/register", element: <RegisterPage /> },
  { path: "/", element: <ProtectedLayout />, children: [
    { index: true, element: <MailPage /> },
    { path: "mail", element: <Navigate to="/" replace /> },
    { path: "mail/starred", element: <Navigate to="/" replace /> },
    { path: "profile", element: <ProfilePage /> },
    { path: "admin", element: <AdminOnly><AdminPage /></AdminOnly> },
  ] },
  { path: "*", element: <NotFoundPage /> },
])

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <React.Suspense fallback={<div className="grid h-svh place-items-center text-sm text-muted-foreground">加载中...</div>}>
        <RouterProvider router={router} />
      </React.Suspense>
      <Toaster />
      <LanguageDomSync />
    </QueryClientProvider>
  </React.StrictMode>,
)
