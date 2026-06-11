import { FormEvent, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useAuth } from "../../contexts/AuthContext";
import { AuthLayout } from "../../components/AuthLayout";

function normalizeEmail(value: string) {
  return value.trim().toLowerCase();
}

export function LoginPage() {
  const { login } = useAuth();

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const canSubmit = useMemo(() => normalizeEmail(email).includes("@") && password.length > 0 && !loading, [email, password, loading]);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canSubmit) return;
    setLoading(true);
    setError(null);
    try {
      await login(normalizeEmail(email), password);
    } catch (err) {
      setError("We could not sign you in. Check your email and password, then try again.");
      console.warn("Login failed", err);
    } finally {
      setLoading(false);
    }
  }

  return (
    <AuthLayout eyebrow="Welcome back" title="Sign in to Hive" subtitle="Manage your self-hosted deployment platform from a secure operator session.">
      <form className="auth-form" onSubmit={handleSubmit} noValidate>
        <div className="form-group">
          <div className="field-label-row">
            <label htmlFor="login-email">Email</label>
          </div>
          <input
            id="login-email"
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            onBlur={() => setEmail((value) => normalizeEmail(value))}
            placeholder="you@example.com"
            type="email"
            autoComplete="email"
            autoCapitalize="none"
            spellCheck={false}
            autoFocus
            required
          />
        </div>

        <div className="form-group">
          <div className="field-label-row">
            <label htmlFor="login-password">Password</label>
            <Link to="/reset-password">Forgot password?</Link>
          </div>
          <div className="password-field">
            <input
              id="login-password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              placeholder="Enter your password"
              type={showPassword ? "text" : "password"}
              autoComplete="current-password"
              required
            />
            <button type="button" className="btn-ghost password-toggle" onClick={() => setShowPassword((value) => !value)} aria-label={showPassword ? "Hide password" : "Show password"}>
              {showPassword ? "Hide" : "Show"}
            </button>
          </div>
        </div>

        {error && (
          <div className="auth-alert" role="alert">
            {error}
          </div>
        )}

        <button type="submit" disabled={!canSubmit} className="btn-primary auth-submit">
          {loading ? "Signing in…" : "Sign in"}
        </button>

        <p className="auth-switch-copy">
          New to this Hive? <Link to="/register">Create the first operator account</Link>
        </p>
      </form>
    </AuthLayout>
  );
}
