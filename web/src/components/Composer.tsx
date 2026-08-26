import { useEffect, useRef, useState } from 'react'
import { api, type UploadedFile } from '../lib/api'
import { newClientMsgId, useSendMessage } from '../hooks/useMessages'
import { useUiStore } from '../store/ui'
import { wsClient } from '../lib/ws'
import { throttle } from '../lib/throttle'
import { MentionPicker, useMentionCandidates } from './MentionPicker'

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

export function Composer({
  channelId,
  threadId,
  placeholder,
  onSent,
}: {
  channelId: string
  threadId?: string
  placeholder?: string
  onSent?: () => void
}) {
  const setDraft = useUiStore((s) => s.setDraft)
  const drafts = useUiStore((s) => s.drafts)
  const sendMutation = useSendMessage(channelId)

  const [body, setBody] = useState('')
  const [uploads, setUploads] = useState<UploadChip[]>([])
  const [alsoSendToChannel, setAlsoSendToChannel] = useState(false)
  const [mentionQuery, setMentionQuery] = useState<string | null>(null)
  const [mentionStart, setMentionStart] = useState(0)

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
    if (value) window.localStorage.setItem(draftStorageKey, value)
    else window.localStorage.removeItem(draftStorageKey)
    if (!threadId) setDraft(channelId, value)
  }

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

  const canSend = (body.trim().length > 0 || uploads.some((u) => u.file)) && !uploads.some((u) => !u.file && !u.error)

  const handleSend = () => {
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

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (mentionQuery !== null && ['ArrowDown', 'ArrowUp', 'Tab', 'Enter', 'Escape'].includes(e.key)) {
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
      <div
        className="rounded-md border border-rule bg-paper-2 focus-within:border-teal"
        onDragOver={(e) => e.preventDefault()}
        onDrop={(e) => {
          e.preventDefault()
          if (e.dataTransfer.files.length) void handleFiles(e.dataTransfer.files)
        }}
      >
        {uploads.length > 0 && (
          <div className="flex flex-wrap gap-2 p-2">
            {uploads.map((u) => (
              <div key={u.id} className="flex items-center gap-2 rounded bg-paper px-2 py-1 text-xs">
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
          className="w-full resize-none bg-transparent px-3 py-2 text-sm text-ink outline-none"
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
          <button
            type="button"
            disabled={!canSend}
            onClick={handleSend}
            className="rounded bg-teal px-3 py-1 text-sm font-medium text-white disabled:opacity-40"
          >
            Send <kbd className="ml-1 font-mono text-[10px] opacity-70">↵</kbd>
          </button>
        </div>
      </div>
    </div>
  )
}
