import { Navigate, Route, Routes } from "react-router-dom";
import { AuthProvider, useAuth } from "./contexts/AuthContext";
import { AppProvider, useAppData } from "./contexts/AppContext";
import { ToastProvider } from "./contexts/ToastContext";
import { DashboardShell } from "./components/DashboardShell";
import { Summary } from "./components/Summary";

// Auth pages
import { LoginPage } from "./pages/auth/LoginPage";
import { RegisterPage } from "./pages/auth/RegisterPage";
import { ResetPasswordPage } from "./pages/auth/ResetPasswordPage";
import { InvitationPage } from "./pages/auth/InvitationPage";

// Dashboard pages
import { OverviewPage } from "./pages/dashboard/OverviewPage";
import { ProjectsPage } from "./pages/dashboard/ProjectsPage";
import { DeploymentsPage } from "./pages/dashboard/DeploymentsPage";
import { StacksPage } from "./pages/dashboard/StacksPage";
import { ApplicationDetailPage, StackDetailPage, DatabaseServiceDetailPage } from "./pages/dashboard/ServiceDetailsPage";
import { RuntimePage } from "./pages/dashboard/RuntimePage";
import { MonitoringPage } from "./pages/dashboard/MonitoringPage";
import { NodeDetailPage } from "./pages/dashboard/NodeDetailPage";
import { TerminalPage } from "./pages/dashboard/TerminalPage";
import { LogViewerPage } from "./pages/dashboard/LogViewerPage";
import { SecretsPage } from "./pages/dashboard/SecretsPage";
import { ConfigsPage } from "./pages/dashboard/ConfigsPage";
import { NetworksPage } from "./pages/dashboard/NetworksPage";
import { DomainsPage } from "./pages/dashboard/DomainsPage";
import { SecurityPage } from "./pages/dashboard/SecurityPage";
import { DatabaseCreatePage } from "./pages/dashboard/DatabaseCreatePage";
import { EnvironmentHierarchyPage } from "./pages/dashboard/EnvironmentHierarchyPage";
import { OrganizationAdminPage } from "./pages/dashboard/OrganizationAdminPage";
import { SettingsInfraPage } from "./pages/dashboard/SettingsInfraPage";
import { SettingsPage } from "./pages/dashboard/SettingsPage";
import { PreviewsPage } from "./pages/dashboard/PreviewsPage";
import { ProfilePage } from "./pages/dashboard/ProfilePage";

// Settings sub-pages
import { NotificationsPage } from "./pages/dashboard/settings/NotificationsPage";
import { RegistriesPage } from "./pages/dashboard/settings/RegistriesPage";
import { BackupDestinationsPage } from "./pages/dashboard/settings/BackupDestinationsPage";
import { SchedulesPage } from "./pages/dashboard/settings/SchedulesPage";
import { GitProvidersPage } from "./pages/dashboard/settings/GitProvidersPage";
import { GlobalSettingsPage } from "./pages/dashboard/settings/GlobalSettingsPage";

function AuthRoutes() {
  return (
    <Routes>
      <Route path="/" element={<LoginPage />} />
      <Route path="/register" element={<RegisterPage />} />
      <Route path="/reset-password" element={<ResetPasswordPage />} />
      <Route path="/invitation" element={<InvitationPage />} />
      <Route path="/accept-invitation/:id" element={<InvitationPage />} />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}

function EventsPage() {
  const { events } = useAppData();
  return <Summary title="Realtime Events" items={events.map((value) => ({ value }))} />;
}

function DashboardRoutes() {
  return (
    <DashboardShell>
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
    </DashboardShell>
  );
}

function AppInner() {
  const { isAuthed } = useAuth();

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
        <AppInner />
      </ToastProvider>
    </AuthProvider>
  );
}
