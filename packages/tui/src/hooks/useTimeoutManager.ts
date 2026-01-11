import { useCallback, useEffect, useRef } from "react";

export function useTimeoutManager() {
  const timers = useRef<Array<ReturnType<typeof setTimeout>>>([]);

  useEffect(() => {
    return () => {
      for (const timer of timers.current) {
        clearTimeout(timer);
      }
      timers.current = [];
    };
  }, []);

  return useCallback((fn: () => void, ms: number) => {
    let id: ReturnType<typeof setTimeout>;
    id = setTimeout(() => {
      try {
        fn();
      } finally {
        timers.current = timers.current.filter((t) => t !== id);
      }
    }, ms);
    timers.current.push(id);
    return id;
  }, []);
}
