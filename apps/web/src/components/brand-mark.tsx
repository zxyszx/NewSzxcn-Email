import { cn } from "@/lib/utils"

export function BrandMark({ className }: { className?: string }) {
  return (
    <span className={cn("grid size-9 shrink-0 place-items-center overflow-hidden rounded-[22%] shadow-sm", className)} aria-hidden="true">
      <img src="/brand-icon.png?v=4" alt="" className="size-full object-cover" />
    </span>
  )
}
