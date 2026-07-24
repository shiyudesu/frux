import { useEffect, useRef } from "react";

export function useDialogFocus<T extends HTMLElement>(open: boolean, onDismiss?: () => void) {
  const initialFocusRef = useRef<T | null>(null);
  const dismissRef = useRef(onDismiss);
  dismissRef.current = onDismiss;

  useEffect(() => {
    if (!open) return undefined;
    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const frame = window.requestAnimationFrame(() => initialFocusRef.current?.focus());
    const inerted: Array<{ element: HTMLElement; alreadyInert: boolean }> = [];
    const dialog = initialFocusRef.current?.closest<HTMLElement>("[role='dialog']");
    let activeBranch = dialog;
    while (activeBranch?.parentElement && activeBranch.parentElement.id !== "root") {
      const parent = activeBranch.parentElement;
      for (const child of parent.children) {
        if (!(child instanceof HTMLElement) || child === activeBranch || child.hasAttribute("data-dialog-allow")) continue;
        inerted.push({ element: child, alreadyInert: child.hasAttribute("inert") });
        child.setAttribute("inert", "");
      }
      activeBranch = parent;
    }

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        dismissRef.current?.();
        return;
      }
      if (event.key !== "Tab" || !dialog) return;
      const focusable = [
        ...dialog.querySelectorAll<HTMLElement>(
          "button:not(:disabled), a[href], input:not(:disabled), textarea:not(:disabled), select:not(:disabled), [tabindex]:not([tabindex='-1'])"
        )
      ].filter((element) => !element.hasAttribute("hidden"));
      if (!focusable.length) {
        event.preventDefault();
        return;
      }
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };
    document.addEventListener("keydown", handleKeyDown);

    return () => {
      window.cancelAnimationFrame(frame);
      document.removeEventListener("keydown", handleKeyDown);
      for (const { element, alreadyInert } of inerted) {
        if (!alreadyInert) element.removeAttribute("inert");
      }
      previousFocus?.focus();
    };
  }, [open]);

  return initialFocusRef;
}
