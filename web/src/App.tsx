import { NavLink, Navigate, Route, Routes } from "react-router-dom"
import { Activity, Bot, Grid3x3, MoveRight, Terminal } from "lucide-react"
import { cn } from "@/lib/utils"
import ClusterMatrix from "@/pages/ClusterMatrix"
import DevTools from "@/pages/DevTools"
import Telemetry from "@/pages/Telemetry"
import Migration from "@/pages/Migration"
import Inspector from "@/pages/Inspector"

const nav = [
  { to: "/matrix", label: "Cluster Matrix", icon: Grid3x3 },
  { to: "/devtools", label: "DevTools Pro", icon: Terminal },
  { to: "/telemetry", label: "Telemetry & Logs", icon: Activity },
  { to: "/migration", label: "Migration", icon: MoveRight },
  { to: "/inspector", label: "AI Inspector", icon: Bot },
]

export default function App() {
  return (
    <div className="flex h-screen">
      <aside className="w-56 shrink-0 border-r border-border p-3">
        <div className="mb-4 px-2 text-lg font-semibold">Searchboard</div>
        <nav className="flex flex-col gap-1">
          {nav.map(({ to, label, icon: Icon }) => (
            <NavLink
              key={to}
              to={to}
              className={({ isActive }) =>
                cn(
                  "flex items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-muted",
                  isActive && "bg-muted font-medium",
                )
              }
            >
              <Icon className="size-4" />
              {label}
            </NavLink>
          ))}
        </nav>
      </aside>
      <main className="flex-1 overflow-auto p-6">
        <Routes>
          <Route path="/" element={<Navigate to="/matrix" replace />} />
          <Route path="/matrix" element={<ClusterMatrix />} />
          <Route path="/devtools" element={<DevTools />} />
          <Route path="/telemetry" element={<Telemetry />} />
          <Route path="/migration" element={<Migration />} />
          <Route path="/inspector" element={<Inspector />} />
        </Routes>
      </main>
    </div>
  )
}
