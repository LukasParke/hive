import { useState } from "react";
import { Link } from "react-router-dom";
import { useAuth } from "../../contexts/AuthContext";
import { useToast } from "../../contexts/ToastContext";
import { AuthLayout } from "../../components/AuthLayout";

export function LoginPage() {
  const { login, register } = useAuth();
  const toast = useToast();

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
    <AuthLayout>
      <h2 style={{ margin: "0 0 4px", fontSize: 20 }}>
        {isRegistering ? "Create Account" : "Sign in to Hive"}
      </h2>
      <p style={{ color: "var(--text-faint)", fontSize: 13, marginBottom: 20 }}>
        {isRegistering ? "Set up your admin account" : "Enter your credentials to continue"}
      </p>

      <div className="form-stack">
        {isRegistering && (
          <div className="form-group">
            <label>Display name</label>
            <input
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
              placeholder="Admin"
            />
          </div>
        )}
        <div className="form-group">
          <label>Email</label>
          <input
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder="admin@example.com"
            type="email"
          />
        </div>
        <div className="form-group">
          <label>Password</label>
          <input
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder="••••••••"
            type="password"
          />
        </div>

        {error && (
          <div
            style={{
              background: "var(--error-bg)",
              color: "var(--error-fg)",
              padding: "10px 14px",
              borderRadius: "var(--radius-sm)",
              fontSize: 13,
            }}
          >
            {error}
          </div>
        )}

        <div style={{ display: "flex", gap: 10, marginTop: 4 }}>
          {isRegistering ? (
            <button onClick={handleRegister} disabled={loading} className="btn-primary" style={{ flex: 1 }}>
              {loading ? "Creating…" : "Create Account"}
            </button>
          ) : (
            <button onClick={handleLogin} disabled={loading} className="btn-primary" style={{ flex: 1 }}>
              {loading ? "Signing in…" : "Sign In"}
            </button>
          )}
        </div>

        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            alignItems: "center",
            fontSize: 13,
          }}
        >
          <button
            onClick={() => { setIsRegistering((v) => !v); setError(null); }}
            className="btn-ghost"
            style={{ padding: 0, fontSize: 13 }}
          >
            {isRegistering ? "Back to sign in" : "Create account"}
          </button>
          {!isRegistering && (
            <Link to="/reset-password" style={{ color: "var(--text-faint)", fontSize: 13 }}>
              Forgot password?
            </Link>
          )}
        </div>
      </div>
    </AuthLayout>
  );
}
