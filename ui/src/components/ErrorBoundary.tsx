import { Component, type ErrorInfo, type ReactNode } from "react";

interface ErrorBoundaryProps {
  children: ReactNode;
}

interface ErrorBoundaryState {
  error: Error | null;
}

export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  state: ErrorBoundaryState = { error: null };

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("Hive UI render failure", error, info);
  }

  render() {
    if (!this.state.error) return this.props.children;

    return (
      <main className="app-error-shell">
        <section className="card app-error-card" role="alert">
          <div className="empty-orb">!</div>
          <h1>Hive hit a UI snag</h1>
          <p>
            The app recovered into a safe screen instead of staying blank. Refresh to load the
            latest UI bundle; if it repeats, copy the details below.
          </p>
          <pre>{this.state.error.message}</pre>
          <div className="hero-actions">
            <button className="btn-primary" onClick={() => window.location.reload()}>Refresh app</button>
            <button className="btn-ghost" onClick={() => this.setState({ error: null })}>Try again</button>
          </div>
        </section>
      </main>
    );
  }
}
