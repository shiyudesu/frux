interface BrandMarkProps {
  compact?: boolean;
}

export function BrandMark({ compact = false }: BrandMarkProps) {
  return (
    <span className={`brand-wordmark ${compact ? "compact" : ""}`} aria-label="FRUX">
      <span className="brand-symbol" aria-hidden="true">
        <span className="brand-symbol-shadow">F</span>
        <span className="brand-symbol-face">F</span>
      </span>
      {!compact && <span className="brand-name">FRUX</span>}
    </span>
  );
}
