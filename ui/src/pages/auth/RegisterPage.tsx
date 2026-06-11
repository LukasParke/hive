import { FormEvent, useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useAuth } from "../../contexts/AuthContext";
import { useToast } from "../../contexts/ToastContext";
import { AuthLayout } from "../../components/AuthLayout";

function normalizeEmail(value: string) {
  return value.trim().toLowerCase();
}

function passwordChecks(password: string) {
  return [
    { label: "12+ characters", passed: password.length >= 12 },
    { label: "Upper and lower case", passed: /[a-z]/.test(password) && /[A-Z]/.test(password) },
    { label: "Number or symbol", passed: /[0-9\W_]/.test(password) },
  ];
}

export function RegisterPage() {
  const { register, login } = useAuth();
  const toast = useToast();
  const navigate = useNavigate();

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const checks = useMemo(() => passwordChecks(password), [password]);
  const passedChecks = checks.filter((check) => check.passed).length;
  const emailValid = /.+@.+\..+/.test(normalizeEmail(email));
  const passwordsMatch = password.length > 0 && password === confirmPassword;
  const canSubmit = emailValid && displayName.trim().length > 0 && passedChecks === checks.length && passwordsMatch && !loading;

  async function handleRegister(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canSubmit) return;
    setLoading(true);
    setError(null);
    const normalizedEmail = normalizeEmail(email);
    try {
      await register(normalizedEmail, password, displayName.trim());
      await login(normalizedEmail, password);
      toast.success("Welcome to Hive. Your operator account is ready.");
      navigate("/dashboard/overview", { replace: true });
    } catch (err) {
      const message = (err as Error).message || "Could not create your account.";
      setError(message.includes("duplicate") || message.includes("unique") ? "An account already exists for this email. Sign in instead." : message);
    } finally {
      setLoading(false);
    }
  }

  return (
    <AuthLayout eyebrow="First run" title="Create your operator account" subtitle="Use a real email and a strong password. Hive will keep you signed in and guide you through the launch checklist.">
      <form className="auth-form" onSubmit={handleRegister} noValidate>
        <div className="form-group">
          <label htmlFor="register-name">Display name</label>
          <input
            id="register-name"
            value={displayName}
            onChange={(event) => setDisplayName(event.target.value)}
            placeholder="Ada Lovelace"
            autoComplete="name"
            required
          />
        </div>

        <div className="form-group">
          <label htmlFor="register-email">Email</label>
          <input
            id="register-email"
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            onBlur={() => setEmail((value) => normalizeEmail(value))}
            placeholder="you@example.com"
            type="email"
            autoComplete="email"
            autoCapitalize="none"
            spellCheck={false}
            required
          />
        </div>

        <div className="form-group">
          <div className="field-label-row">
            <label htmlFor="register-password">Password</label>
            <button type="button" className="btn-ghost inline-text-button" onClick={() => setShowPassword((value) => !value)}>
              {showPassword ? "Hide" : "Show"}
            </button>
          </div>
          <input
            id="register-password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            placeholder="Create a strong password"
            type={showPassword ? "text" : "password"}
            autoComplete="new-password"
            required
          />
          <div className="password-strength" aria-label={`Password strength ${passedChecks} of ${checks.length}`}>
            <span style={{ width: `${(passedChecks / checks.length) * 100}%` }} />
          </div>
          <div className="password-checks">
            {checks.map((check) => (
              <span key={check.label} className={check.passed ? "is-passed" : ""}>
                {check.passed ? "✓" : "○"} {check.label}
              </span>
            ))}
          </div>
        </div>

        <div className="form-group">
          <label htmlFor="register-confirm-password">Confirm password</label>
          <input
            id="register-confirm-password"
            value={confirmPassword}
            onChange={(event) => setConfirmPassword(event.target.value)}
            placeholder="Repeat your password"
            type={showPassword ? "text" : "password"}
            autoComplete="new-password"
            required
          />
          {confirmPassword && !passwordsMatch && <span className="field-hint is-error">Passwords do not match.</span>}
        </div>

        {error && (
          <div className="auth-alert" role="alert">
            {error}
          </div>
        )}

        <button type="submit" disabled={!canSubmit} className="btn-primary auth-submit">
          {loading ? "Creating account…" : "Create account and continue"}
        </button>

        <p className="auth-switch-copy">
          Already have access? <Link to="/">Sign in</Link>
        </p>
      </form>
    </AuthLayout>
  );
}
