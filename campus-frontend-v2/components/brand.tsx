import { GraduationCap } from 'lucide-react'

export function Brand() {
  return (
    <div className="flex items-center gap-3">
      <div className="flex size-10 items-center justify-center rounded-xl bg-cyan-400 text-slate-950 shadow-[0_0_24px_rgba(34,211,238,0.18)]">
        <GraduationCap className="size-5" strokeWidth={2.5} />
      </div>
      <div>
        <p className="text-sm font-bold tracking-wide text-white">
          Student <span className="text-cyan-300">Directory</span>
        </p>
        <p className="text-[10px] font-medium uppercase tracking-[0.22em] text-slate-500">Management system</p>
      </div>
    </div>
  )
}
