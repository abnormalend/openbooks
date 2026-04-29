import { describe, it, expect } from "vitest";
import notificationReducer, {
  addNotification,
  dismissNotification,
  toggleDrawer,
  clearNotifications
} from "../notificationSlice";
import { NotificationType } from "../messages";

const make = (timestamp: number) => ({
  appearance: NotificationType.NOTIFY,
  title: `n${timestamp}`,
  timestamp
});

describe("notificationSlice", () => {
  it("addNotification prepends to the list", () => {
    let state = notificationReducer(undefined, addNotification(make(1)));
    state = notificationReducer(state, addNotification(make(2)));
    expect(state.notifications.map((n) => n.timestamp)).toEqual([2, 1]);
  });

  it("dismissNotification removes by timestamp", () => {
    let state = notificationReducer(undefined, addNotification(make(1)));
    state = notificationReducer(state, addNotification(make(2)));
    state = notificationReducer(state, dismissNotification(make(1)));
    expect(state.notifications.map((n) => n.timestamp)).toEqual([2]);
  });

  it("toggleDrawer flips isOpen", () => {
    let state = notificationReducer(undefined, { type: "@@INIT" });
    expect(state.isOpen).toBe(false);
    state = notificationReducer(state, toggleDrawer());
    expect(state.isOpen).toBe(true);
    state = notificationReducer(state, toggleDrawer());
    expect(state.isOpen).toBe(false);
  });

  it("clearNotifications empties the list", () => {
    let state = notificationReducer(undefined, addNotification(make(1)));
    state = notificationReducer(state, addNotification(make(2)));
    state = notificationReducer(state, clearNotifications());
    expect(state.notifications).toEqual([]);
  });
});
