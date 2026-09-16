import { useEffect, useRef, useState } from "react";
import { request } from "./books";

export type CashView = { order: string[]; hidden: string[] };
type SavedView = CashView & { revision: string };
const empty = (): SavedView => ({ order: [], hidden: [], revision: "" });

export function orderedAccounts<T extends { code: string }>(
  rows: T[],
  order: string[],
) {
  const positions = new Map(order.map((code, i) => [code, i]));
  return [...rows].sort(
    (a, b) =>
      (positions.get(a.code) ?? Infinity) - (positions.get(b.code) ?? Infinity),
  );
}

// Reordering the visible list preserves hidden/missing accounts' saved slots.
export function reorderedView(
  view: CashView,
  all: string[],
  visible: string[],
): CashView {
  const full = [...new Set([...view.order, ...all])];
  const shown = new Set(visible);
  let i = 0;
  return {
    ...view,
    order: full.map((code) => (shown.has(code) ? visible[i++] : code)),
  };
}

export function useCashView(company: string, shared: boolean, scope: string) {
  const key = `books.cash-view.${scope}`;
  const [view, setView] = useState<SavedView>(empty);
  const [ready, setReady] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [generation, setGeneration] = useState(0);
  const busy = useRef(false);
  const endpoint = `/books/companies/${encodeURIComponent(company)}/cash-view`;
  useEffect(() => {
    const controller = new AbortController();
    setReady(false);
    setError("");
    const read = async () => {
      if (shared) return request<SavedView>(endpoint, controller.signal);
      // Demo and unconfigured local previews keep view preferences in the browser.
      try {
        const data = JSON.parse(localStorage.getItem(key) ?? "null");
        if (
          data &&
          Array.isArray(data.order) &&
          Array.isArray(data.hidden) &&
          [...data.order, ...data.hidden].every((v) => typeof v === "string")
        )
          return {
            order: [...new Set<string>(data.order)],
            hidden: [...new Set<string>(data.hidden)],
            revision: "",
          };
      } catch {
        /* Missing, corrupt or blocked storage starts with all accounts. */
      }
      return empty();
    };
    read()
      .then((result) => {
        if (!controller.signal.aborted) {
          setView(result);
          setReady(true);
        }
      })
      .catch(() => {
        if (!controller.signal.aborted)
          setError("Could not load the account arrangement.");
      });
    return () => controller.abort();
  }, [endpoint, shared, key, generation]);
  return {
    view,
    ready,
    saving,
    error,
    reload: () => setGeneration((v) => v + 1),
    update: async (next: CashView) => {
      if (busy.current || !ready) return false;
      busy.current = true;
      setSaving(true);
      setError("");
      const previous = view;
      setView({ ...next, revision: previous.revision });
      try {
        if (shared) {
          const saved = await request<SavedView>(endpoint, undefined, {
            ...next,
            revision: previous.revision,
          });
          setView(saved);
        } else {
          localStorage.setItem(key, JSON.stringify(next));
        }
        return true;
      } catch (e) {
        setView(previous);
        setError(
          shared && e instanceof Error
            ? e.message
            : "Could not save the account arrangement.",
        );
        return false;
      } finally {
        busy.current = false;
        setSaving(false);
      }
    },
  };
}
