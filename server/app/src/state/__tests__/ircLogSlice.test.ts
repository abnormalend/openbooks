import { describe, it, expect } from "vitest";
import ircLogReducer, {
  appendEntry,
  clearEntries,
  MAX_ENTRIES
} from "../ircLogSlice";

const make = (i: number) => ({ line: `line ${i}`, timestamp: i });

describe("ircLogSlice", () => {
  it("appendEntry adds to the tail (chronological order)", () => {
    let state = ircLogReducer(undefined, appendEntry(make(1)));
    state = ircLogReducer(state, appendEntry(make(2)));
    state = ircLogReducer(state, appendEntry(make(3)));
    expect(state.entries.map((e) => e.timestamp)).toEqual([1, 2, 3]);
  });

  it("appendEntry caps at MAX_ENTRIES, dropping oldest", () => {
    let state = ircLogReducer(undefined, { type: "@@INIT" });
    for (let i = 0; i < MAX_ENTRIES + 50; i++) {
      state = ircLogReducer(state, appendEntry(make(i)));
    }
    expect(state.entries).toHaveLength(MAX_ENTRIES);
    // Oldest should now be index 50 (we dropped 0-49).
    expect(state.entries[0].timestamp).toBe(50);
    expect(state.entries[MAX_ENTRIES - 1].timestamp).toBe(MAX_ENTRIES + 49);
  });

  it("clearEntries empties the log", () => {
    let state = ircLogReducer(undefined, appendEntry(make(1)));
    state = ircLogReducer(state, appendEntry(make(2)));
    state = ircLogReducer(state, clearEntries());
    expect(state.entries).toEqual([]);
  });
});
