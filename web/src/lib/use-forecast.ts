import { useCallback, useEffect, useRef, useState } from "react";
import { request } from "@/lib/books";
import { summarizeOutlook, type Forecast, type Outlook } from "@/lib/forecast";

export type ForecastState = {
  scenario: string;
  data?: Forecast;
  outlook?: Outlook;
  error: string;
  loading: boolean;
  retry: () => void;
};

// Loads one scenario at a time. The last good result stays on screen while
// another scenario loads, so switching doesn't collapse the page.
export function useForecast(company: string, scenario: string): ForecastState {
  const cache = useRef(new Map<string, { data: Forecast; outlook: Outlook }>());
  const [state, setState] = useState<{
    key: string;
    data?: Forecast;
    outlook?: Outlook;
    error: string;
  }>({ key: "", error: "" });
  const [attempt, setAttempt] = useState(0);
  const key = `${company}/${scenario}`;
  useEffect(() => {
    if (!scenario) return;
    const cached = cache.current.get(key);
    if (cached) {
      setState({ key, ...cached, error: "" });
      return;
    }
    const controller = new AbortController();
    request<Forecast>(
      `/books/companies/${encodeURIComponent(company)}/cash-forecast?scenario=${encodeURIComponent(scenario)}`,
      controller.signal,
    )
      .then((data) => {
        if (controller.signal.aborted) return;
        const value = { data, outlook: summarizeOutlook(data) };
        cache.current.set(key, value);
        setState({ key, ...value, error: "" });
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
    data: state.data,
    outlook: state.outlook,
    error: current ? state.error : "",
    loading: !current,
    retry,
  };
}
