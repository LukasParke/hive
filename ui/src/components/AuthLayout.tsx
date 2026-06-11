import type { ReactNode } from "react";
import { LogoWithText } from "./Logo";

interface AuthLayoutProps {
  children: ReactNode;
  eyebrow?: string;
  title?: string;
  subtitle?: string;
}

export function AuthLayout({ children, eyebrow, title, subtitle }: AuthLayoutProps) {
  return (
    <div className="auth-shell">
      <section className="auth-brand-panel" aria-label="Hive platform introduction">
        <LogoWithText size={38} />
        <div className="auth-brand-copy">
          <span className="eyebrow">Self-hosted deployment platform</span>
          <h1>Bring your apps home to a calmer control plane.</h1>
          <p>
            Hive gives independent operators a warm, swarm-native cockpit for apps, databases,
            domains, backups, and runtime health.
          </p>
        </div>
        <div className="auth-proof-grid" aria-label="Hive security and operations highlights">
          <div><strong>Direct install</strong><span>No vendor cloud required.</span></div>
          <div><strong>Runtime aware</strong><span>Live Swarm status and logs.</span></div>
          <div><strong>Secure by default</strong><span>Short-lived access sessions.</span></div>
        </div>
      </section>

      <section className="auth-card" aria-label={title ?? "Authentication"}>
        {(eyebrow || title || subtitle) && (
          <header className="auth-card-header">
            {eyebrow && <div className="eyebrow">{eyebrow}</div>}
            {title && <h2>{title}</h2>}
            {subtitle && <p>{subtitle}</p>}
          </header>
        )}
        {children}
      </section>
    </div>
  );
}
