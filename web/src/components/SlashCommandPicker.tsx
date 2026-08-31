import { useEffect, useMemo, useState } from 'react'
import { type SlashCommandSummary } from '../lib/api'
import { fuzzySearch } from '../lib/fuzzy'
import { PopoverMenu } from './PopoverMenu'

/** The composer's `/`-triggered slash-command autocomplete, modeled directly on EmojiPicker's
 * keyboard-navigable, PopoverMenu-anchored shape (SPEC.md §6.x). Unlike the emoji picker, the
 * textarea itself keeps typing focus — this menu only renders and listens for navigation keys. */
export function SlashCommandPicker({
  query,
  commands,
  anchorClassName,
  onSelect,
  onDismiss,
}: {
  query: string
  commands: SlashCommandSummary[]
  anchorClassName: string
  onSelect: (command: SlashCommandSummary) => void
  onDismiss: () => void
}) {
  const [index, setIndex] = useState(0)

  const results = useMemo(() => {
    if (!query) return commands
    const byTrigger = fuzzySearch(query, commands, (c) => c.trigger)
    const byDescription = fuzzySearch(query, commands, (c) => c.description)
    const seen = new Set<string>()
    const merged: SlashCommandSummary[] = []
    for (const m of [...byTrigger, ...byDescription]) {
      if (seen.has(m.item.trigger)) continue
      seen.add(m.item.trigger)
      merged.push(m.item)
    }
    return merged
  }, [query, commands])

  useEffect(() => {
    setIndex(0)
  }, [results.length])

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        setIndex((i) => Math.min(i + 1, results.length - 1))
      } else if (e.key === 'ArrowUp') {
        e.preventDefault()
        setIndex((i) => Math.max(i - 1, 0))
      } else if (e.key === 'Tab' || e.key === 'Enter') {
        e.preventDefault()
        if (results[index]) onSelect(results[index])
      } else if (e.key === 'Escape') {
        e.preventDefault()
        onDismiss()
      }
    }
    window.addEventListener('keydown', handler, true)
    return () => window.removeEventListener('keydown', handler, true)
  }, [results, index, onSelect, onDismiss])

  return (
    <PopoverMenu anchorClassName={anchorClassName} onClose={onDismiss}>
      <div className="w-80 p-1">
        {results.length === 0 ? (
          <div className="flex flex-col items-center gap-1 py-6 text-center text-sm text-ink-3">
            <span>No matching commands found.</span>
            <span className="font-mono text-[10.5px]">Press Esc to exit.</span>
          </div>
        ) : (
          <div role="listbox" className="max-h-64 overflow-y-auto">
            {results.map((c, i) => (
              <button
                key={c.trigger}
                type="button"
                role="option"
                aria-selected={i === index}
                onMouseEnter={() => setIndex(i)}
                onClick={() => onSelect(c)}
                className={
                  'flex w-full flex-col items-start gap-0.5 rounded px-2.5 py-1.5 text-left ' +
                  (i === index ? 'bg-teal-soft' : 'hover:bg-paper-2')
                }
              >
                <span className="flex items-center gap-2">
                  <span className="font-mono text-sm font-semibold text-ink">{c.trigger}</span>
                  {c.admin_only && (
                    <span className="rounded bg-paper-3 px-1 font-mono text-[8px] text-ink-2">ADMIN</span>
                  )}
                </span>
                <span className="truncate text-[12px] text-ink-3">{c.description}</span>
              </button>
            ))}
          </div>
        )}
      </div>
    </PopoverMenu>
  )
}
