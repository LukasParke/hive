import { useState } from "react";
import { api } from "../../api/client";

export function ResetPasswordPage() {
  const [email, setEmail] = useState("");
  const [token, setToken] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [message, setMessage] = useState<string>("");

  return (
    <section style={{ width: 520, border: "1px solid var(--border-primary)", borderRadius: "var(--radius-lg)", padding: 24, background: "var(--bg-secondary)", boxShadow: "var(--shadow-lg)" }}>
      <h1 style={{ marginTop: 0 }}>Reset Password</h1>
      <p style={{ color: "var(--text-secondary)" }}>Request a reset token, then set a new password.</p>
      <div style={{ display: "grid", gap: 8 }}>
        <input value={email} onChange={(e) => setEmail(e.target.value)} placeholder="Email" />
        <button
          onClick={async () => {
            const res = await api.sendResetPassword({ email });
            const issuedToken = String(res.token ?? "");
            if (issuedToken) {
              setToken(issuedToken);
              setMessage("Reset token issued (dev mode).");
              return;
            }
            setMessage("If your email exists, a reset token has been generated.");
          }}
        >
          Send reset token
        </button>
        <input value={token} onChange={(e) => setToken(e.target.value)} placeholder="Reset token" />
        <input value={newPassword} onChange={(e) => setNewPassword(e.target.value)} placeholder="New password" type="password" />
        <button
          onClick={async () => {
            await api.resetPassword({ token, newPassword });
            setMessage("Password updated. You can now log in.");
          }}
        >
          Reset password
        </button>
      </div>
      {message && <p style={{ color: "var(--text-secondary)", marginTop: 8 }}>{message}</p>}
    </section>
  );
}
