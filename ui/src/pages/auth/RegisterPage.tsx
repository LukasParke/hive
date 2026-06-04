import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useAuth } from "../../contexts/AuthContext";
import { useToast } from "../../contexts/ToastContext";
import { AuthLayout } from "../../components/AuthLayout";

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
    <AuthLayout>
      <h2 style={{ margin: "0 0 4px", fontSize: 20 }}>Create Account</h2>
      <p style={{ color: "var(--text-faint)", fontSize: 13, marginBottom: 20 }}>
        Set up your Hive admin account
      </p>

      <div className="form-stack">
        <div className="form-group">
          <label>Display name</label>
          <input
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            placeholder="Admin"
          />
        </div>
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

        <button onClick={handleRegister} disabled={loading} className="btn-primary">
          {loading ? "Creating…" : "Create Account"}
        </button>

        <div style={{ textAlign: "center", fontSize: 13 }}>
          <Link to="/" style={{ color: "var(--text-faint)" }}>
            Back to sign in
          </Link>
        </div>
      </div>
    </AuthLayout>
  );
}
