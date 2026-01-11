import { useCallback, useEffect, useRef } from "react";
import { useKeyboard } from "@opentui/react";
import type { KeyEvent } from "@opentui/core";

export function useKeyboardStable(
  handler: (event: KeyEvent) => void,
  enabled = true
) {
  const handlerRef = useRef(handler);
  const enabledRef = useRef(enabled);

  useEffect(() => {
    handlerRef.current = handler;
  }, [handler]);

  useEffect(() => {
    enabledRef.current = enabled;
  }, [enabled]);

  const stableHandler = useCallback((event: KeyEvent) => {
    if (!enabledRef.current) return;
    handlerRef.current(event);
  }, []);

  useKeyboard(stableHandler);
}
