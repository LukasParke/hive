import { lazy, Suspense } from "react";
import { Navigate, Route, Routes } from "react-router-dom";
import { AuthProvider, useAuth } from "./contexts/AuthContext";
import { AppProvider, useAppData } from "./contexts/AppContext";
import { ToastProvider } from "./contexts/ToastContext";
import { DashboardShell } from "./components/DashboardShell";
import { ErrorBoundary } from "./components/ErrorBoundary";
import { Summary } from "./components/Summary";
import { LoadingState } from "./components/LoadingState";

const LoginPage = lazy(() => import("./pages/auth/LoginPage").then((m) => ({ default: m.LoginPage })));
const RegisterPage = lazy(() => import("./pages/auth/RegisterPage").then((m) => ({ default: m.RegisterPage })));
const ResetPasswordPage = lazy(() => import("./pages/auth/ResetPasswordPage").then((m) => ({ default: m.ResetPasswordPage })));
const InvitationPage = lazy(() => import("./pages/auth/InvitationPage").then((m) => ({ default: m.InvitationPage })));
const OverviewPage = lazy(() => import("./pages/dashboard/OverviewPage").then((m) => ({ default: m.OverviewPage })));
const ProjectsPage = lazy(() => import("./pages/dashboard/ProjectsPage").then((m) => ({ default: m.ProjectsPage })));
const DeploymentsPage = lazy(() => import("./pages/dashboard/DeploymentsPage").then((m) => ({ default: m.DeploymentsPage })));
const StacksPage = lazy(() => import("./pages/dashboard/StacksPage").then((m) => ({ default: m.StacksPage })));
const ApplicationDetailPage = lazy(() => import("./pages/dashboard/ServiceDetailsPage").then((m) => ({ default: m.ApplicationDetailPage })));
const StackDetailPage = lazy(() => import("./pages/dashboard/ServiceDetailsPage").then((m) => ({ default: m.StackDetailPage })));
const DatabaseServiceDetailPage = lazy(() => import("./pages/dashboard/ServiceDetailsPage").then((m) => ({ default: m.DatabaseServiceDetailPage })));
const RuntimePage = lazy(() => import("./pages/dashboard/RuntimePage").then((m) => ({ default: m.RuntimePage })));
const MonitoringPage = lazy(() => import("./pages/dashboard/MonitoringPage").then((m) => ({ default: m.MonitoringPage })));
const NodeDetailPage = lazy(() => import("./pages/dashboard/NodeDetailPage").then((m) => ({ default: m.NodeDetailPage })));
const TerminalPage = lazy(() => import("./pages/dashboard/TerminalPage").then((m) => ({ default: m.TerminalPage })));
const LogViewerPage = lazy(() => import("./pages/dashboard/LogViewerPage").then((m) => ({ default: m.LogViewerPage })));
const SecretsPage = lazy(() => import("./pages/dashboard/SecretsPage").then((m) => ({ default: m.SecretsPage })));
const ConfigsPage = lazy(() => import("./pages/dashboard/ConfigsPage").then((m) => ({ default: m.ConfigsPage })));
const NetworksPage = lazy(() => import("./pages/dashboard/NetworksPage").then((m) => ({ default: m.NetworksPage })));
const DomainsPage = lazy(() => import("./pages/dashboard/DomainsPage").then((m) => ({ default: m.DomainsPage })));
const SecurityPage = lazy(() => import("./pages/dashboard/SecurityPage").then((m) => ({ default: m.SecurityPage })));
const DatabaseCreatePage = lazy(() => import("./pages/dashboard/DatabaseCreatePage").then((m) => ({ default: m.DatabaseCreatePage })));
const EnvironmentHierarchyPage = lazy(() => import("./pages/dashboard/EnvironmentHierarchyPage").then((m) => ({ default: m.EnvironmentHierarchyPage })));
const OrganizationAdminPage = lazy(() => import("./pages/dashboard/OrganizationAdminPage").then((m) => ({ default: m.OrganizationAdminPage })));
const SettingsInfraPage = lazy(() => import("./pages/dashboard/SettingsInfraPage").then((m) => ({ default: m.SettingsInfraPage })));
const SettingsPage = lazy(() => import("./pages/dashboard/SettingsPage").then((m) => ({ default: m.SettingsPage })));
const PreviewsPage = lazy(() => import("./pages/dashboard/PreviewsPage").then((m) => ({ default: m.PreviewsPage })));
const ProfilePage = lazy(() => import("./pages/dashboard/ProfilePage").then((m) => ({ default: m.ProfilePage })));
const NotificationsPage = lazy(() => import("./pages/dashboard/settings/NotificationsPage").then((m) => ({ default: m.NotificationsPage })));
const RegistriesPage = lazy(() => import("./pages/dashboard/settings/RegistriesPage").then((m) => ({ default: m.RegistriesPage })));
const BackupDestinationsPage = lazy(() => import("./pages/dashboard/settings/BackupDestinationsPage").then((m) => ({ default: m.BackupDestinationsPage })));
const SchedulesPage = lazy(() => import("./pages/dashboard/settings/SchedulesPage").then((m) => ({ default: m.SchedulesPage })));
const GitProvidersPage = lazy(() => import("./pages/dashboard/settings/GitProvidersPage").then((m) => ({ default: m.GitProvidersPage })));
const GlobalSettingsPage = lazy(() => import("./pages/dashboard/settings/GlobalSettingsPage").then((m) => ({ default: m.GlobalSettingsPage })));

