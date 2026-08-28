import { useEffect, useRef } from 'react'

/** A small anchored popover menu, closing on Esc, outside click, or item select, with a basic focus trap. */
export function PopoverMenu({
  anchorClassName,
  onClose,
  children,
}: {
  anchorClassName: string
  onClose: () => void
  children: React.ReactNode
}) {
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const first = ref.current?.querySelector<HTMLElement>('[role="menuitem"]')
    first?.focus()

    function handlePointerDown(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose()
    }
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        e.preventDefault()
        onClose()
        return
      }
      if (e.key !== 'Tab' || !ref.current) return
      const items = Array.from(ref.current.querySelectorAll<HTMLElement>('[role="menuitem"]'))
      if (items.length === 0) return
      const first = items[0]
      const last = items[items.length - 1]
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault()
        first.focus()
      }
    }
    document.addEventListener('mousedown', handlePointerDown)
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('mousedown', handlePointerDown)
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [onClose])

  return (
    <div
      ref={ref}
      role="menu"
      className={
        'absolute z-40 min-w-[170px] rounded-md border border-rule bg-paper py-1 font-body normal-case tracking-normal shadow-lg ' +
        anchorClassName
      }
    >
      {children}
    </div>
  )
}

export function MenuItem({ onClick, danger, children }: { onClick: () => void; danger?: boolean; children: React.ReactNode }) {
  return (
    <button
      type="button"
      role="menuitem"
      onClick={onClick}
      className={
        'block w-full px-3 py-1.5 text-left text-sm hover:bg-paper-3 ' + (danger ? 'text-red-600' : 'text-ink-2')
      }
    >
      {children}
    </button>
  )
}
