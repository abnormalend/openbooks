import { describe, it, expect, beforeEach } from "vitest";
import stateReducer, {
  setActiveItem,
  setConnectionState,
  setUsername,
  addInFlightDownload,
  removeInFlightDownload,
  toggleSidebar
} from "../stateSlice";

describe("stateSlice", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it("toggleSidebar flips isSidebarOpen", () => {
    let state = stateReducer(undefined, { type: "@@INIT" });
    expect(state.isSidebarOpen).toBe(true);
    state = stateReducer(state, toggleSidebar());
    expect(state.isSidebarOpen).toBe(false);
    state = stateReducer(state, toggleSidebar());
    expect(state.isSidebarOpen).toBe(true);
  });

  it("setConnectionState sets isConnected", () => {
    let state = stateReducer(undefined, setConnectionState(true));
    expect(state.isConnected).toBe(true);
    state = stateReducer(state, setConnectionState(false));
    expect(state.isConnected).toBe(false);
  });

  it("setUsername stores the IRC username", () => {
    const state = stateReducer(undefined, setUsername("evan_28"));
    expect(state.username).toBe("evan_28");
  });

  it("addInFlightDownload + removeInFlightDownload behave as a FIFO", () => {
    let state = stateReducer(undefined, addInFlightDownload("!Bot foo.epub"));
    state = stateReducer(state, addInFlightDownload("!Bot bar.epub"));
    expect(state.inFlightDownloads).toEqual(["!Bot foo.epub", "!Bot bar.epub"]);
    state = stateReducer(state, removeInFlightDownload());
    expect(state.inFlightDownloads).toEqual(["!Bot bar.epub"]);
  });

  it("setActiveItem stores or clears the active history item", () => {
    let state = stateReducer(undefined, setActiveItem({ query: "x", timestamp: 1 }));
    expect(state.activeItem?.query).toBe("x");
    state = stateReducer(state, setActiveItem(null));
    expect(state.activeItem).toBeNull();
  });
});
