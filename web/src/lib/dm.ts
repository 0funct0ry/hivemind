import type { DM } from './api'

/** The display name for a DM row: the peer's name for a 1:1 DM, the server-computed
 * "Bruce, Hugo, +2"-style name for a group DM. */
export function dmDisplayName(d: DM): string {
  if (d.kind === 'group_dm') return d.name || 'Group'
  return d.peer?.display_name || d.peer?.username || 'Unknown'
}

/** Whether a DM row should show as online: the peer's presence for a 1:1 DM, "is anyone in
 * the group online" for a group DM. */
export function dmIsOnline(d: DM, online: Set<string>): boolean {
  if (d.kind === 'group_dm') return (d.members ?? []).some((m) => online.has(m.id))
  return !!d.peer && online.has(d.peer.id)
}
