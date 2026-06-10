import { LogoWithText } from "./Logo";

export function AuthLayout({ children }: { children: React.ReactNode }) {
  return (
    <div
      style={{
        minHeight: "100vh",
        width: "100%",
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        background: "var(--bg-root)",
        padding: 24,
        gap: 28,
      }}
    >
      <LogoWithText size={36} />
      <div
        style={{
          width: "100%",
          maxWidth: 420,
          background: "var(--bg-secondary)",
          border: "1px solid var(--border-primary)",
          borderRadius: "var(--radius-lg)",
          boxShadow: "var(--shadow-lg)",
          padding: "28px 28px 24px",
        }}
      >
        {children}
      </div>
    </div>
  );
}
