interface ErrorStateProps {
  message: string;
  onRetry?: () => void;
}

export function ErrorState({ message, onRetry }: ErrorStateProps) {
  return (
    <div style={{ padding: 24, background: "var(--error-bg)", borderRadius: "var(--radius-md)", border: "1px solid #3a1a1d" }}>
      <p style={{ color: "var(--error-fg)", margin: "0 0 8px", fontWeight: 600 }}>Error</p>
      <p style={{ color: "#e8a0a0", margin: 0 }}>{message}</p>
      {onRetry && (
        <button onClick={onRetry} style={{ marginTop: 12, padding: "4px 12px" }}>
          Retry
        </button>
      )}
    </div>
  );
}
