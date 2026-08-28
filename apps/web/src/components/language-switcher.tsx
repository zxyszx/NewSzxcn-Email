import { Check } from "lucide-react"

import { Button, type ButtonProps } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { languageOptions, useLanguage } from "@/lib/language"
import { cn } from "@/lib/utils"

type LanguageSwitcherProps = {
  className?: string
  variant?: ButtonProps["variant"]
}

export function LanguageSwitcher({ className, variant = "ghost" }: LanguageSwitcherProps) {
  const [language, setLanguage] = useLanguage()
  const currentLanguage = languageOptions.find((item) => item.value === language) || languageOptions[0]

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          type="button"
          variant={variant}
          size="icon"
          className={cn("size-7 shrink-0 rounded-md text-muted-foreground", className)}
          aria-label="切换语言"
          title="切换语言"
        >
          <span className="text-xs font-semibold leading-none">{currentLanguage.shortLabel}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-40">
        {languageOptions.map((item) => (
          <DropdownMenuItem key={item.value} onSelect={() => setLanguage(item.value)} className="gap-2">
            <span className="min-w-0 flex-1">{item.label}</span>
            <Check className={cn("h-4 w-4 text-emerald-500", item.value === language ? "opacity-100" : "opacity-0")} />
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
