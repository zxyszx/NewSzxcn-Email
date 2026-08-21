import { cn } from "@/lib/utils"

export function BrandMark({ className }: { className?: string }) {
  return (
    <span className={cn("grid size-9 shrink-0 place-items-center overflow-hidden rounded-md border border-black/10 bg-white text-black shadow-sm", className)} aria-hidden="true">
      <img src="/brand-glyph.svg" alt="" className="size-[72%]" />
    </span>
  )
}
