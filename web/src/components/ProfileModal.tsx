import { useEffect, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { api, ApiError, type User } from '../lib/api'
import { Modal } from './Modal'
import { Avatar } from './Avatar'

function formatJoined(ts?: number): string {
  if (!ts) return '—'
  return new Date(ts).toLocaleDateString(undefined, { year: 'numeric', month: 'long', day: 'numeric' })
}

export function ProfileModal({
  open,
  onClose,
  user,
  mode = 'view',
}: {
  open: boolean
  onClose: () => void
  user: User
  mode?: 'view' | 'edit'
}) {
  const [edit, setEdit] = useState(mode === 'edit')
  const [name, setName] = useState(user.display_name)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [dragging, setDragging] = useState(false)
  const [photoSaved, setPhotoSaved] = useState(false)
  const qc = useQueryClient()

  const ACCEPTED_AVATAR_TYPES = ['image/png', 'image/jpeg', 'image/gif', 'image/webp']

  useEffect(() => {
    if (!open) return
    setEdit(mode === 'edit')
    setName(user.display_name)
    setError('')
    setPhotoSaved(false)
  }, [open, mode, user.display_name])

  const nameValid = name.trim().length > 0
  const nameChanged = name.trim() !== user.display_name

  async function save(patch: { display_name?: string; avatar_file_id?: string | null }) {
    setBusy(true)
    setError('')
    try {
      await api.updateMe(patch)
      await qc.invalidateQueries({ queryKey: ['auth', 'me'] })
      if (patch.display_name !== undefined) setEdit(false)
      if (patch.avatar_file_id !== undefined) setPhotoSaved(true)
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Could not update profile.')
    } finally {
      setBusy(false)
    }
  }

  async function uploadFile(file: File) {
    if (!ACCEPTED_AVATAR_TYPES.includes(file.type)) {
      setError('Avatar must be PNG, JPEG, GIF, or WebP.')
      return
    }
    setBusy(true)
    setError('')
    try {
      const uploaded = await api.uploadAvatar(file)
      await api.updateMe({ avatar_file_id: uploaded.id })
      await qc.invalidateQueries({ queryKey: ['auth', 'me'] })
      setPhotoSaved(true)
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Could not upload photo.')
    } finally {
      setBusy(false)
    }
  }

  async function upload(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    e.target.value = ''
    if (!file) return
    await uploadFile(file)
  }

  function onDrop(e: React.DragEvent<HTMLDivElement>) {
    e.preventDefault()
    setDragging(false)
    if (busy) return
    const file = e.dataTransfer.files?.[0]
    if (file) void uploadFile(file)
  }

  function onDragOver(e: React.DragEvent<HTMLDivElement>) {
    e.preventDefault()
    if (!busy) setDragging(true)
  }

  function onDragLeave(e: React.DragEvent<HTMLDivElement>) {
    e.preventDefault()
    setDragging(false)
  }

  function cancelEdit() {
    setName(user.display_name)
    setError('')
    setEdit(false)
  }

  return (
    <Modal open={open} onClose={onClose} labelledBy="profile-title" className="w-[min(420px,92vw)]">
      <div className="p-6">
        <div className="flex items-center justify-between">
          <h2 id="profile-title" className="font-display text-lg font-semibold text-ink">
            {edit ? 'Edit profile' : 'Profile'}
          </h2>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close"
            className="grid h-7 w-7 place-items-center rounded text-ink-3 hover:bg-paper-3 hover:text-ink"
          >
            ✕
          </button>
        </div>

        <div
          className={
            'mt-5 flex flex-col items-center gap-3 border-b border-rule pb-5' +
            (edit ? ' rounded-md' : '') +
            (dragging ? ' bg-teal-soft outline-dashed outline-2 outline-teal outline-offset-4' : '')
          }
          onDragOver={edit ? onDragOver : undefined}
          onDragLeave={edit ? onDragLeave : undefined}
          onDrop={edit ? onDrop : undefined}
        >
          <Avatar name={user.display_name || user.username} color={user.avatar_color} avatarUrl={user.avatar_url} size={72} />
          {edit && (
            <>
              <div className="flex gap-2">
                <label className="cursor-pointer rounded border border-rule px-2.5 py-1 text-xs font-medium text-ink-2 hover:bg-paper-3">
                  Upload photo
                  <input type="file" accept="image/png,image/jpeg,image/gif,image/webp" onChange={upload} disabled={busy} className="hidden" />
                </label>
                <button
                  type="button"
                  onClick={() => save({ avatar_file_id: null })}
                  disabled={busy || !user.avatar_url}
                  className="rounded border border-rule px-2.5 py-1 text-xs font-medium text-red-600 hover:bg-red-50 disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-transparent"
                >
                  Remove photo
                </button>
              </div>
              <p className="text-[11px] text-ink-3">
                {dragging ? 'Drop to upload' : photoSaved ? 'Photo saved.' : 'or drag and drop an image here'}
              </p>
            </>
          )}
        </div>

        {edit ? (
          <label className="mt-5 block text-sm">
            <span className="font-medium text-ink-2">Display name</span>
            <input
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="mt-1 w-full rounded border border-rule bg-paper p-2"
            />
            {!nameValid && <span className="mt-1 block text-xs text-red-600">Display name can't be empty.</span>}
          </label>
        ) : (
          <dl className="mt-5 space-y-3 text-sm">
            <div>
              <dt className="lbl text-ink-3">Display name</dt>
              <dd className="mt-0.5 text-ink">{user.display_name || user.username}</dd>
            </div>
            <div>
              <dt className="lbl text-ink-3">Username</dt>
              <dd className="mt-0.5 text-ink">@{user.username}</dd>
            </div>
            <div>
              <dt className="lbl text-ink-3">Email</dt>
              <dd className="mt-0.5 text-ink">{user.email}</dd>
            </div>
            <div>
              <dt className="lbl text-ink-3">Role</dt>
              <dd className="mt-0.5 capitalize text-ink">{user.role}</dd>
            </div>
            <div>
              <dt className="lbl text-ink-3">Joined</dt>
              <dd className="mt-0.5 text-ink">{formatJoined(user.created_at)}</dd>
            </div>
          </dl>
        )}

        {error && <p className="mt-4 text-sm text-red-600">{error}</p>}

        <div className="mt-6 flex gap-2">
          {edit ? (
            <>
              <button
                type="button"
                onClick={() => (nameChanged ? save({ display_name: name.trim() }) : setEdit(false))}
                disabled={busy || !nameValid || (!nameChanged && !photoSaved)}
                className="rounded bg-teal px-3 py-2 text-sm text-white disabled:opacity-50"
              >
                {busy ? 'Saving…' : 'Save'}
              </button>
              <button
                type="button"
                onClick={cancelEdit}
                disabled={busy}
                className="rounded border border-rule px-3 py-2 text-sm text-ink-2 hover:bg-paper-3"
              >
                Cancel
              </button>
            </>
          ) : (
            <button
              type="button"
              onClick={() => setEdit(true)}
              className="rounded bg-teal px-3 py-2 text-sm text-white"
            >
              Edit
            </button>
          )}
        </div>
      </div>
    </Modal>
  )
}
