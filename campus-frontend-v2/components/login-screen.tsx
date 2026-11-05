'use client'

import { FormEvent, useState } from 'react'
import { ArrowRight, ShieldCheck } from 'lucide-react'
import { Brand } from '@/components/brand'
import { api, ApiError, type Session } from '@/lib/api'

type Mode = 'sign-in' | 'register'

const inputClass =
  'mt-2 h-12 w-full rounded-xl border border-white/10 bg-white/[0.045] px-4 text-sm text-white outline-none transition placeholder:text-slate-600 focus:border-cyan-300/60 focus:ring-4 focus:ring-cyan-300/10'

export function LoginScreen({ onLogin }: { onLogin: (session: Session) => void }) {
  const [mode, setMode] = useState<Mode>('sign-in')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [isSubmitting, setIsSubmitting] = useState(false)

  const isRegister = mode === 'register'

  function switchMode() {
    setMode(isRegister ? 'sign-in' : 'register')
    setError('')
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError('')
    setIsSubmitting(true)
    try {
      if (isRegister) await api.register(email, password)
      onLogin(await api.login(email, password))
    } catch (submissionError) {
      setError(submissionError instanceof ApiError ? submissionError.message : 'Something went wrong. Please try again.')
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
          <div className="mb-8 flex size-14 items-center justify-center rounded-2xl border border-cyan-300/20 bg-cyan-300/10 text-cyan-300">
            <ShieldCheck className="size-7" />
          </div>
          <h1 className="text-5xl font-semibold leading-[1.08] tracking-[-0.04em] text-white">A clearer view of your campus.</h1>
          <p className="mt-6 max-w-sm text-base leading-7 text-slate-400">
            Keep the student directory in one focused workspace.
          </p>
        </div>
        <p className="text-xs text-slate-600">Student Directory · Secure administrator access</p>
      </section>

      <section className="flex flex-1 items-center justify-center px-6 py-12">
        <div className="w-full max-w-[410px]">
          <div className="mb-10 lg:hidden">
            <Brand />
          </div>
          <div className="mb-8">
            <p className="mb-3 text-xs font-semibold uppercase tracking-[0.2em] text-cyan-300">
              {isRegister ? 'Get started' : 'Welcome back'}
            </p>
            <h2 className="text-3xl font-semibold tracking-[-0.03em] text-white">
              {isRegister ? 'Create your account' : 'Sign in to your workspace'}
            </h2>
            <p className="mt-3 text-sm leading-6 text-slate-400">
              {isRegister ? 'Choose an email and a password of at least 8 characters.' : 'Enter your details to access the student directory.'}
            </p>
          </div>

          <form onSubmit={handleSubmit} className="space-y-5">
            <label className="block text-sm font-medium text-slate-300">
              Email address
              <input
                required
                type="email"
                autoComplete="email"
                value={email}
                onChange={(event) => setEmail(event.target.value)}
                placeholder="you@example.com"
                className={inputClass}
              />
            </label>
            <label className="block text-sm font-medium text-slate-300">
              Password
              <input
                required
                type="password"
                minLength={isRegister ? 8 : undefined}
                autoComplete={isRegister ? 'new-password' : 'current-password'}
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                placeholder={isRegister ? 'At least 8 characters' : 'Enter your password'}
                className={inputClass}
              />
            </label>
            {error && (
              <p role="alert" className="rounded-lg border border-red-400/20 bg-red-400/10 px-3 py-2.5 text-xs leading-5 text-red-300">
                {error}
              </p>
            )}
            <button
              type="submit"
              disabled={isSubmitting}
              className="group flex h-12 w-full items-center justify-center gap-2 rounded-xl bg-cyan-300 text-sm font-bold text-slate-950 transition hover:bg-cyan-200 disabled:cursor-not-allowed disabled:opacity-60"
            >
              {isSubmitting ? (isRegister ? 'Creating account...' : 'Signing in...') : isRegister ? 'Create account' : 'Sign in'}
              {!isSubmitting && <ArrowRight className="size-4 transition group-hover:translate-x-0.5" />}
            </button>
          </form>

          <p className="mt-8 text-center text-xs text-slate-600">
            {isRegister ? 'Already have an account?' : 'New here?'}{' '}
            <button type="button" onClick={switchMode} className="text-cyan-300 transition hover:text-cyan-200">
              {isRegister ? 'Sign in' : 'Create an account'}
            </button>
          </p>
        </div>
      </section>
    </main>
  )
}
