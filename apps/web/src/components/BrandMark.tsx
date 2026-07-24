interface BrandMarkProps {
  compact?: boolean;
}

export function BrandMark({ compact = false }: BrandMarkProps) {
  return (
    <span className={`brand-wordmark ${compact ? "compact" : ""}`} aria-label="GCFeed">
      <span className="brand-symbol" aria-hidden="true">
        <span className="brand-symbol-shadow">G</span>
        <span className="brand-symbol-face">G</span>
      </span>
      {!compact && <span className="brand-name">GCFeed</span>}
    </span>
  );
}
