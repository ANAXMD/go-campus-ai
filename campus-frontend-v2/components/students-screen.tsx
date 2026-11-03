'use client'

import { ReactNode, useMemo, useState } from 'react'
import useSWR from 'swr'
import { BookOpen, GraduationCap, LogOut, Pencil, Plus, Search, Trash2, Users } from 'lucide-react'
import { Brand } from '@/components/brand'
import { DeleteStudentDialog, StudentFormDialog } from '@/components/student-dialogs'
import { Button } from '@/components/ui/button'
import { api, ApiError, type Session, type Student } from '@/lib/api'

const avatarColors = [
  'bg-cyan-500/15 text-cyan-300',
  'bg-violet-500/15 text-violet-300',
  'bg-amber-500/15 text-amber-300',
  'bg-emerald-500/15 text-emerald-300',
  'bg-pink-500/15 text-pink-300',
]

function initials(name: string) {
  return name
    .split(/\s+/)
    .filter(Boolean)
    .map((part) => part[0])
    .join('')
    .slice(0, 2)
    .toUpperCase()
}

/** Summary numbers derived from the real list, so they can never disagree with the table. */
function summarise(students: Student[]) {
  const counts = new Map<string, number>()
  for (const student of students) counts.set(student.major, (counts.get(student.major) ?? 0) + 1)
  const [topMajor, topCount] = [...counts.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))[0] ?? ['—', 0]
  return { total: students.length, majors: counts.size, topMajor, topCount }
}

type Dialogs = { kind: 'add' } | { kind: 'edit'; student: Student } | { kind: 'delete'; student: Student } | null

