import { create } from "zustand"
import { createJSONStorage, persist, type StateStorage } from "zustand/middleware"

export interface Tab {
  id: string
  title: string
  text: string
}

export const DEFAULT_TEXT = `# Run the request under the cursor with Ctrl/Cmd+Enter.
GET _cluster/health

POST _search
{
  "query": { "match_all": {} }
}
`

interface DevToolsState {
  tabs: Tab[]
  activeId: string
  /** Counter for default tab titles, so closing a tab never reuses a number. */
  counter: number
  addTab: (text?: string) => string
  closeTab: (id: string) => void
  setActive: (id: string) => void
  setText: (id: string, text: string) => void
  rename: (id: string, title: string) => void
}

// Without localStorage (tests, private modes that block it) tabs live in memory only.
const memory = new Map<string, string>()
const memoryStorage: StateStorage = {
  getItem: (key) => memory.get(key) ?? null,
  setItem: (key, value) => void memory.set(key, value),
  removeItem: (key) => void memory.delete(key),
}

let fallbackId = 0
function newId(): string {
  return globalThis.crypto?.randomUUID?.() ?? `tab-${Date.now()}-${fallbackId++}`
}

function freshTab(number: number, text = DEFAULT_TEXT): Tab {
  return { id: newId(), title: `Console ${number}`, text }
}

function initialState() {
  const first = freshTab(1)
  return { tabs: [first], activeId: first.id, counter: 1 }
}

export const useDevTools = create<DevToolsState>()(
  persist(
    (set, get) => ({
      ...initialState(),

      addTab: (text) => {
        const counter = get().counter + 1
        const tab = freshTab(counter, text)
        set((state) => ({ tabs: [...state.tabs, tab], activeId: tab.id, counter }))
        return tab.id
      },

      closeTab: (id) => {
        const { tabs, activeId, counter } = get()
        const index = tabs.findIndex((tab) => tab.id === id)
        if (index === -1) return

        const remaining = tabs.filter((tab) => tab.id !== id)
        if (remaining.length === 0) {
          const tab = freshTab(counter + 1)
          set({ tabs: [tab], activeId: tab.id, counter: counter + 1 })
          return
        }
        // Closing the active tab activates its neighbor to the left, or the first one.
        const nextActive = id === activeId ? remaining[Math.max(index - 1, 0)].id : activeId
        set({ tabs: remaining, activeId: nextActive })
      },

      setActive: (id) => {
        if (get().tabs.some((tab) => tab.id === id)) set({ activeId: id })
      },

      setText: (id, text) => set((state) => ({ tabs: state.tabs.map((tab) => (tab.id === id ? { ...tab, text } : tab)) })),

      rename: (id, title) => {
        const trimmed = title.trim()
        if (trimmed === "") return
        set((state) => ({ tabs: state.tabs.map((tab) => (tab.id === id ? { ...tab, title: trimmed.slice(0, 40) } : tab)) }))
      },
    }),
    {
      name: "searchboard-devtools",
      version: 1,
      storage: createJSONStorage(() => (typeof localStorage === "undefined" ? memoryStorage : localStorage)),
      partialize: (state) => ({ tabs: state.tabs, activeId: state.activeId, counter: state.counter }),
    },
  ),
)

export { initialState as _initialStateForTests }
