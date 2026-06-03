import { useEffect, useMemo, useState } from "react";
import { useLocation, useParams } from "react-router-dom";
import { api, type Session } from "../../api/client";

function useTokenFromRoute() {
  const location = useLocation();
  const params = useParams();
  return useMemo(() => {
    const queryToken = new URLSearchParams(location.search).get("token");
    if (queryToken && queryToken.trim() !== "") return queryToken.trim();
    const routeToken = params.id ?? params["accept-invitation"];
    return (routeToken ?? "").trim();
  }, [location.search, params]);
}

export function InvitationPage() {
  const token = useTokenFromRoute();
  const [invitation, setInvitation] = useState<Record<string, unknown> | null>(null);
  const [message, setMessage] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");

  useEffect(() => {
    if (!token) return;
    api
      .getInvitationByToken(token)
      .then((res) => {
        setInvitation(res);
        if (typeof res.email === "string") setEmail(res.email);
      })
      .catch((err) => setMessage((err as Error).message));
  }, [token]);

  return (
    <section style={{ width: 520, border: "1px solid var(--border-primary)", borderRadius: "var(--radius-lg)", padding: 24, background: "var(--bg-secondary)", boxShadow: "var(--shadow-lg)" }}>
      <h1 style={{ marginTop: 0 }}>Invitation</h1>
      {!token ? <p style={{ color: "var(--error-fg)" }}>Missing invitation token.</p> : <p style={{ color: "var(--text-secondary)" }}>Review invitation and accept.</p>}
      {invitation && <pre style={{ background: "var(--code-bg)", borderRadius: 6, padding: 10, color: "var(--text-primary)", border: "1px solid var(--border-primary)" }}>{JSON.stringify(invitation, null, 2)}</pre>}
      <div style={{ display: "grid", gap: 8 }}>
        <input value={email} onChange={(e) => setEmail(e.target.value)} placeholder="Email" />
        <input value={password} onChange={(e) => setPassword(e.target.value)} placeholder="Password" type="password" />
        <button
          onClick={async () => {
            const registered = await api.register({ email, password, displayName: email.split("@")[0] || "User" });
            const session = await api.login({ email, password });
            await api.acceptInvitationByToken(session as Session, token);
            setMessage(`Invitation accepted for user ${registered.id}.`);
          }}
          disabled={!token}
          style={{ padding: "8px 20px", background: "var(--gold-500)", color: "var(--text-on-gold)", border: "none", borderRadius: 4, fontWeight: 600 }}
        >
          Register and accept invitation
        </button>
      </div>
      {message && <p style={{ color: "var(--text-secondary)", marginTop: 8 }}>{message}</p>}
    </section>
  );
}
