// All communication with the Go API lives here, so the components never build
// URLs, attach tokens or parse error bodies themselves.

export const API_URL = (process.env.NEXT_PUBLIC_API_URL ?? 'http://localhost:8080').replace(/\/+$/, '')

export type Student = {
  id: number
  name: string
  email: string
  major: string
}

export type StudentInput = Omit<Student, 'id'>

/** One page of the directory, plus the size of the whole (filtered) result. */
export type StudentPage = {
  items: Student[]
  total: number
  limit: number
  offset: number
}

export type StudentStats = {
  total: number
  majors: number
  topMajor: string
  topCount: number
}

export type PageQuery = {
  q: string
  limit: number
  offset: number
}

/** Admins can change the directory; viewers can only read it. */
export type Role = 'admin' | 'viewer'

export type Session = {
  token: string
  email: string
  role: Role
}

const isRole = (value: unknown): value is Role => value === 'admin' || value === 'viewer'

/** An error response from the API (or a failure to reach it, status 0). */
export class ApiError extends Error {
  readonly status: number

  constructor(message: string, status: number) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

// ---------------------------------------------------------------- session

const SESSION_KEY = 'campusflow_session'

/** Reads the `exp` claim (seconds since epoch) from a JWT without verifying it. */
function tokenExpiry(token: string): number | null {
  try {
    const payload = token.split('.')[1]
    const json = atob(payload.replace(/-/g, '+').replace(/_/g, '/'))
    const exp = JSON.parse(json).exp
    return typeof exp === 'number' ? exp : null
  } catch {
    return null
  }
}

export const sessionStore = {
  /** Returns the saved session, or null if there is none or it has expired. */
  load(): Session | null {
    try {
      const raw = localStorage.getItem(SESSION_KEY)
      if (!raw) return null
      const session = JSON.parse(raw) as Session
      const exp = tokenExpiry(session.token)
      // A saved session without a valid role predates roles; sign in again to get one.
      if (!session.token || !session.email || !isRole(session.role) || exp === null || exp * 1000 <= Date.now()) {
        localStorage.removeItem(SESSION_KEY)
        return null
      }
      return session
    } catch {
      return null
    }
  },
  save(session: Session) {
    try {
      localStorage.setItem(SESSION_KEY, JSON.stringify(session))
    } catch {
      // Storage can be blocked (private mode). The session then lasts until reload.
    }
  },
  clear() {
    try {
      localStorage.removeItem(SESSION_KEY)
    } catch {
      // nothing to clear
    }
  },
}

// ---------------------------------------------------------------- requests

async function request<T>(path: string, init: { method?: string; body?: unknown; token?: string } = {}): Promise<T> {
  let response: Response
  try {
    response = await fetch(`${API_URL}${path}`, {
      method: init.method ?? 'GET',
      headers: {
        ...(init.body !== undefined ? { 'Content-Type': 'application/json' } : {}),
        ...(init.token ? { Authorization: `Bearer ${init.token}` } : {}),
      },
      body: init.body !== undefined ? JSON.stringify(init.body) : undefined,
    })
  } catch {
    throw new ApiError(`Cannot reach the server at ${API_URL}. Is the API running?`, 0)
  }

  if (response.status === 204) return undefined as T

  const data: unknown = await response.json().catch(() => null)
  if (!response.ok) {
    const message =
      data && typeof data === 'object' && 'error' in data && typeof data.error === 'string'
        ? data.error
        : `Request failed (${response.status})`
    throw new ApiError(message, response.status)
  }
  return data as T
}

export const api = {
  login: (email: string, password: string) =>
    request<Session>('/api/login', { method: 'POST', body: { email, password } }),

  register: (email: string, password: string) =>
    request<{ id: number; email: string; role: Role }>('/api/register', { method: 'POST', body: { email, password } }),

  listStudents: (token: string, { q, limit, offset }: PageQuery) => {
    const params = new URLSearchParams({ limit: String(limit), offset: String(offset) })
    if (q) params.set('q', q)
    return request<StudentPage>(`/api/students?${params}`, { token })
  },

  studentStats: (token: string) => request<StudentStats>('/api/students/stats', { token }),

  createStudent: (token: string, input: StudentInput) =>
    request<Student>('/api/students', { method: 'POST', body: input, token }),

  updateStudent: (token: string, id: number, input: StudentInput) =>
    request<Student>(`/api/students/${id}`, { method: 'PUT', body: input, token }),

  deleteStudent: (token: string, id: number) =>
    request<void>(`/api/students/${id}`, { method: 'DELETE', token }),
}
