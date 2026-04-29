import { describe, it, expect, beforeEach } from "vitest";
import historyReducer, {
  addHistoryItem,
  updateHistoryItem
} from "../historySlice";

describe("historySlice", () => {
  beforeEach(() => {
    // Reducer's initial state hydrates from localStorage; reset between tests.
    localStorage.clear();
  });

  it("addHistoryItem prepends new entries", () => {
    let state = historyReducer(undefined, { type: "@@INIT" });
    state = historyReducer(state, addHistoryItem({ query: "first", timestamp: 1 }));
    state = historyReducer(state, addHistoryItem({ query: "second", timestamp: 2 }));
    expect(state.items.map((x) => x.query)).toEqual(["second", "first"]);
  });

  it("addHistoryItem caps at 16 entries", () => {
    let state = historyReducer(undefined, { type: "@@INIT" });
    for (let i = 0; i < 20; i++) {
      state = historyReducer(state, addHistoryItem({ query: `q${i}`, timestamp: i }));
    }
    expect(state.items).toHaveLength(16);
    // Most recent insert is q19 at index 0; oldest kept is q4 (q0..q3 dropped).
    expect(state.items[0].query).toBe("q19");
    expect(state.items[15].query).toBe("q4");
  });

  it("updateHistoryItem replaces by timestamp without changing position", () => {
    let state = historyReducer(undefined, addHistoryItem({ query: "foo", timestamp: 1 }));
    state = historyReducer(state, addHistoryItem({ query: "bar", timestamp: 2 }));
    state = historyReducer(
      state,
      updateHistoryItem({ query: "foo!", timestamp: 1, results: [] })
    );
    const updated = state.items.find((x) => x.timestamp === 1);
    expect(updated?.query).toBe("foo!");
    expect(updated?.results).toEqual([]);
    // bar is still first (most recently added).
    expect(state.items[0].timestamp).toBe(2);
  });
});
