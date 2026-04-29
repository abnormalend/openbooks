import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { MessageType, NotificationType } from "../messages";

// Cross-language enum drift catcher.
//
// MessageType / NotificationType are sent over the wire as integers and
// must match server/messages.go on the Go side. If you add or reorder
// constants in either file, this test fails until both sides agree.
//
// Tests run from server/app/, so navigate to ../messages.go (i.e. server/messages.go).
const goSourcePath = resolve(process.cwd(), "../messages.go");

function extractGoEnum(src: string, typeName: string): Record<string, number> {
  // Walk every const ( ... ) block; pick the one that mentions typeName.
  const blockRe = /const\s*\(([\s\S]*?)\)/g;
  for (const match of src.matchAll(blockRe)) {
    const body = match[1];
    if (!body.includes(typeName)) continue;
    const out: Record<string, number> = {};
    let next = 0;
    for (const raw of body.split("\n")) {
      const line = raw.replace(/\/\/.*$/, "").trim();
      if (!line) continue;
      const m = line.match(/^([A-Z][A-Z0-9_]*)(?:\s+\w+)?(?:\s*=\s*(.+))?$/);
      if (!m) continue;
      const [, name, expr] = m;
      if (expr === undefined || expr === "iota") {
        out[name] = next++;
      } else if (/^\d+$/.test(expr)) {
        out[name] = parseInt(expr, 10);
        next = out[name] + 1;
      }
    }
    if (Object.keys(out).length > 0) return out;
  }
  throw new Error(`could not find const block for ${typeName} in Go source`);
}

function tsEnumKeys(e: Record<string, string | number>): string[] {
  // Numeric TS enums are reverse-mapped (both string->number and number->string entries).
  // Filter to the canonical string keys.
  return Object.keys(e).filter((k) => isNaN(Number(k)));
}

describe("Go <-> TS enum mirror", () => {
  const goSrc = readFileSync(goSourcePath, "utf8");

  it("MessageType matches server/messages.go", () => {
    const goEnum = extractGoEnum(goSrc, "MessageType");
    expect(new Set(tsEnumKeys(MessageType as unknown as Record<string, number>))).toEqual(
      new Set(Object.keys(goEnum))
    );
    for (const [name, val] of Object.entries(goEnum)) {
      expect((MessageType as unknown as Record<string, number>)[name]).toBe(val);
    }
  });

  it("NotificationType matches server/messages.go", () => {
    const goEnum = extractGoEnum(goSrc, "NotificationType");
    expect(new Set(tsEnumKeys(NotificationType as unknown as Record<string, number>))).toEqual(
      new Set(Object.keys(goEnum))
    );
    for (const [name, val] of Object.entries(goEnum)) {
      expect((NotificationType as unknown as Record<string, number>)[name]).toBe(val);
    }
  });
});
