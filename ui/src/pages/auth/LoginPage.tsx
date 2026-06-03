import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useAuth } from "../../contexts/AuthContext";
import { useToast } from "../../contexts/ToastContext";

export function LoginPage() {
  const { login, register } = useAuth();
  const toast = useToast();
  const navigate = useNavigate();

  const [email, setEmail] = useState("admin@example.com");
  const [password, setPassword] = useState("password123");
  const [displayName, setDisplayName] = useState("Admin");
  const [isRegistering, setIsRegistering] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  async function handleLogin() {
    setLoading(true);
    setError(null);
    try {
      await login(email, password);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  }

  async function handleRegister() {
    setLoading(true);
    setError(null);
    try {
      await register(email, password, displayName);
      toast.success("Account created. You can now log in.");
      setIsRegistering(false);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  }

  return (
    <section style={{ width: 460, border: "1px solid var(--border-primary)", borderRadius: "var(--radius-lg)", padding: 24, background: "var(--bg-secondary)", boxShadow: "var(--shadow-lg)" }}>
      <h1 style={{ marginTop: 0 }}>{isRegistering ? "Create Account" : "Sign in to Hive"}</h1>
      <div style={{ display: "grid", gap: 10 }}>
        {isRegistering && <input value={displayName} onChange={(e) => setDisplayName(e.target.value)} placeholder="Display name" />}
        <input value={email} onChange={(e) => setEmail(e.target.value)} placeholder="Email" />
        <input value={password} onChange={(e) => setPassword(e.target.value)} placeholder="Password" type="password" />
        <div style={{ display: "flex", gap: 8 }}>
          {isRegistering ? (
            <button onClick={handleRegister} disabled={loading} style={{ padding: "8px 20px", background: "var(--gold-500)", color: "var(--text-on-gold)", border: "none", borderRadius: 4, fontWeight: 600 }}>
              {loading ? "Creating..." : "Register"}
            </button>
          ) : (
            <button onClick={handleLogin} disabled={loading} style={{ padding: "8px 20px", background: "var(--gold-500)", color: "var(--text-on-gold)", border: "none", borderRadius: 4, fontWeight: 600 }}>
              {loading ? "Signing in..." : "Login"}
            </button>
          )}
          <button onClick={() => setIsRegistering((v) => !v)} style={{ padding: "8px 16px" }}>
            {isRegistering ? "Back to login" : "Create account"}
          </button>
        </div>
        <Link to="/reset-password" style={{ fontSize: 13, color: "var(--text-secondary)" }}>Forgot password?</Link>
      </div>
      {error && <p style={{ color: "var(--error-fg)", marginTop: 12 }}>Error: {error}</p>}
    </section>
  );
}
