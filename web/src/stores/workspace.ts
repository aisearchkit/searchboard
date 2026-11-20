import { create } from "zustand"
import { persist } from "zustand/middleware"

interface WorkspaceState {
  clusterId: string | null
  setCluster: (id: string | null) => void
}

export const useWorkspace = create<WorkspaceState>()(
  persist(
    (set) => ({ clusterId: null, setCluster: (clusterId) => set({ clusterId }) }),
    { name: "searchboard-workspace" },
  ),
)
