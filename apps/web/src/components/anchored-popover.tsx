import * as React from "react"
import { createPortal } from "react-dom"

import { cn } from "@/lib/utils"

type PopoverPlacement = "right-start" | "bottom-start"

type PopoverPosition = {
  left: number
  top: number
  arrowOffset: number
  arrowSide: "left" | "right" | "top" | "bottom"
  ready: boolean
}

export function AnchoredPopover({
  anchorRef,
  open,
  onOpenChange,
  children,
  className,
  placement = "right-start",
  offset = 8,
  collisionPadding = 12,
  alignTopSelector,
  role = "dialog",
  ariaLabel,
  id,
}: {
  anchorRef: React.RefObject<HTMLElement>
  open: boolean
  onOpenChange: (open: boolean) => void
  children: React.ReactNode
  className?: string
  placement?: PopoverPlacement
  offset?: number
  collisionPadding?: number
  alignTopSelector?: string
  role?: "dialog" | "menu"
  ariaLabel: string
  id: string
}) {
  const contentRef = React.useRef<HTMLDivElement>(null)
  const [position, setPosition] = React.useState<PopoverPosition>({ left: 0, top: 0, arrowOffset: 20, arrowSide: "left", ready: false })

  const updatePosition = React.useCallback(() => {
    const anchor = anchorRef.current
    const content = contentRef.current
    if (!anchor || !content) return

    const anchorRect = anchor.getBoundingClientRect()
    const contentRect = content.getBoundingClientRect()
    const shellRect = anchor.closest(".mail-window")?.getBoundingClientRect()
    const boundary = shellRect || { left: 0, top: 0, right: window.innerWidth, bottom: window.innerHeight }
    const minLeft = boundary.left + collisionPadding
    const maxLeft = boundary.right - collisionPadding - contentRect.width
    const minTop = boundary.top + collisionPadding
    const maxTop = boundary.bottom - collisionPadding - contentRect.height
    let left = anchorRect.right + offset
    let top = anchorRect.top
    let arrowSide: PopoverPosition["arrowSide"] = "left"

    if (placement === "right-start") {
      const aligned = alignTopSelector ? anchor.closest(".mail-window")?.querySelector<HTMLElement>(alignTopSelector) : null
      top = aligned?.getBoundingClientRect().top ?? anchorRect.top
      if (left + contentRect.width > boundary.right - collisionPadding) {
        left = anchorRect.left - contentRect.width - offset
        arrowSide = "right"
      }
      left = Math.min(Math.max(left, minLeft), Math.max(minLeft, maxLeft))
      top = Math.min(Math.max(top, minTop), Math.max(minTop, maxTop))
      const arrowOffset = Math.min(Math.max(anchorRect.top + anchorRect.height / 2 - top, 18), Math.max(18, contentRect.height - 18))
      setPosition({ left, top, arrowOffset, arrowSide, ready: true })
      return
    }

    left = anchorRect.left
    top = anchorRect.bottom + offset
    arrowSide = "top"
    if (top + contentRect.height > boundary.bottom - collisionPadding) {
      top = anchorRect.top - contentRect.height - offset
      arrowSide = "bottom"
    }
    left = Math.min(Math.max(left, minLeft), Math.max(minLeft, maxLeft))
    top = Math.min(Math.max(top, minTop), Math.max(minTop, maxTop))
    const arrowOffset = Math.min(Math.max(anchorRect.left + anchorRect.width / 2 - left, 18), Math.max(18, contentRect.width - 18))
    setPosition({ left, top, arrowOffset, arrowSide, ready: true })
  }, [alignTopSelector, anchorRef, collisionPadding, offset, placement])

  React.useLayoutEffect(() => {
    if (!open) return
    setPosition((current) => ({ ...current, ready: false }))
    const frame = window.requestAnimationFrame(updatePosition)
    window.addEventListener("resize", updatePosition)
    window.addEventListener("scroll", updatePosition, true)
    return () => {
      window.cancelAnimationFrame(frame)
      window.removeEventListener("resize", updatePosition)
      window.removeEventListener("scroll", updatePosition, true)
    }
  }, [open, updatePosition])

  React.useEffect(() => {
    if (!open) return
    const closeOnPointerDown = (event: PointerEvent) => {
      const target = event.target as Node | null
      if (!target || contentRef.current?.contains(target) || anchorRef.current?.contains(target)) return
      onOpenChange(false)
    }
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return
      event.preventDefault()
      onOpenChange(false)
    }
    document.addEventListener("pointerdown", closeOnPointerDown)
    document.addEventListener("keydown", closeOnEscape)
    return () => {
      document.removeEventListener("pointerdown", closeOnPointerDown)
      document.removeEventListener("keydown", closeOnEscape)
    }
  }, [anchorRef, onOpenChange, open])

  React.useEffect(() => {
    if (!open || !position.ready) return
    const target = contentRef.current?.querySelector<HTMLElement>("[data-popover-autofocus], input, [role='menuitem'], button")
    target?.focus({ preventScroll: true })
  }, [open, position.ready])

  const wasOpen = React.useRef(false)
  React.useEffect(() => {
    if (wasOpen.current && !open) anchorRef.current?.focus({ preventScroll: true })
    wasOpen.current = open
  }, [anchorRef, open])

  if (!open || typeof document === "undefined") return null

  return createPortal(
    <div
      ref={contentRef}
      id={id}
      role={role}
      aria-label={ariaLabel}
      className={cn("anchored-popover", className)}
      data-arrow-side={position.arrowSide}
      style={{
        left: position.left,
        top: position.top,
        visibility: position.ready ? "visible" : "hidden",
        "--anchored-arrow-offset": `${position.arrowOffset}px`,
      } as React.CSSProperties}
    >
      <span className="anchored-popover-arrow" aria-hidden="true" />
      {children}
    </div>,
    document.body,
  )
}
