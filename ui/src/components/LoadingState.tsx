export function LoadingState() {
  return (
    <div style={{ display: "flex", alignItems: "center", gap: 8, padding: 24, color: "var(--text-secondary)" }}>
      <div
        style={{
          width: 20,
          height: 20,
          border: "2px solid var(--border-primary)",
          borderTopColor: "var(--gold-500)",
          borderRadius: "50%",
          animation: "spin 0.6s linear infinite",
        }}
      />
      Loading...
    </div>
  );
}
