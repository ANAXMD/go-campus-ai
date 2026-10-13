'use client'

import { FormEvent, useState } from 'react'
import useSWR from 'swr'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import {
  ArrowRight,
  BookOpen,
  ChevronDown,
  GraduationCap,
  LayoutDashboard,
  LogOut,
  Plus,
  Search,
  ShieldCheck,
  SlidersHorizontal,
  Users,
} from 'lucide-react'

type Student = {
  id: string
  name: string
  email: string
  major: string
}

const avatarColors = ['bg-cyan-500/15 text-cyan-300', 'bg-violet-500/15 text-violet-300', 'bg-amber-500/15 text-amber-300', 'bg-emerald-500/15 text-emerald-300', 'bg-pink-500/15 text-pink-300']

function getStudentInitials(name: string) {
  return name.split(' ').map((part) => part[0]).join('').slice(0, 2).toUpperCase()
}

export default function Page() {
  const [isAuthenticated, setIsAuthenticated] = useState(false)

  if (!isAuthenticated) {
    return <LoginScreen onLogin={() => setIsAuthenticated(true)} />
  }

  return <StudentListScreen onLogout={() => setIsAuthenticated(false)} />
}

function Brand() {
  return (
    <div className="flex items-center gap-3">
      <div className="flex size-10 items-center justify-center rounded-xl bg-cyan-400 text-slate-950 shadow-[0_0_24px_rgba(34,211,238,0.18)]">
        <GraduationCap className="size-5" strokeWidth={2.5} />
      </div>
      <div>
        <p className="text-sm font-bold tracking-wide text-white">Campus<span className="text-cyan-300">Flow</span></p>
        <p className="text-[10px] font-medium uppercase tracking-[0.22em] text-slate-500">Management system</p>
      </div>
    </div>
  )
}

function LoginScreen({ onLogin }: { onLogin: () => void }) {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [isSubmitting, setIsSubmitting] = useState(false)

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError('')
    setIsSubmitting(true)

    try {
      const response = await fetch('http://127.0.0.1:8080/api/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, password }),
      })
      const data = await response.json().catch(() => null)

      if (!response.ok) {
        throw new Error(data?.message || data?.error || 'Unable to sign in. Please check your credentials.')
      }

      if (!data?.token) {
        throw new Error('Sign-in succeeded, but no token was returned.')
      }

      localStorage.setItem('campusflow_token', data.token)
      onLogin()
    } catch (submissionError) {
      setError(submissionError instanceof Error ? submissionError.message : 'Unable to sign in. Please try again.')
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <main className="relative flex min-h-screen overflow-hidden bg-[#07111f] text-slate-100">
      <div className="pointer-events-none absolute -left-32 top-1/4 size-96 rounded-full bg-cyan-500/10 blur-3xl" />
      <div className="pointer-events-none absolute -right-24 bottom-0 size-[28rem] rounded-full bg-blue-600/10 blur-3xl" />
      <section className="hidden w-[44%] flex-col justify-between border-r border-white/[0.07] p-10 lg:flex">
        <Brand />
        <div className="max-w-md">
          <div className="mb-8 flex size-14 items-center justify-center rounded-2xl border border-cyan-300/20 bg-cyan-300/10 text-cyan-300"><ShieldCheck className="size-7" /></div>
          <h1 className="text-5xl font-semibold leading-[1.08] tracking-[-0.04em] text-white">A clearer view of your campus.</h1>
          <p className="mt-6 max-w-sm text-base leading-7 text-slate-400">Manage students, programs, and campus operations from one focused workspace.</p>
        </div>
        <p className="text-xs text-slate-600">© 2024 CampusFlow · Secure administrator access</p>
      </section>
      <section className="flex flex-1 items-center justify-center px-6 py-12">
        <div className="w-full max-w-[410px]">
          <div className="mb-10 lg:hidden"><Brand /></div>
          <div className="mb-8">
            <p className="mb-3 text-xs font-semibold uppercase tracking-[0.2em] text-cyan-300">Welcome back</p>
            <h2 className="text-3xl font-semibold tracking-[-0.03em] text-white">Sign in to your workspace</h2>
            <p className="mt-3 text-sm leading-6 text-slate-400">Enter your details to access the campus dashboard.</p>
          </div>
          <form onSubmit={handleSubmit} className="space-y-5">
            <label className="block text-sm font-medium text-slate-300">Email address
              <input required type="email" value={email} onChange={(event) => setEmail(event.target.value)} placeholder="you@campus.edu" className="mt-2 h-12 w-full rounded-xl border border-white/10 bg-white/[0.045] px-4 text-sm text-white outline-none transition placeholder:text-slate-600 focus:border-cyan-300/60 focus:ring-4 focus:ring-cyan-300/10" />
            </label>
            <label className="block text-sm font-medium text-slate-300">Password
              <input required type="password" value={password} onChange={(event) => setPassword(event.target.value)} placeholder="Enter your password" className="mt-2 h-12 w-full rounded-xl border border-white/10 bg-white/[0.045] px-4 text-sm text-white outline-none transition placeholder:text-slate-600 focus:border-cyan-300/60 focus:ring-4 focus:ring-cyan-300/10" />
            </label>
            <div className="flex items-center justify-between pt-1 text-xs"><label className="flex items-center gap-2 text-slate-500"><input type="checkbox" className="size-3.5 accent-cyan-400" /> Remember me</label><button type="button" className="text-cyan-300 transition hover:text-cyan-200">Forgot password?</button></div>
            {error && <p role="alert" className="rounded-lg border border-red-400/20 bg-red-400/10 px-3 py-2.5 text-xs leading-5 text-red-300">{error}</p>}
            <button type="submit" disabled={isSubmitting} className="group flex h-12 w-full items-center justify-center gap-2 rounded-xl bg-cyan-300 text-sm font-bold text-slate-950 transition hover:bg-cyan-200 disabled:cursor-not-allowed disabled:opacity-60">{isSubmitting ? 'Signing in...' : 'Sign in'} {!isSubmitting && <ArrowRight className="size-4 transition group-hover:translate-x-0.5" />}</button>
          </form>
          <p className="mt-8 text-center text-xs text-slate-600">Need access? <span className="text-slate-400">Contact your campus administrator.</span></p>
        </div>
      </section>
    </main>
  )
}

