import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from 'react'
import { ApiError, api, type Message, type UploadedFile } from '../lib/api'
import { newClientMsgId, useEditMessage, useMessages, useSendMessage } from '../hooks/useMessages'
import { useUiStore } from '../store/ui'
import { wsClient } from '../lib/ws'
import { throttle } from '../lib/throttle'
import { MentionPicker, useMentionCandidates } from './MentionPicker'
import { fileTypeAbbrev } from '../lib/fileType'

const EDIT_WINDOW_MS = 15 * 60 * 1000

export interface ComposerHandle {
  /** Enters edit mode on the given message, stashing the current draft to restore on Esc. */
  startEdit: (message: Message) => void
}

interface UploadChip {
  id: string
  name: string
  size: number
  progress: number
  file?: UploadedFile
  error?: string
}

function draftKey(channelId: string): string {
  return `hivemind:draft:${channelId}`
}

export const Composer = forwardRef<
  ComposerHandle,
  {
    channelId: string
    threadId?: string
    placeholder?: string
    onSent?: () => void
    currentUserId?: string
  }
>(function Composer({ channelId, threadId, placeholder, onSent, currentUserId }, ref) {
  const setDraft = useUiStore((s) => s.setDraft)
  const drafts = useUiStore((s) => s.drafts)
  const sendMutation = useSendMessage(channelId)
  const editMutation = useEditMessage(channelId)
  const { messages: channelMessages } = useMessages(threadId ? undefined : channelId)

  const [body, setBody] = useState('')
  const [uploads, setUploads] = useState<UploadChip[]>([])
  const [alsoSendToChannel, setAlsoSendToChannel] = useState(false)
  const [mentionQuery, setMentionQuery] = useState<string | null>(null)
  const [mentionStart, setMentionStart] = useState(0)
  const [editing, setEditing] = useState<{ id: string } | null>(null)
  const [stashedDraft, setStashedDraft] = useState<string | null>(null)
  const [editBanner, setEditBanner] = useState<string | null>(null)

  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const draftStorageKey = threadId ? `${draftKey(channelId)}:thread:${threadId}` : draftKey(channelId)

  useEffect(() => {
    const stored = window.localStorage.getItem(draftStorageKey)
    if (stored) setBody(stored)
    else if (!threadId) setBody(drafts[channelId] ?? '')
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [draftStorageKey])

  useEffect(() => {
    const ta = textareaRef.current
    if (ta) {
      ta.style.height = 'auto'
      ta.style.height = `${Math.min(ta.scrollHeight, 240)}px`
    }
  }, [body])

  const persistDraft = (value: string) => {
    if (editing) return // don't clobber the real draft with in-progress edit text
    if (value) window.localStorage.setItem(draftStorageKey, value)
    else window.localStorage.removeItem(draftStorageKey)
    if (!threadId) setDraft(channelId, value)
  }

  const startEdit = (message: Message) => {
    setStashedDraft(body)
    setEditing({ id: message.id })
    setEditBanner(null)
    setBody(message.body)
    requestAnimationFrame(() => textareaRef.current?.focus())
  }

  const cancelEdit = () => {
    setEditing(null)
    setEditBanner(null)
    setBody(stashedDraft ?? '')
    setStashedDraft(null)
  }

  useImperativeHandle(ref, () => ({ startEdit }))

  const sendTyping = useRef(
    throttle(() => wsClient.send('typing', { channel_id: channelId }), 3000),
  ).current

  const mentionCandidates = useMentionCandidates(channelId, mentionQuery ?? '')

  const handleChange = (value: string) => {
    setBody(value)
    persistDraft(value)
    sendTyping()

    const cursor = textareaRef.current?.selectionStart ?? value.length
    const upToCursor = value.slice(0, cursor)
    const match = /@([a-z0-9._-]*)$/i.exec(upToCursor)
    if (match) {
      setMentionQuery(match[1])
      setMentionStart(cursor - match[1].length - 1)
    } else {
      setMentionQuery(null)
    }
  }

  const openMentionPickerAtCursor = () => {
    const ta = textareaRef.current
    const cursor = ta?.selectionStart ?? body.length
    const before = body.slice(0, cursor)
    const needsSpace = before.length > 0 && !/\s$/.test(before)
    const insertion = `${needsSpace ? ' ' : ''}@`
    const next = `${before}${insertion}${body.slice(cursor)}`
    setBody(next)
    persistDraft(next)
    const newCursor = cursor + insertion.length
    setMentionQuery('')
    setMentionStart(newCursor - 1)
    requestAnimationFrame(() => {
      ta?.focus()
      ta?.setSelectionRange(newCursor, newCursor)
    })
  }

  const toggleCodeBlock = () => {
    const ta = textareaRef.current
    if (!ta) return
    const start = ta.selectionStart
    const end = ta.selectionEnd
    const selected = body.slice(start, end)
    const fenced = selected ? '```\n' + selected + '\n```' : '```\n\n```'
    const next = body.slice(0, start) + fenced + body.slice(end)
    setBody(next)
    persistDraft(next)
    const cursor = selected ? start + fenced.length : start + 4
    requestAnimationFrame(() => {
      ta.focus()
      ta.setSelectionRange(cursor, cursor)
    })
  }

  const insertMention = (name: string) => {
    const cursor = textareaRef.current?.selectionStart ?? body.length
    const next = `${body.slice(0, mentionStart)}@${name} ${body.slice(cursor)}`
    setBody(next)
    persistDraft(next)
    setMentionQuery(null)
    requestAnimationFrame(() => textareaRef.current?.focus())
  }

  const handleFiles = async (files: FileList | File[]) => {
    for (const file of Array.from(files)) {
      const chipId = `${file.name}-${Date.now()}-${Math.random()}`
      setUploads((u) => [...u, { id: chipId, name: file.name, size: file.size, progress: 0 }])
      try {
        const uploaded = await api.uploadFile(file, (pct) => {
          setUploads((u) => u.map((c) => (c.id === chipId ? { ...c, progress: pct } : c)))
        })
        setUploads((u) => u.map((c) => (c.id === chipId ? { ...c, progress: 100, file: uploaded } : c)))
      } catch (err) {
        setUploads((u) =>
          u.map((c) => (c.id === chipId ? { ...c, error: err instanceof Error ? err.message : 'Upload failed' } : c)),
        )
      }
    }
  }

  const removeUpload = (id: string) => setUploads((u) => u.filter((c) => c.id !== id))

  const canSend = editing
    ? body.trim().length > 0
    : (body.trim().length > 0 || uploads.some((u) => u.file)) && !uploads.some((u) => !u.file && !u.error)

  const handleSaveEdit = () => {
    if (!editing || body.trim().length === 0) return
    editMutation.mutate(
      { id: editing.id, body: body.trim() },
      {
        onSuccess: () => {
          setEditing(null)
          setEditBanner(null)
          setBody(stashedDraft ?? '')
          setStashedDraft(null)
        },
        onError: (err) => {
          if (err instanceof ApiError && err.code === 'edit_window_expired') {
            setEditBanner('This message can no longer be edited (15-minute window expired).')
          } else {
            setEditBanner(err instanceof Error ? err.message : 'Could not save the edit.')
          }
        },
      },
    )
  }

  const handleSend = () => {
    if (editing) {
      handleSaveEdit()
      return
    }
    if (!canSend) return
    const fileIds = uploads.filter((u) => u.file).map((u) => u.file!.id)
    sendMutation.mutate({
      body: body.trim(),
      threadId,
      fileIds: fileIds.length ? fileIds : undefined,
      alsoSendToChannel: threadId ? alsoSendToChannel : undefined,
      clientMsgId: newClientMsgId(),
    })
    setBody('')
    setUploads([])
    setAlsoSendToChannel(false)
    persistDraft('')
    onSent?.()
  }

  const handleEditLastMessage = () => {
    if (!currentUserId) return
    const cutoff = Date.now() - EDIT_WINDOW_MS
    for (let i = channelMessages.length - 1; i >= 0; i--) {
      const m = channelMessages[i]
      if (m.user_id === currentUserId && !m.deleted_at && m.created_at > cutoff && !m.id.startsWith('optimistic-')) {
        startEdit(m)
        return
      }
    }
  }

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (mentionQuery !== null && ['ArrowDown', 'ArrowUp', 'Tab', 'Enter', 'Escape'].includes(e.key)) {
      return
    }
    if (e.key === 'Escape' && editing) {
      e.preventDefault()
      cancelEdit()
      return
    }
    if (e.key === 'ArrowUp' && body === '' && !editing) {
      e.preventDefault()
      handleEditLastMessage()
      return
    }
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSend()
    }
  }

  return (
    <div className="relative border-t border-rule bg-paper p-3">
      {mentionQuery !== null && (
        <MentionPicker
          candidates={mentionCandidates}
          onSelect={(c) => insertMention(c.key)}
          onDismiss={() => setMentionQuery(null)}
        />
      )}
      {editing && !editBanner && (
        <div className="mb-1.5 flex items-center justify-between px-1 font-mono text-[10.5px] text-ink-3">
          <span>Editing message (Press Esc to cancel)</span>
        </div>
      )}
      {editBanner && (
        <div className="mb-1.5 rounded-md border border-pollen bg-pollen-soft px-2.5 py-1.5 text-[12.5px] text-[#7A4E00]">
          {editBanner}
        </div>
      )}
      <div
        className="rounded-md border border-rule bg-paper"
        onDragOver={(e) => e.preventDefault()}
        onDrop={(e) => {
          e.preventDefault()
          if (e.dataTransfer.files.length) void handleFiles(e.dataTransfer.files)
        }}
      >
        {uploads.length > 0 && (
          <div className="flex flex-wrap gap-2 p-2">
            {uploads.map((u) => (
              <div key={u.id} className="flex items-center gap-2 rounded bg-paper-2 px-2 py-1 text-xs">
                <span className="grid h-[18px] w-[18px] shrink-0 place-items-center rounded bg-paper-3 font-mono text-[7px] text-ink-2">
                  {fileTypeAbbrev(u.name)}
                </span>
                <span className="max-w-[140px] truncate">{u.name}</span>
                {u.error ? (
                  <span className="text-red-600">{u.error}</span>
                ) : u.progress < 100 ? (
                  <div className="h-1 w-16 overflow-hidden rounded bg-paper-3">
                    <div className="h-full bg-teal" style={{ width: `${u.progress}%` }} />
                  </div>
                ) : (
                  <span className="text-teal">✓</span>
                )}
                <button type="button" onClick={() => removeUpload(u.id)} aria-label="Remove attachment" className="text-ink-3">
                  ×
                </button>
              </div>
            ))}
          </div>
        )}
        <textarea
          ref={textareaRef}
          value={body}
          onChange={(e) => handleChange(e.target.value)}
          onKeyDown={handleKeyDown}
          onPaste={(e) => {
            if (e.clipboardData.files.length) void handleFiles(e.clipboardData.files)
          }}
          placeholder={placeholder ?? 'Message'}
          rows={1}
          className="w-full resize-none appearance-none bg-transparent px-3 py-2 text-sm text-ink shadow-none outline-none focus:shadow-none focus:outline-none focus-visible:shadow-none focus-visible:outline-none"
        />
        <div className="flex items-center justify-between px-3 pb-2">
          <div className="flex items-center gap-2">
            <label className="cursor-pointer text-ink-3 hover:text-ink">
              📎
              <input
                type="file"
                multiple
                className="hidden"
                onChange={(e) => {
                  if (e.target.files) void handleFiles(e.target.files)
                  e.target.value = ''
                }}
              />
            </label>
            <button
              type="button"
              title="Mention someone"
              aria-label="Mention someone"
              onClick={openMentionPickerAtCursor}
              className="px-1 text-ink-3 hover:text-ink"
            >
              @
            </button>
            <button
              type="button"
              title="Code block"
              aria-label="Code block"
              onClick={toggleCodeBlock}
              className="px-1 font-mono text-xs text-ink-3 hover:text-ink"
            >
              {'</>'}
            </button>
            {threadId && (
              <label className="flex items-center gap-1 font-mono text-[11px] text-ink-2">
                <input
                  type="checkbox"
                  checked={alsoSendToChannel}
                  onChange={(e) => setAlsoSendToChannel(e.target.checked)}
                />
                Also send to channel
              </label>
            )}
          </div>
          {editing ? (
            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={cancelEdit}
                className="rounded border border-rule px-3 py-1 text-sm text-ink-2 hover:bg-paper-3"
              >
                Cancel
              </button>
              <button
                type="button"
                disabled={!canSend || editMutation.isPending}
                onClick={handleSend}
                className="rounded bg-teal px-3 py-1 text-sm font-medium text-white hover:bg-[#0B564B] disabled:opacity-40 disabled:hover:bg-teal"
              >
                {editMutation.isPending ? 'Saving…' : 'Save'}
              </button>
            </div>
          ) : (
            <button
              type="button"
              disabled={!canSend}
              onClick={handleSend}
              className="rounded bg-teal px-3 py-1 text-sm font-medium text-white hover:bg-[#0B564B] disabled:opacity-40 disabled:hover:bg-teal"
            >
              Send <kbd className="ml-1 font-mono text-[10px] opacity-70">↵</kbd>
            </button>
          )}
        </div>
      </div>
      <div className="flex gap-3 px-1 pt-1.5 font-mono text-[9px] text-ink-3">
        <span>↵ send</span>
        <span>⇧↵ newline</span>
        <span>@ mention</span>
        <span>⌘K jump</span>
        <span>⌘/ search</span>
      </div>
    </div>
  )
})
