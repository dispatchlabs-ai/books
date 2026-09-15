import { describe, it, expect } from "vitest";
import { decimalMinor, decimalValue } from "./budget";
describe("budget amounts", () => {
  it("preserves exact units and unset versus zero", () => {
    expect(decimalMinor("", 2)).toBeNull();
    expect(decimalMinor("0", 2)).toBe("0");
    expect(decimalMinor("123.45", 2)).toBe("12345");
    expect(decimalValue("12345", 2)).toBe("123.45");
    expect(decimalMinor("23", 0)).toBe("23");
    expect(decimalMinor("12.345", 3)).toBe("12345");
  });
  it("rejects negatives, excess precision and unsafe magnitude", () => {
    for (const s of ["-1", "1.001", "1e5", "123abc", "92233720368547758.08"])
      expect(() => decimalMinor(s, 2)).toThrow();
  });
});
