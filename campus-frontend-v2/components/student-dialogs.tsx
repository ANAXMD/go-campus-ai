'use client'

import { FormEvent, useState } from 'react'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { api, ApiError, type Student } from '@/lib/api'

const fieldClass =
  'h-11 rounded-lg border border-white/10 bg-white/[0.04] px-3 text-sm text-white outline-none placeholder:text-slate-600 focus:border-cyan-300/60 focus:ring-4 focus:ring-cyan-300/10'

type CommonProps = {
  token: string
  onClose: () => void
  /** Called after the change succeeded, so the caller can refresh its list. */
  onDone: () => void | Promise<unknown>
  /** Called when the API says the session is no longer valid. */
  onUnauthorized: () => void
}

function messageFor(error: unknown, fallback: string) {
  return error instanceof ApiError ? error.message : fallback
}

/** Add a student (student === null) or edit an existing one. Mount it only while open. */
export function StudentFormDialog({
  student,
  token,
  onClose,
  onDone,
  onUnauthorized,
}: CommonProps & { student: Student | null }) {
  const [form, setForm] = useState({
    name: student?.name ?? '',
    email: student?.email ?? '',
    major: student?.major ?? '',
  })
  const [isSaving, setIsSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const isEdit = student !== null

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setIsSaving(true)
    setError(null)
    try {
      if (student) await api.updateStudent(token, student.id, form)
      else await api.createStudent(token, form)
      await onDone()
      onClose()
    } catch (saveError) {
      if (saveError instanceof ApiError && saveError.status === 401) return onUnauthorized()
      setError(messageFor(saveError, 'Unable to save the student.'))
    } finally {
      setIsSaving(false)
    }
  }

  const field = (key: keyof typeof form, label: string, placeholder: string, type = 'text') => (
    <label className="flex flex-col gap-2 text-sm font-medium text-slate-300">
      {label}
      <input
        required
        type={type}
        value={form[key]}
        onChange={(event) => setForm({ ...form, [key]: event.target.value })}
        placeholder={placeholder}
        className={fieldClass}
      />
    </label>
  )

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="border-white/10 bg-[#0d1a2b] text-slate-100 sm:max-w-[440px]">
        <DialogHeader>
          <DialogTitle className="text-white">{isEdit ? 'Edit student' : 'Add student'}</DialogTitle>
          <DialogDescription className="text-slate-400">
            {isEdit ? 'Update this student record.' : 'Create a new student record for your campus directory.'}
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="flex flex-col gap-4">
          {error && (
            <p role="alert" className="text-sm text-red-300">
              {error}
            </p>
          )}
          {field('name', 'Name', 'Alex Morgan')}
          {field('email', 'Email', 'alex@example.com', 'email')}
          {field('major', 'Major', 'Computer Science')}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose} className="text-slate-400 hover:bg-white/5 hover:text-white">
              Cancel
            </Button>
            <Button type="submit" disabled={isSaving} className="bg-cyan-300 text-slate-950 hover:bg-cyan-200">
              {isSaving ? 'Saving...' : 'Save'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

/** Asks for confirmation, then deletes. Mount it only while open. */
export function DeleteStudentDialog({
  student,
  token,
  onClose,
  onDone,
  onUnauthorized,
}: CommonProps & { student: Student }) {
  const [isDeleting, setIsDeleting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function handleDelete() {
    setIsDeleting(true)
    setError(null)
    try {
      await api.deleteStudent(token, student.id)
      await onDone()
      onClose()
    } catch (deleteError) {
      if (deleteError instanceof ApiError && deleteError.status === 401) return onUnauthorized()
      setError(messageFor(deleteError, 'Unable to delete the student.'))
    } finally {
      setIsDeleting(false)
    }
  }

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="border-white/10 bg-[#0d1a2b] text-slate-100 sm:max-w-[440px]">
        <DialogHeader>
          <DialogTitle className="text-white">Delete {student.name}?</DialogTitle>
          <DialogDescription className="text-slate-400">
            This permanently removes the record for {student.email}. It cannot be undone.
          </DialogDescription>
        </DialogHeader>
        {error && (
          <p role="alert" className="text-sm text-red-300">
            {error}
          </p>
        )}
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onClose} className="text-slate-400 hover:bg-white/5 hover:text-white">
            Cancel
          </Button>
          <Button type="button" disabled={isDeleting} onClick={handleDelete} className="bg-red-500 text-white hover:bg-red-400">
            {isDeleting ? 'Deleting...' : 'Delete'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
