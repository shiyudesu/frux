// Feed 右侧操作栏按钮。
interface ActionButtonProps {
  icon: string;
  label: string;
  active?: boolean;
  compact?: boolean;
  onClick?: () => void;
}

export function ActionButton({ icon, label, active, compact, onClick }: ActionButtonProps) {
  return (
    <button className={`rail-button ${active ? "active" : ""} ${compact ? "compact" : ""}`} onClick={onClick}>
      <span className={`material-symbols-outlined ${active ? "filled" : ""}`}>{icon}</span>
      {label && <strong>{label}</strong>}
    </button>
  );
}
