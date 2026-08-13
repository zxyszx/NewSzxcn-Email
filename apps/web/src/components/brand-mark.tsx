import { Mail } from "lucide-react"

import { cn } from "@/lib/utils"

export function BrandMark({ className }: { className?: string }) {
  return (
    <span className={cn("grid size-9 shrink-0 place-items-center rounded-md border border-primary/20 bg-primary/[0.03] text-primary", className)} aria-hidden="true">
      <Mail className="size-6 stroke-[1.8]" />
    </span>
  )
}
