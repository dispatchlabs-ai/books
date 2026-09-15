import { useCallback, useEffect, useRef, useState } from "react";
import { request, today } from "@/lib/books";
import { summarizeOutlook, type Forecast, type Outlook } from "@/lib/forecast";

export type ForecastState = {
  /** The scenario requested. */
  scenario: string;
  /** The scenario whose data is on screen; it differs while another loads. */
  shown: string;
  data?: Forecast;
  outlook?: Outlook;
  error: string;
  loading: boolean;
  retry: () => void;
};

type Loaded = { data: Forecast; outlook: Outlook; scenario: string };

// Loads one scenario at a time. The last good result stays on screen while
// another scenario loads, so switching doesn't collapse the page.
export function useForecast(company: string, scenario: string): ForecastState {
  const cache = useRef(new Map<string, Loaded>());
  const [state, setState] = useState<{
    key: string;
    loaded?: Loaded;
    error: string;
  }>({ key: "", error: "" });
  const [attempt, setAttempt] = useState(0);
  const key = `${company}/${scenario}`;
  useEffect(() => {
    if (!scenario) return;
    const cached = cache.current.get(key);
    if (cached) {
      setState({ key, loaded: cached, error: "" });
      return;
    }
    const controller = new AbortController();
    request<Forecast>(
      `/books/companies/${encodeURIComponent(company)}/cash-forecast?scenario=${encodeURIComponent(scenario)}`,
      controller.signal,
    )
      .then((data) => {
        if (controller.signal.aborted) return;
        const loaded = {
          data,
          outlook: summarizeOutlook(data, today()),
          scenario,
        };
        cache.current.set(key, loaded);
        setState({ key, loaded, error: "" });
      })
      .catch((e) => {
        if (!controller.signal.aborted)
          setState((s) => ({ ...s, key, error: e.message }));
      });
    return () => controller.abort();
  }, [company, scenario, key, attempt]);
  const retry = useCallback(() => {
    cache.current.clear();
    setState((s) => ({ ...s, key: "", error: "" }));
    setAttempt((n) => n + 1);
  }, []);
  const current = state.key === key;
  return {
    scenario,
    shown: state.loaded?.scenario ?? scenario,
    data: state.loaded?.data,
    outlook: state.loaded?.outlook,
    error: current ? state.error : "",
    loading: !current,
    retry,
  };
}
