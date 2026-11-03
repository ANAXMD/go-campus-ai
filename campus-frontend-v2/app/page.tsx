'use client'

import { useEffect, useState } from 'react'
import { LoginScreen } from '@/components/login-screen'
import { StudentsScreen } from '@/components/students-screen'
import { sessionStore, type Session } from '@/lib/api'

export default function Page() {
  // undefined = still reading localStorage (it does not exist during server rendering)
  const [session, setSession] = useState<Session | null | undefined>(undefined)

  useEffect(() => {
    setSession(sessionStore.load())
  }, [])

  function handleLogin(next: Session) {
    sessionStore.save(next)
    setSession(next)
  }

  function handleLogout() {
    sessionStore.clear()
    setSession(null)
  }

  if (session === undefined) return <main className="min-h-screen bg-[#07111f]" />
  if (session === null) return <LoginScreen onLogin={handleLogin} />
  return <StudentsScreen session={session} onLogout={handleLogout} />
}