function PageFallback() {
  return (
    <div className="card" style={{ margin: 16 }}>
      <LoadingState />
    </div>
  );
}

function AuthRoutes() {
  return (
    <Suspense fallback={<PageFallback />}>
      <Routes>
        <Route path="/" element={<LoginPage />} />
        <Route path="/register" element={<RegisterPage />} />
        <Route path="/reset-password" element={<ResetPasswordPage />} />
        <Route path="/invitation" element={<InvitationPage />} />
        <Route path="/accept-invitation/:id" element={<InvitationPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </Suspense>
  );
}

function EventsPage() {
  const { events } = useAppData();
  return <Summary title="Realtime Events" items={events.map((value) => ({ value }))} />;
}

function DashboardRoutes() {
  return (
    <DashboardShell>
      <Suspense fallback={<PageFallback />}>
        <Routes>
          <Route path="/" element={<Navigate to="/dashboard/overview" replace />} />
          <Route path="/dashboard/overview" element={<OverviewPage />} />
          <Route path="/dashboard/projects" element={<ProjectsPage />} />
          <Route path="/dashboard/deployments" element={<DeploymentsPage />} />
          <Route path="/dashboard/stacks" element={<StacksPage />} />
          <Route path="/dashboard/services/application/:id" element={<ApplicationDetailPage />} />
          <Route path="/dashboard/services/application/:id/previews" element={<PreviewsPage />} />
          <Route path="/dashboard/services/stack/:id" element={<StackDetailPage />} />
          <Route path="/dashboard/services/database/:id" element={<DatabaseServiceDetailPage />} />
          <Route path="/dashboard/database/create" element={<DatabaseCreatePage />} />
          <Route path="/dashboard/runtime" element={<RuntimePage />} />
          <Route path="/dashboard/monitoring" element={<MonitoringPage />} />
          <Route path="/dashboard/nodes/:id" element={<NodeDetailPage />} />
          <Route path="/dashboard/terminal/:containerID" element={<TerminalPage />} />
          <Route path="/dashboard/logs/:containerID" element={<LogViewerPage />} />
          <Route path="/dashboard/secrets" element={<SecretsPage />} />
          <Route path="/dashboard/configs" element={<ConfigsPage />} />
          <Route path="/dashboard/networks" element={<NetworksPage />} />
          <Route path="/dashboard/domains" element={<DomainsPage />} />
          <Route path="/dashboard/security" element={<SecurityPage />} />
          <Route path="/dashboard/environments" element={<EnvironmentHierarchyPage />} />
          <Route path="/dashboard/settings" element={<SettingsPage />} />
          <Route path="/dashboard/settings/notifications" element={<NotificationsPage />} />
          <Route path="/dashboard/settings/registries" element={<RegistriesPage />} />
          <Route path="/dashboard/settings/backups" element={<BackupDestinationsPage />} />
          <Route path="/dashboard/settings/schedules" element={<SchedulesPage />} />
          <Route path="/dashboard/settings/git-providers" element={<GitProvidersPage />} />
          <Route path="/dashboard/settings/global" element={<GlobalSettingsPage />} />
          <Route path="/dashboard/settings/users" element={<OrganizationAdminPage />} />
          <Route path="/dashboard/settings/infra" element={<SettingsInfraPage />} />
          <Route path="/dashboard/profile" element={<ProfilePage />} />
          <Route path="/dashboard/events" element={<EventsPage />} />
          {/* Legacy redirects */}
          <Route path="/overview" element={<Navigate to="/dashboard/overview" replace />} />
          <Route path="/projects" element={<Navigate to="/dashboard/projects" replace />} />
          <Route path="/settings" element={<Navigate to="/dashboard/settings" replace />} />
          <Route path="*" element={<Navigate to="/dashboard/overview" replace />} />
        </Routes>
      </Suspense>
    </DashboardShell>
  );
}

function AppInner() {
  const { isAuthed, authLoading } = useAuth();

  if (authLoading) {
    return (
      <main style={{ minHeight: "100vh", width: "100%", display: "flex", alignItems: "center", justifyContent: "center" }}>
        <div className="card">Loading Hive…</div>
      </main>
    );
  }

  if (!isAuthed) {
    return (
      <main style={{ minHeight: "100vh", width: "100%", display: "flex", alignItems: "stretch", justifyContent: "center" }}>
        <AuthRoutes />
      </main>
    );
  }

  return (
    <AppProvider>
      <DashboardRoutes />
    </AppProvider>
  );
}

export function App() {
  return (
    <AuthProvider>
      <ToastProvider>
        <ErrorBoundary>
          <AppInner />
        </ErrorBoundary>
      </ToastProvider>
    </AuthProvider>
  );
}
