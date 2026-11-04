'use client'

import { ReactNode, useEffect, useState } from 'react'
import useSWR, { useSWRConfig } from 'swr'
import { BookOpen, ChevronLeft, ChevronRight, GraduationCap, LogOut, Pencil, Plus, Search, Trash2, Users } from 'lucide-react'
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

const PAGE_SIZES = [10, 25, 50]
const SEARCH_DELAY_MS = 300

function initials(name: string) {
  return name
    .split(/\s+/)
    .filter(Boolean)
    .map((part) => part[0])
    .join('')
    .slice(0, 2)
    .toUpperCase()
}

/** Returns `value`, but only after it has stopped changing for `delay` ms. */
function useDebounced<T>(value: T, delay: number): T {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delay)
    return () => clearTimeout(timer)
  }, [value, delay])
  return debounced
}

type Dialogs = { kind: 'add' } | { kind: 'edit'; student: Student } | { kind: 'delete'; student: Student } | null

export function StudentsScreen({ session, onLogout }: { session: Session; onLogout: () => void }) {
  const isAdmin = session.role === 'admin'
  const { mutate: revalidateAll } = useSWRConfig()

  const [search, setSearch] = useState('')
  const [page, setPage] = useState(0) // zero-based
  const [pageSize, setPageSize] = useState(PAGE_SIZES[0])
  const [dialog, setDialog] = useState<Dialogs>(null)

  // The server does the searching, so wait until typing pauses before asking it.
  const query = useDebounced(search.trim(), SEARCH_DELAY_MS)
  const isSettling = search.trim() !== query

  const onApiError = (error: unknown) => {
    if (error instanceof ApiError && error.status === 401) onLogout()
  }

  // If the last row of the last page is deleted, step back instead of showing an empty page.
  const [lastKnownTotal, setLastKnownTotal] = useState<number | null>(null)
  const lastPage = lastKnownTotal === null ? page : Math.max(0, Math.ceil(lastKnownTotal / pageSize) - 1)
  const currentPage = Math.min(page, lastPage)

  const {
    data: result,
    error,
    isLoading,
    isValidating,
  } = useSWR(
    ['students', session.email, query, currentPage, pageSize],
    () => api.listStudents(session.token, { q: query, limit: pageSize, offset: currentPage * pageSize }),
    {
      keepPreviousData: true, // keep showing the old page while the next one loads
      shouldRetryOnError: false,
      onError: onApiError,
      onSuccess: (data) => setLastKnownTotal(data.total),
    },
  )

  const { data: stats } = useSWR(['student-stats', session.email], () => api.studentStats(session.token), {
    shouldRetryOnError: false,
    onError: onApiError,
  })

  const students = result?.items ?? []
  const total = result?.total ?? 0
  const pageCount = Math.max(1, Math.ceil(total / pageSize))
  const firstShown = total === 0 ? 0 : currentPage * pageSize + 1
  const lastShown = currentPage * pageSize + students.length

  const dialogProps = {
    token: session.token,
    onClose: () => setDialog(null),
    // A change can affect any page, any search and the stats: refresh them all.
    onDone: () => revalidateAll((key: unknown) => Array.isArray(key) && (key[0] === 'students' || key[0] === 'student-stats')),
    onUnauthorized: onLogout,
  }

  const status = error
    ? 'Unable to load records'
    : isLoading
      ? 'Loading records...'
      : total === 0
        ? 'No records'
        : `Showing ${firstShown}-${lastShown} of ${total}${query ? ` matching "${query}"` : ''}`

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
              <p className="text-[10px] text-slate-500">{isAdmin ? 'Administrator' : 'Viewer · read-only'}</p>
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
              <span className="ml-auto rounded-md bg-white/5 px-2 py-0.5 text-[10px] text-cyan-200">{stats?.total ?? '–'}</span>
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
            {isAdmin && (
              <Button
                onClick={() => setDialog({ kind: 'add' })}
                className="h-10 rounded-lg bg-cyan-300 px-4 text-xs font-semibold text-slate-950 hover:bg-cyan-200"
              >
                <Plus data-icon="inline-start" /> Add Student
              </Button>
            )}
          </div>

          <div className="mb-5 grid grid-cols-1 gap-3 sm:grid-cols-3">
            <Stat label="Total students" value={stats ? String(stats.total) : '–'} icon={<Users className="size-4" />} />
            <Stat label="Majors" value={stats ? String(stats.majors) : '–'} icon={<BookOpen className="size-4" />} />
            <Stat
              label="Largest major"
              value={stats ? stats.topMajor || '—' : '–'}
              hint={stats?.topCount ? `${stats.topCount} ${stats.topCount === 1 ? 'student' : 'students'}` : undefined}
              icon={<GraduationCap className="size-4" />}
            />
          </div>

          <div className="overflow-hidden rounded-2xl border border-white/[0.08] bg-white/[0.025]">
            <div className="flex flex-col justify-between gap-4 border-b border-white/[0.07] px-5 py-4 sm:flex-row sm:items-center">
              <div>
                <h2 className="text-sm font-semibold text-white">All students</h2>
                <p className="mt-1 text-xs text-slate-500" aria-live="polite">
                  {status}
                  {(isSettling || (isValidating && !isLoading)) && <span className="ml-2 text-slate-600">updating…</span>}
                </p>
              </div>
              <div className="relative">
                <Search className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-slate-600" />
                <input
                  aria-label="Search students"
                  value={search}
                  onChange={(event) => {
                    setSearch(event.target.value)
                    setPage(0) // a new search starts from the first page
                  }}
                  placeholder="Search name, email or major..."
                  className="h-9 w-full rounded-lg border border-white/10 bg-white/[0.04] pl-9 pr-3 text-xs text-white outline-none placeholder:text-slate-600 focus:border-cyan-300/50 sm:w-72"
                />
              </div>
            </div>

            {error && !result ? (
              <p role="alert" className="px-5 py-10 text-center text-sm text-red-300">
                {error instanceof Error ? error.message : 'Unable to load students.'}
              </p>
            ) : isLoading || (students.length === 0 && total > 0) ? (
              // The second condition is the moment after deleting the last row of the
              // last page: the old (now empty) page is still shown while the previous one loads.
              <p className="px-5 py-10 text-center text-sm text-slate-500">Loading students...</p>
            ) : students.length === 0 ? (
              <p className="px-5 py-10 text-center text-sm text-slate-500">
                {query
                  ? `No students match "${query}".`
                  : isAdmin
                    ? 'No students yet. Add the first one with the button above.'
                    : 'No students yet.'}
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
                      {isAdmin && <th className="px-5 py-3.5 text-right">Actions</th>}
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-white/[0.05]">
                    {students.map((student) => (
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
                        {isAdmin && (
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
                        )}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}

            <div className="flex flex-col items-center justify-between gap-3 border-t border-white/[0.07] px-5 py-3 sm:flex-row">
              <label className="flex items-center gap-2 text-xs text-slate-500">
                Rows per page
                <select
                  aria-label="Rows per page"
                  value={pageSize}
                  onChange={(event) => {
                    setPageSize(Number(event.target.value))
                    setPage(0)
                  }}
                  className="h-8 rounded-md border border-white/10 bg-[#0d1a2b] px-2 text-xs text-slate-200 outline-none focus:border-cyan-300/50"
                >
                  {PAGE_SIZES.map((size) => (
                    <option key={size} value={size}>
                      {size}
                    </option>
                  ))}
                </select>
              </label>
              <div className="flex items-center gap-2 text-xs text-slate-500">
                <span>
                  Page {currentPage + 1} of {pageCount}
                </span>
                <button
                  aria-label="Previous page"
                  disabled={currentPage === 0}
                  onClick={() => setPage(currentPage - 1)}
                  className="rounded-md border border-white/10 p-1.5 text-slate-300 transition hover:bg-white/5 disabled:cursor-not-allowed disabled:opacity-30"
                >
                  <ChevronLeft className="size-4" />
                </button>
                <button
                  aria-label="Next page"
                  disabled={currentPage + 1 >= pageCount}
                  onClick={() => setPage(currentPage + 1)}
                  className="rounded-md border border-white/10 p-1.5 text-slate-300 transition hover:bg-white/5 disabled:cursor-not-allowed disabled:opacity-30"
                >
                  <ChevronRight className="size-4" />
                </button>
              </div>
            </div>
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
