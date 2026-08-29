import { useEffect, useMemo, useRef, useState } from 'react'
import { EMOJIS, type EmojiEntry } from '../data/emojis'
import { PopoverMenu } from './PopoverMenu'

const COLUMNS = 8

/** A searchable, keyboard-navigable emoji picker anchored via `PopoverMenu`'s CSS-relative
 * positioning — shared by the reaction "+" trigger and the composer's 🙂 button and
 * `:shortcode` autocomplete (SPEC.md §6.4/§6.5). */
export function EmojiPicker({
  anchorClassName,
  initialQuery,
  autoFocusSearch = true,
  onSelect,
  onDismiss,
}: {
  anchorClassName: string
  initialQuery?: string
  /** False for the composer's `:shortcode` inline mode, where the textarea (not this picker's
   * own search input) must keep keyboard focus so the user can keep typing the shortcode. */
  autoFocusSearch?: boolean
  onSelect: (emoji: string) => void
  onDismiss: () => void
}) {
  const [query, setQuery] = useState(initialQuery ?? '')
  const [index, setIndex] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (autoFocusSearch) inputRef.current?.focus()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    if (initialQuery !== undefined) setQuery(initialQuery)
  }, [initialQuery])

  const results = useMemo<EmojiEntry[]>(() => {
    const q = query.trim().toLowerCase()
    if (!q) return EMOJIS
    return EMOJIS.filter((e) => e.name.includes(q) || e.keywords.some((k) => k.includes(q)))
  }, [query])

  useEffect(() => {
    setIndex(0)
  }, [results.length])

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'ArrowRight') {
        e.preventDefault()
        setIndex((i) => Math.min(i + 1, results.length - 1))
      } else if (e.key === 'ArrowLeft') {
        e.preventDefault()
        setIndex((i) => Math.max(i - 1, 0))
      } else if (e.key === 'ArrowDown') {
        e.preventDefault()
        setIndex((i) => Math.min(i + COLUMNS, results.length - 1))
      } else if (e.key === 'ArrowUp') {
        e.preventDefault()
        setIndex((i) => Math.max(i - COLUMNS, 0))
      } else if (e.key === 'Tab' || e.key === 'Enter') {
        e.preventDefault()
        if (results[index]) onSelect(results[index].char)
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
      <div className="w-72 p-2">
        {autoFocusSearch ? (
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search emoji…"
            className="w-full rounded border border-rule bg-paper-2 px-2 py-1 text-sm text-ink outline-none focus:border-teal"
          />
        ) : (
          <div className="px-1 py-1 font-mono text-[10.5px] text-ink-3">Matching “:{query}”</div>
        )}
        {results.length === 0 ? (
          <div className="flex flex-col items-center gap-2 py-6 text-center text-sm text-ink-3">
            <span>No matching emojis found.</span>
            <button
              type="button"
              onClick={() => setQuery('')}
              className="rounded border border-rule px-2 py-1 text-xs text-ink-2 hover:bg-paper-3"
            >
              Clear Search
            </button>
          </div>
        ) : (
          <div role="listbox" className="mt-2 grid max-h-56 grid-cols-8 gap-0.5 overflow-y-auto">
            {results.map((e, i) => (
              <button
                key={e.char}
                type="button"
                role="option"
                aria-selected={i === index}
                title={e.name}
                onMouseEnter={() => setIndex(i)}
                onClick={() => onSelect(e.char)}
                className={'grid h-8 w-8 place-items-center rounded text-lg ' + (i === index ? 'bg-teal-soft' : 'hover:bg-paper-2')}
              >
                {e.char}
              </button>
            ))}
          </div>
        )}
      </div>
    </PopoverMenu>
  )
}
