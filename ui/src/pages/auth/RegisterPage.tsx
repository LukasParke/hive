import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useAuth } from "../../contexts/AuthContext";
import { useToast } from "../../contexts/ToastContext";

export function RegisterPage() {
  const { register } = useAuth();
  const toast = useToast();
  const navigate = useNavigate();

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  async function handleRegister() {
    setLoading(true);
    setError(null);
    try {
      await register(email, password, displayName);
      toast.success("Account created. You can now log in.");
      navigate("/");
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  }

  return (
    <section style={{ width: 460, border: "1px solid var(--border-primary)", borderRadius: "var(--radius-lg)", padding: 24, background: "var(--bg-secondary)", boxShadow: "var(--shadow-lg)" }}>
      <h1 style={{ marginTop: 0 }}>Create Account</h1>
      <div style={{ display: "grid", gap: 10 }}>
        <input value={displayName} onChange={(e) => setDisplayName(e.target.value)} placeholder="Display name" />
        <input value={email} onChange={(e) => setEmail(e.target.value)} placeholder="Email" />
        <input value={password} onChange={(e) => setPassword(e.target.value)} placeholder="Password" type="password" />
        <div style={{ display: "flex", gap: 8 }}>
          <button onClick={handleRegister} disabled={loading} style={{ padding: "8px 20px", background: "var(--gold-500)", color: "var(--text-on-gold)", border: "none", borderRadius: 4, fontWeight: 600 }}>
            {loading ? "Creating..." : "Register"}
          </button>
          <Link to="/" style={{ padding: "8px 16px", color: "var(--text-secondary)", textDecoration: "none" }}>Back to login</Link>
        </div>
      </div>
      {error && <p style={{ color: "var(--error-fg)", marginTop: 12 }}>Error: {error}</p>}
    </section>
  );
}
