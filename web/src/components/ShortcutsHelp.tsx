import { useUiStore } from '../store/ui'
import { SHORTCUTS, type ShortcutDef } from '../hooks/useGlobalShortcuts'
import { Modal } from './Modal'

function groupByCategory(shortcuts: ShortcutDef[]): Map<string, ShortcutDef[]> {
  const groups = new Map<string, ShortcutDef[]>()
  for (const s of shortcuts) {
    const list = groups.get(s.category) ?? []
    list.push(s)
    groups.set(s.category, list)
  }
  return groups
}

export function ShortcutsHelp() {
  const open = useUiStore((s) => s.shortcutsHelpOpen)
  const close = useUiStore((s) => s.closeShortcutsHelp)

  if (!open) return null

  const groups = groupByCategory(SHORTCUTS)

  return (
    <Modal open={open} onClose={close} labelledBy="shortcuts-help-label">
      <div className="max-h-[52vh] overflow-y-auto p-4">
        <h2 id="shortcuts-help-label" className="mb-3 font-display text-lg font-semibold text-ink">
          Keyboard shortcuts
        </h2>
        {[...groups.entries()].map(([category, shortcuts]) => (
          <div key={category} className="mb-4 last:mb-0">
            <div className="lbl mb-1">{category}</div>
            <dl className="flex flex-col gap-1">
              {shortcuts.map((s) => (
                <div key={s.combo} className="flex items-center justify-between gap-4 text-sm">
                  <dt className="text-ink-2">{s.description}</dt>
                  <dd className="shrink-0 rounded bg-paper-2 px-1.5 py-0.5 font-mono text-[11px] text-ink">
                    {s.combo}
                  </dd>
                </div>
              ))}
            </dl>
          </div>
        ))}
      </div>
    </Modal>
  )
}
