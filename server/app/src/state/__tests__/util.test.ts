import { describe, it, expect } from "vitest";
import { getWebsocketURL, getApiURL } from "../util";

// Vitest sets import.meta.env.DEV = true, so the dev-port rewrite to 5228
// is exercised here. jsdom defaults to http://localhost:3000/.

describe("getWebsocketURL", () => {
  it("rewrites http -> ws and appends /ws", () => {
    const url = getWebsocketURL();
    expect(url.protocol).toBe("ws:");
    expect(url.pathname).toBe("/ws");
  });

  it("rewrites the dev port to 5228", () => {
    const url = getWebsocketURL();
    expect(url.port).toBe("5228");
  });
});

describe("getApiURL", () => {
  it("rewrites the dev port to 5228", () => {
    const url = getApiURL();
    expect(url.port).toBe("5228");
  });

  it("preserves the http origin", () => {
    const url = getApiURL();
    expect(url.protocol).toBe("http:");
    expect(url.hostname).toBe("localhost");
  });
});