export function StudentsScreen({ session, onLogout }: { session: Session; onLogout: () => void }) {
  const [query, setQuery] = useState('')
  const [dialog, setDialog] = useState<Dialogs>(null)

  const {
    data: students = [],
    error,
    isLoading,
    mutate,
  } = useSWR(`students:${session.email}`, () => api.listStudents(session.token), {
    shouldRetryOnError: false,
    onError: (loadError: unknown) => {
      if (loadError instanceof ApiError && loadError.status === 401) onLogout()
    },
  })

  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase()
    if (!needle) return students
    return students.filter((s) => `${s.name} ${s.email} ${s.major} ${s.id}`.toLowerCase().includes(needle))
  }, [students, query])
  const summary = useMemo(() => summarise(students), [students])

  const dialogProps = {
    token: session.token,
    onClose: () => setDialog(null),
    onDone: () => mutate(),
    onUnauthorized: onLogout,
  }

  const status = isLoading ? 'Loading records...' : error ? 'Unable to load records' : `${filtered.length} of ${students.length} shown`

  return (
    <main className="min-h-screen bg-[#07111f] text-slate-100">
      <header className="flex h-[76px] items-center justify-between border-b border-white/[0.07] px-6 lg:px-10">
        <Brand />
        <div className="flex items-center gap-5">
          <div className="hidden items-center gap-3 border-r border-white/10 pr-5 sm:flex">
            <div className="flex size-8 items-center justify-center rounded-full bg-cyan-300/15 text-xs font-bold text-cyan-300">
              {session.email[0]?.toUpperCase()}
            </div>
            <div>
              <p className="text-xs font-semibold text-slate-200">{session.email}</p>
              <p className="text-[10px] text-slate-500">Signed in</p>
            </div>
          </div>
          <button onClick={onLogout} aria-label="Sign out" className="text-slate-500 transition hover:text-cyan-300">
            <LogOut className="size-[18px]" />
          </button>
        </div>
      </header>

      <div className="mx-auto flex max-w-[1440px]">
        <aside className="hidden w-64 shrink-0 border-r border-white/[0.07] px-5 py-8 lg:block">
          <p className="mb-4 px-3 text-[10px] font-bold uppercase tracking-[0.2em] text-slate-600">Workspace</p>
          <nav className="space-y-1">
            <span className="flex w-full items-center gap-3 rounded-xl bg-cyan-300/10 px-3 py-3 text-sm font-semibold text-cyan-300">
              <Users className="size-4" /> Students
              <span className="ml-auto rounded-md bg-white/5 px-2 py-0.5 text-[10px] text-cyan-200">{summary.total}</span>
            </span>
          </nav>
        </aside>

        <section className="min-w-0 flex-1 px-6 py-9 lg:px-10">
          <div className="mb-10 flex flex-col justify-between gap-5 sm:flex-row sm:items-end">
            <div>
              <p className="mb-2 text-xs font-semibold uppercase tracking-[0.2em] text-cyan-300">Student directory</p>
              <h1 className="text-3xl font-semibold tracking-[-0.035em] text-white">Students</h1>
              <p className="mt-2 text-sm text-slate-500">Manage and view everyone enrolled at your campus.</p>
            </div>
            <Button
              onClick={() => setDialog({ kind: 'add' })}
              className="h-10 rounded-lg bg-cyan-300 px-4 text-xs font-semibold text-slate-950 hover:bg-cyan-200"
            >
              <Plus data-icon="inline-start" /> Add Student
            </Button>
          </div>

          <div className="mb-5 grid grid-cols-1 gap-3 sm:grid-cols-3">
            <Stat label="Total students" value={String(summary.total)} icon={<Users className="size-4" />} />
            <Stat label="Majors" value={String(summary.majors)} icon={<BookOpen className="size-4" />} />
            <Stat
              label="Largest major"
              value={summary.topMajor}
              hint={summary.topCount ? `${summary.topCount} ${summary.topCount === 1 ? 'student' : 'students'}` : undefined}
              icon={<GraduationCap className="size-4" />}
            />
          </div>

          <div className="overflow-hidden rounded-2xl border border-white/[0.08] bg-white/[0.025]">
            <div className="flex flex-col justify-between gap-4 border-b border-white/[0.07] px-5 py-4 sm:flex-row sm:items-center">
              <div>
                <h2 className="text-sm font-semibold text-white">All students</h2>
                <p className="mt-1 text-xs text-slate-500">{status}</p>
              </div>
              <div className="relative">
                <Search className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-slate-600" />
                <input
                  aria-label="Search students"
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                  placeholder="Search students..."
                  className="h-9 w-full rounded-lg border border-white/10 bg-white/[0.04] pl-9 pr-3 text-xs text-white outline-none placeholder:text-slate-600 focus:border-cyan-300/50 sm:w-60"
                />
              </div>
            </div>

            {error ? (
              <p role="alert" className="px-5 py-10 text-center text-sm text-red-300">
                {error instanceof Error ? error.message : 'Unable to load students.'}
              </p>
            ) : isLoading ? (
              <p className="px-5 py-10 text-center text-sm text-slate-500">Loading students...</p>
            ) : filtered.length === 0 ? (
              <p className="px-5 py-10 text-center text-sm text-slate-500">
                {students.length === 0 ? 'No students yet. Add the first one with the button above.' : 'No students match your search.'}
              </p>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full min-w-[720px] text-left">
                  <thead>
                    <tr className="border-b border-white/[0.06] text-[10px] font-bold uppercase tracking-[0.15em] text-slate-600">
                      <th className="px-5 py-3.5">Student</th>
                      <th className="px-5 py-3.5">ID</th>
                      <th className="px-5 py-3.5">Email</th>
                      <th className="px-5 py-3.5">Major</th>
                      <th className="px-5 py-3.5 text-right">Actions</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-white/[0.05]">
                    {filtered.map((student) => (
                      <tr key={student.id} className="transition hover:bg-white/[0.025]">
                        <td className="px-5 py-4">
                          <div className="flex items-center gap-3">
                            <div
                              className={`flex size-8 items-center justify-center rounded-full text-[10px] font-bold ${avatarColors[student.id % avatarColors.length]}`}
                            >
                              {initials(student.name)}
                            </div>
                            <span className="text-sm font-medium text-slate-200">{student.name}</span>
                          </div>
                        </td>
                        <td className="px-5 py-4 font-mono text-xs text-slate-500">{student.id}</td>
                        <td className="px-5 py-4 text-xs text-slate-400">{student.email}</td>
                        <td className="px-5 py-4 text-xs text-slate-400">{student.major}</td>
                        <td className="px-5 py-4">
                          <div className="flex justify-end gap-1">
                            <button
                              onClick={() => setDialog({ kind: 'edit', student })}
                              aria-label={`Edit ${student.name}`}
                              className="rounded-md p-2 text-slate-500 transition hover:bg-white/5 hover:text-cyan-300"
                            >
                              <Pencil className="size-4" />
                            </button>
                            <button
                              onClick={() => setDialog({ kind: 'delete', student })}
                              aria-label={`Delete ${student.name}`}
                              className="rounded-md p-2 text-slate-500 transition hover:bg-white/5 hover:text-red-300"
                            >
                              <Trash2 className="size-4" />
                            </button>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        </section>
      </div>

      {dialog?.kind === 'add' && <StudentFormDialog key="add" student={null} {...dialogProps} />}
      {dialog?.kind === 'edit' && <StudentFormDialog key={`edit-${dialog.student.id}`} student={dialog.student} {...dialogProps} />}
      {dialog?.kind === 'delete' && <DeleteStudentDialog key={`delete-${dialog.student.id}`} student={dialog.student} {...dialogProps} />}
    </main>
  )
}

function Stat({ label, value, hint, icon }: { label: string; value: string; hint?: string; icon: ReactNode }) {
  return (
    <div className="rounded-xl border border-white/[0.07] bg-white/[0.025] p-4">
      <div className="mb-3 flex items-center justify-between text-slate-600">
        <span className="text-[11px] font-medium">{label}</span>
        <span className="text-cyan-300">{icon}</span>
      </div>
      <p className="truncate text-2xl font-semibold tracking-tight text-white">{value}</p>
      {hint && <p className="mt-1 text-[11px] text-slate-500">{hint}</p>}
    </div>
  )
}
