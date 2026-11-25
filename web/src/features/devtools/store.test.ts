import { beforeEach, describe, expect, it } from "vitest"
import { DEFAULT_TEXT, _initialStateForTests, useDevTools } from "./store"

const state = () => useDevTools.getState()

beforeEach(() => {
  useDevTools.setState(_initialStateForTests())
})

describe("devtools tabs", () => {
  it("starts with one tab holding the example requests", () => {
    expect(state().tabs).toHaveLength(1)
    expect(state().tabs[0]).toMatchObject({ title: "Console 1", text: DEFAULT_TEXT })
    expect(state().activeId).toBe(state().tabs[0].id)
  })

  it("adds a tab and makes it active", () => {
    const id = state().addTab()
    expect(state().tabs.map((t) => t.title)).toEqual(["Console 1", "Console 2"])
    expect(state().activeId).toBe(id)
  })

  it("adds a tab with given text (for example from the history)", () => {
    state().addTab("GET _cat/indices")
    expect(state().tabs[1].text).toBe("GET _cat/indices")
  })

  it("never reuses a title number after a tab was closed", () => {
    const second = state().addTab()
    state().closeTab(second)
    state().addTab()
    expect(state().tabs.map((t) => t.title)).toEqual(["Console 1", "Console 3"])
  })

  it("activates the left neighbor when the active tab is closed", () => {
    const [first] = state().tabs
    const second = state().addTab()
    const third = state().addTab()
    state().setActive(second)
    state().closeTab(second)
    expect(state().activeId).toBe(first.id)
    expect(state().tabs.map((t) => t.id)).toEqual([first.id, third])
  })

  it("keeps the active tab when another one is closed", () => {
    const [first] = state().tabs
    const second = state().addTab()
    state().closeTab(first.id)
    expect(state().activeId).toBe(second)
  })

  it("always leaves one tab", () => {
    const [only] = state().tabs
    state().closeTab(only.id)
    expect(state().tabs).toHaveLength(1)
    expect(state().tabs[0].id).not.toBe(only.id)
    expect(state().activeId).toBe(state().tabs[0].id)
  })

  it("ignores unknown ids", () => {
    const before = state().tabs
    state().closeTab("nope")
    state().setActive("nope")
    state().setText("nope", "x")
    expect(state().tabs).toEqual(before)
    expect(state().activeId).toBe(before[0].id)
  })

  it("stores text per tab", () => {
    const [first] = state().tabs
    const second = state().addTab()
    state().setText(first.id, "GET a")
    state().setText(second, "GET b")
    expect(state().tabs.map((t) => t.text)).toEqual(["GET a", "GET b"])
  })

  it("renames a tab, ignoring blank names and limiting the length", () => {
    const [tab] = state().tabs
    state().rename(tab.id, "  Prod checks  ")
    expect(state().tabs[0].title).toBe("Prod checks")
    state().rename(tab.id, "   ")
    expect(state().tabs[0].title).toBe("Prod checks")
    state().rename(tab.id, "x".repeat(100))
    expect(state().tabs[0].title).toHaveLength(40)
  })
})
