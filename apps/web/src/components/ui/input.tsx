import * as React from "react"

import { cn } from "@/lib/utils"

export type InputProps = React.ComponentProps<"input"> & {
  allowPasswordManager?: boolean
}

const Input = React.forwardRef<HTMLInputElement, InputProps>(
  ({ className, type, allowPasswordManager = false, ...props }, ref) => {
    return (
      <input
        type={type}
        {...(!allowPasswordManager
          ? {
              "data-1p-ignore": "true",
              "data-lpignore": "true",
              "data-bwignore": "true",
            }
          : {})}
        className={cn(
          "flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-base shadow-sm transition-colors file:border-0 file:bg-transparent file:text-sm file:font-medium file:text-foreground placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:cursor-not-allowed disabled:opacity-50 md:text-sm",
          className
        )}
        ref={ref}
        {...props}
      />
    )
  }
)
Input.displayName = "Input"

export { Input }
