import type { Ref } from "react";
import { Icon } from "./Icon";
import type { IconName } from "./Icon";

interface ActionButtonProps {
  icon: IconName;
  label: string;
  ariaLabel: string;
  active?: boolean;
  compact?: boolean;
  dataUI?: string;
  buttonRef?: Ref<HTMLButtonElement>;
  ariaExpanded?: boolean;
  ariaHasPopup?: "menu";
  onClick?: () => void;
}

export function ActionButton({
  icon,
  label,
  ariaLabel,
  active,
  compact,
  dataUI,
  buttonRef,
  ariaExpanded,
  ariaHasPopup,
  onClick
}: ActionButtonProps) {
  return (
    <button
      aria-label={ariaLabel}
      aria-pressed={active === undefined ? undefined : active}
      aria-expanded={ariaExpanded}
      aria-haspopup={ariaHasPopup}
      className={`rail-button ${active ? "active" : ""} ${compact ? "compact" : ""}`}
      data-ui={dataUI}
      ref={buttonRef}
      type="button"
      onClick={onClick}
    >
      <span className="rail-icon"><Icon name={icon} filled={active} /></span>
      {label && <strong>{label}</strong>}
    </button>
  );
}