function StudentListScreen({ onLogout }: { onLogout: () => void }) {
  const [query, setQuery] = useState('')
  const [isAddStudentOpen, setIsAddStudentOpen] = useState(false)
  const [newStudent, setNewStudent] = useState({ name: '', email: '', major: '' })
  const [isSavingStudent, setIsSavingStudent] = useState(false)
  const [saveStudentError, setSaveStudentError] = useState<string | null>(null)
  const token = typeof window === 'undefined' ? null : localStorage.getItem('campusflow_token')
  const { data: students = [], error, isLoading, mutate } = useSWR<Student[]>(
    token ? 'http://127.0.0.1:8080/api/students' : null,
    async (url: string) => {
      const response = await fetch(url, { headers: { Authorization: `Bearer ${token}` } })
      const data = await response.json().catch(() => null)
      if (!response.ok) throw new Error(data?.message || data?.error || 'Unable to load students.')
      if (!Array.isArray(data)) throw new Error('The students response was not an array.')
      return data
    },
  )
  async function handleSaveStudent(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setIsSavingStudent(true)
    setSaveStudentError(null)

    try {
      const response = await fetch('http://127.0.0.1:8080/api/students', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
        },
        body: JSON.stringify(newStudent),
      })
      const data = await response.json().catch(() => null)
      if (!response.ok) throw new Error(data?.message || data?.error || 'Unable to save student.')
      await mutate()
      setIsAddStudentOpen(false)
      setNewStudent({ name: '', email: '', major: '' })
    } catch (saveError) {
      setSaveStudentError(saveError instanceof Error ? saveError.message : 'Unable to save student.')
    } finally {
      setIsSavingStudent(false)
    }
  }

  const filteredStudents = students.filter((student) => `${student.name} ${student.email} ${student.major} ${student.id}`.toLowerCase().includes(query.toLowerCase()))

  return (
    <main className="min-h-screen bg-[#07111f] text-slate-100">
      <header className="flex h-[76px] items-center justify-between border-b border-white/[0.07] px-6 lg:px-10"><Brand /><div className="flex items-center gap-5"><div className="hidden items-center gap-3 border-r border-white/10 pr-5 sm:flex"><div className="flex size-8 items-center justify-center rounded-full bg-cyan-300/15 text-xs font-bold text-cyan-300">JD</div><div><p className="text-xs font-semibold text-slate-200">Jordan Davis</p><p className="text-[10px] text-slate-500">Administrator</p></div><ChevronDown className="size-3.5 text-slate-500" /></div><button onClick={onLogout} aria-label="Sign out" className="text-slate-500 transition hover:text-cyan-300"><LogOut className="size-[18px]" /></button></div></header>
      <div className="mx-auto flex max-w-[1440px]">
        <aside className="hidden w-64 shrink-0 border-r border-white/[0.07] px-5 py-8 lg:block"><p className="mb-4 px-3 text-[10px] font-bold uppercase tracking-[0.2em] text-slate-600">Workspace</p><nav className="space-y-1"><button className="flex w-full items-center gap-3 rounded-xl bg-cyan-300/10 px-3 py-3 text-sm font-semibold text-cyan-300"><LayoutDashboard className="size-4" /> Overview</button><button className="flex w-full items-center gap-3 rounded-xl px-3 py-3 text-sm text-slate-500 transition hover:bg-white/[0.04] hover:text-slate-200"><Users className="size-4" /> Students <span className="ml-auto rounded-md bg-white/5 px-2 py-0.5 text-[10px] text-slate-500">128</span></button><button className="flex w-full items-center gap-3 rounded-xl px-3 py-3 text-sm text-slate-500 transition hover:bg-white/[0.04] hover:text-slate-200"><BookOpen className="size-4" /> Programs</button></nav></aside>
        <section className="min-w-0 flex-1 px-6 py-9 lg:px-10"><div className="mb-10 flex flex-col justify-between gap-5 sm:flex-row sm:items-end"><div><p className="mb-2 text-xs font-semibold uppercase tracking-[0.2em] text-cyan-300">Student directory</p><h1 className="text-3xl font-semibold tracking-[-0.035em] text-white">Students</h1><p className="mt-2 text-sm text-slate-500">Manage and view everyone enrolled at your campus.</p></div><div className="flex items-center gap-3"><Dialog open={isAddStudentOpen} onOpenChange={setIsAddStudentOpen}><DialogTrigger render={<Button className="h-10 rounded-lg bg-cyan-300 px-4 text-xs font-semibold text-slate-950 hover:bg-cyan-200" />}><Plus data-icon="inline-start" /> Add Student</DialogTrigger><DialogContent className="border-white/10 bg-[#0d1a2b] text-slate-100 sm:max-w-[440px]"><DialogHeader><DialogTitle className="text-white">Add student</DialogTitle><DialogDescription className="text-slate-400">Create a new student record for your campus directory.</DialogDescription></DialogHeader><form onSubmit={handleSaveStudent} className="flex flex-col gap-4">{saveStudentError && <p role="alert" className="text-sm text-red-300">{saveStudentError}</p>}<label className="flex flex-col gap-2 text-sm font-medium text-slate-300">Name<input required value={newStudent.name} onChange={(event) => setNewStudent({ ...newStudent, name: event.target.value })} placeholder="Alex Morgan" className="h-11 rounded-lg border border-white/10 bg-white/[0.04] px-3 text-sm text-white outline-none placeholder:text-slate-600 focus:border-cyan-300/60 focus:ring-4 focus:ring-cyan-300/10" /></label><label className="flex flex-col gap-2 text-sm font-medium text-slate-300">Email<input required type="email" value={newStudent.email} onChange={(event) => setNewStudent({ ...newStudent, email: event.target.value })} placeholder="alex@campus.edu" className="h-11 rounded-lg border border-white/10 bg-white/[0.04] px-3 text-sm text-white outline-none placeholder:text-slate-600 focus:border-cyan-300/60 focus:ring-4 focus:ring-cyan-300/10" /></label><label className="flex flex-col gap-2 text-sm font-medium text-slate-300">Major<input required value={newStudent.major} onChange={(event) => setNewStudent({ ...newStudent, major: event.target.value })} placeholder="Computer Science" className="h-11 rounded-lg border border-white/10 bg-white/[0.04] px-3 text-sm text-white outline-none placeholder:text-slate-600 focus:border-cyan-300/60 focus:ring-4 focus:ring-cyan-300/10" /></label><DialogFooter><Button type="button" variant="ghost" onClick={() => setIsAddStudentOpen(false)} className="text-slate-400 hover:bg-white/5 hover:text-white">Cancel</Button><Button type="submit" className="bg-cyan-300 text-slate-950 hover:bg-cyan-200">{isSavingStudent ? 'Saving...' : 'Save'}</Button></DialogFooter></form></DialogContent></Dialog><button className="flex h-10 items-center justify-center gap-2 rounded-lg border border-white/10 px-4 text-xs font-semibold text-slate-300 transition hover:bg-white/[0.05]"><SlidersHorizontal className="size-4" /> Filters</button></div></div>
          <div className="mb-5 grid grid-cols-1 gap-3 sm:grid-cols-3"><Stat label="Total students" value="128" icon={<Users className="size-4" />} /><Stat label="Active programs" value="24" icon={<BookOpen className="size-4" />} /><Stat label="New this month" value="12" icon={<ArrowRight className="size-4" />} /></div>
          <div className="overflow-hidden rounded-2xl border border-white/[0.08] bg-white/[0.025]"><div className="flex flex-col justify-between gap-4 border-b border-white/[0.07] px-5 py-4 sm:flex-row sm:items-center"><div><h2 className="text-sm font-semibold text-white">All students</h2><p className="mt-1 text-xs text-slate-500">{isLoading ? 'Loading records...' : error ? 'Unable to load records' : `${filteredStudents.length} records shown`}</p></div><div className="relative"><Search className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-slate-600" /><input aria-label="Search students" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search students..." className="h-9 w-full rounded-lg border border-white/10 bg-white/[0.04] pl-9 pr-3 text-xs text-white outline-none placeholder:text-slate-600 focus:border-cyan-300/50 sm:w-60" /></div></div>{error ? <p role="alert" className="px-5 py-10 text-center text-sm text-red-300">{error.message}</p> : isLoading ? <p className="px-5 py-10 text-center text-sm text-slate-500">Loading students...</p> : <div className="overflow-x-auto"><table className="w-full min-w-[680px] text-left"><thead><tr className="border-b border-white/[0.06] text-[10px] font-bold uppercase tracking-[0.15em] text-slate-600"><th className="px-5 py-3.5">Student</th><th className="px-5 py-3.5">ID</th><th className="px-5 py-3.5">Email</th><th className="px-5 py-3.5">Major</th></tr></thead><tbody className="divide-y divide-white/[0.05]">{filteredStudents.map((student, index) => <tr key={student.id} className="transition hover:bg-white/[0.025]"><td className="px-5 py-4"><div className="flex items-center gap-3"><div className={`flex size-8 items-center justify-center rounded-full text-[10px] font-bold ${avatarColors[index % avatarColors.length]}`}>{getStudentInitials(student.name)}</div><span className="text-sm font-medium text-slate-200">{student.name}</span></div></td><td className="px-5 py-4 font-mono text-xs text-slate-500">{student.id}</td><td className="px-5 py-4 text-xs text-slate-400">{student.email}</td><td className="px-5 py-4 text-xs text-slate-400">{student.major}</td></tr>)}</tbody></table></div>}</div><p className="mt-5 text-center text-[11px] text-slate-600">{isLoading ? 'Fetching latest student data' : error ? 'Student data unavailable' : `${filteredStudents.length} students shown · Last updated just now`}</p>
        </section>
      </div>
    </main>
  )
}

function Stat({ label, value, icon }: { label: string; value: string; icon: React.ReactNode }) { return <div className="rounded-xl border border-white/[0.07] bg-white/[0.025] p-4"><div className="mb-3 flex items-center justify-between text-slate-600"> <span className="text-[11px] font-medium">{label}</span><span className="text-cyan-300">{icon}</span></div><p className="text-2xl font-semibold tracking-tight text-white">{value}</p></div> }
