import { expect, it } from "vitest";
import { orderedAccounts, reorderedView } from "./cash-view";
it("keeps unknown accounts at the end in their existing order without mutating input", () => {
  const rows = [{ code: "a" }, { code: "b" }, { code: "c" }, { code: "d" }];
  expect(
    orderedAccounts(rows, ["b", "missing", "a"]).map((r) => r.code),
  ).toEqual(["b", "a", "c", "d"]);
  expect(rows.map((r) => r.code)).toEqual(["a", "b", "c", "d"]);
});
it("retains hidden and temporarily missing slots while visible rows move", () => {
  const view = {
    order: ["a", "hidden", "b", "missing", "c"],
    hidden: ["hidden"],
  };
  expect(
    reorderedView(
      view,
      ["a", "hidden", "b", "c", "new"],
      ["new", "c", "a", "b"],
    ),
  ).toEqual({
    order: ["new", "hidden", "c", "missing", "a", "b"],
    hidden: ["hidden"],
  });
});
