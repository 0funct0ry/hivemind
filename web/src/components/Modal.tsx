import { useEffect, useRef } from 'react'
import { createPortal } from 'react-dom'

const FOCUSABLE_SELECTOR =
  'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])'

let openModalCount = 0

export function Modal({
  open,
  onClose,
  labelledBy,
  initialFocusRef,
  children,
  className,
}: {
  open: boolean
  onClose: () => void
  labelledBy: string
  initialFocusRef?: React.RefObject<HTMLElement | null>
  children: React.ReactNode
  className?: string
}) {
  const dialogRef = useRef<HTMLDivElement>(null)
  const previouslyFocused = useRef<Element | null>(null)

  useEffect(() => {
    if (!open) return

    previouslyFocused.current = document.activeElement
    openModalCount += 1
    document.body.style.overflow = 'hidden'

    const toFocus = initialFocusRef?.current ?? dialogRef.current
    toFocus?.focus()

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key !== 'Tab' || !dialogRef.current) return
      const focusable = Array.from(dialogRef.current.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR))
      if (focusable.length === 0) return
      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault()
        first.focus()
      }
    }
    document.addEventListener('keydown', handleKeyDown)

    return () => {
      document.removeEventListener('keydown', handleKeyDown)
      openModalCount = Math.max(0, openModalCount - 1)
      if (openModalCount === 0) document.body.style.overflow = ''
      if (previouslyFocused.current instanceof HTMLElement) previouslyFocused.current.focus()
    }
  }, [open, initialFocusRef])

  if (!open) return null

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-start justify-center bg-black/40 pt-[15vh]" onClick={onClose}>
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={labelledBy}
        tabIndex={-1}
        onClick={(e) => e.stopPropagation()}
        className={
          'max-h-[70vh] w-full max-w-lg overflow-auto rounded-lg border border-rule bg-paper shadow-xl outline-none ' +
          (className ?? '')
        }
      >
        {children}
      </div>
    </div>,
    document.body,
  )
}
