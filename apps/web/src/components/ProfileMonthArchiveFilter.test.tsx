// @vitest-environment jsdom
import { act, useState } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ProfileMonthArchiveFilter } from "./ProfileDashboard";

describe("ProfileMonthArchiveFilter", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    vi.useRealTimers();
  });

  it("shows loading, empty, and retryable error states", () => {
    const retry = vi.fn();
    act(() => root.render(
      <ProfileMonthArchiveFilter
        error=""
        months={[]}
        state="loading"
        value=""
        onChange={() => {}}
        onRetry={retry}
      />
    ));
    act(() => trigger().click());
    expect(container.textContent).toContain("正在加载日期");

    act(() => root.render(
      <ProfileMonthArchiveFilter
        error=""
        months={[]}
        state="ready"
        value=""
        onChange={() => {}}
        onRetry={retry}
      />
    ));
    expect(container.textContent).toContain("暂无可筛选月份");

    act(() => root.render(
      <ProfileMonthArchiveFilter
        error="作品日期加载失败"
        months={[]}
        state="error"
        value=""
        onChange={() => {}}
        onRetry={retry}
      />
    ));
    act(() => button("重试").click());
    expect(retry).toHaveBeenCalledTimes(1);
  });

  it("keeps the panel open during the pointer leave grace period", () => {
    vi.useFakeTimers();
    renderControlled();
    const rootElement = required<HTMLElement>(".profile-month-archive");
    act(() => rootElement.dispatchEvent(new MouseEvent("mouseover", { bubbles: true })));
    expect(dialog()).toBeTruthy();

    act(() => rootElement.dispatchEvent(new MouseEvent("mouseout", {
      bubbles: true,
      relatedTarget: document.body
    })));
    act(() => vi.advanceTimersByTime(499));
    expect(dialog()).toBeTruthy();
    act(() => vi.advanceTimersByTime(1));
    expect(container.querySelector('[role="dialog"]')).toBeNull();
  });

  it("selects a year's first month, an exact month, and all", async () => {
    renderControlled();
    act(() => trigger().click());
    act(() => button("2025年").click());
    expect(trigger().textContent).toContain("2025年12月");

    act(() => trigger().click());
    const year2026 = button("2026年");
    await act(async () => {
      year2026.focus();
      await Promise.resolve();
    });
    act(() => button("2月").click());
    expect(trigger().textContent).toContain("2026年2月");

    act(() => trigger().click());
    act(() => button("全部").click());
    expect(trigger().textContent).toContain("日期筛选");
  });

  it("dismisses outside and restores trigger focus on Escape", () => {
    renderControlled();
    const triggerButton = trigger();
    act(() => triggerButton.click());
    act(() => document.body.dispatchEvent(new MouseEvent("pointerdown", { bubbles: true })));
    expect(container.querySelector('[role="dialog"]')).toBeNull();

    act(() => triggerButton.click());
    act(() => document.dispatchEvent(new KeyboardEvent("keydown", {
      key: "Escape",
      bubbles: true
    })));
    expect(container.querySelector('[role="dialog"]')).toBeNull();
    expect(document.activeElement).toBe(triggerButton);
  });

  it("supports keyboard movement across year and month columns", () => {
    renderControlled("2026-08");
    const triggerButton = trigger();
    act(() => triggerButton.dispatchEvent(new KeyboardEvent("keydown", {
      key: "ArrowDown",
      bubbles: true
    })));
    expect(document.activeElement?.textContent?.trim()).toBe("2026年");

    act(() => document.activeElement?.dispatchEvent(new KeyboardEvent("keydown", {
      key: "ArrowDown",
      bubbles: true
    })));
    expect(document.activeElement?.textContent?.trim()).toBe("2025年");

    act(() => document.activeElement?.dispatchEvent(new KeyboardEvent("keydown", {
      key: "ArrowUp",
      bubbles: true
    })));
    act(() => document.activeElement?.dispatchEvent(new KeyboardEvent("keydown", {
      key: "ArrowRight",
      bubbles: true
    })));
    expect(document.activeElement?.textContent?.trim()).toBe("8月");

    act(() => document.activeElement?.dispatchEvent(new KeyboardEvent("keydown", {
      key: "End",
      bubbles: true
    })));
    expect(document.activeElement?.textContent?.trim()).toBe("2月");
    act(() => document.activeElement?.dispatchEvent(new KeyboardEvent("keydown", {
      key: "ArrowLeft",
      bubbles: true
    })));
    expect(document.activeElement?.textContent?.trim()).toBe("2026年");
  });

  function renderControlled(initialValue = "") {
    act(() => root.render(<ControlledArchive initialValue={initialValue} />));
  }

  function trigger(): HTMLButtonElement {
    return required<HTMLButtonElement>(".profile-month-trigger");
  }

  function dialog(): HTMLElement {
    return required<HTMLElement>('[role="dialog"]');
  }

  function button(text: string): HTMLButtonElement {
    const match = [...container.querySelectorAll<HTMLButtonElement>("button")]
      .find((candidate) => candidate.textContent?.trim() === text);
    if (!match) throw new Error(`button not found: ${text}`);
    return match;
  }

  function required<T extends Element>(selector: string): T {
    const element = container.querySelector<T>(selector);
    if (!element) throw new Error(`missing ${selector}`);
    return element;
  }
});

function ControlledArchive({ initialValue }: { initialValue: string }) {
  const [value, setValue] = useState(initialValue);
  return (
    <ProfileMonthArchiveFilter
      error=""
      months={["2026-08", "2026-02", "2025-12"]}
      state="ready"
      value={value}
      onChange={setValue}
      onRetry={() => {}}
    />
  );
}
