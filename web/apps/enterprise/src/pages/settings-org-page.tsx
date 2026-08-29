import { useState } from "react";
import { useTranslation } from "react-i18next";
import { PageShell, Tabs, TabsContent, TabsList, TabsTrigger } from "@argus/ui";
import "../styles/settings.css";
import { OrgAccessTab } from "../components/settings/org-access-tab";
import { OrgDepartmentsTab } from "../components/settings/org-departments-tab";
import { OrgPoliciesTab } from "../components/settings/org-policies-tab";
import { OrgRolesTab } from "../components/settings/org-roles-tab";
import { OrgUsersTab } from "../components/settings/org-users-tab";
import { OrgRemoteAccessTab } from "../components/settings/org-remote-access-tab";
import { OrgTelemetryTab } from "../components/settings/org-telemetry-tab";
import { usePermission } from "../lib/permissions";

const TAB_KEYS = [
  "users",
  "departments",
  "roles",
  "policies",
  "access",
  "remote_access",
  "telemetry",
] as const;

/** 组织设置：用户、部门、角色、审批策略和资源接入。 */
export function SettingsOrgPage() {
  const { t } = useTranslation();
  const canReadRemoteAccess = usePermission("remote_access.grant.read");
  const [tab, setTab] = useState<(typeof TAB_KEYS)[number]>("users");
  const tabs = canReadRemoteAccess
    ? TAB_KEYS
    : TAB_KEYS.filter((key) => key !== "remote_access");

  return (
    <PageShell
      description={t("settings.org.description")}
      title={t("settings.org.title")}
    >
      <Tabs onValueChange={(value) => setTab(value as typeof tab)} value={tab}>
        <TabsList>
          {tabs.map((key) => (
            <TabsTrigger key={key} value={key}>
              {t(`settings.org.tabs.${key}`)}
            </TabsTrigger>
          ))}
        </TabsList>
        <TabsContent value="users">
          <OrgUsersTab />
        </TabsContent>
        <TabsContent value="departments">
          <OrgDepartmentsTab />
        </TabsContent>
        <TabsContent value="roles">
          <OrgRolesTab />
        </TabsContent>
        <TabsContent value="policies">
          <OrgPoliciesTab />
        </TabsContent>
        <TabsContent value="access">
          <OrgAccessTab />
        </TabsContent>
        {canReadRemoteAccess && (
          <TabsContent value="remote_access">
            <OrgRemoteAccessTab />
          </TabsContent>
        )}
        <TabsContent value="telemetry">
          <OrgTelemetryTab />
        </TabsContent>
      </Tabs>
    </PageShell>
  );
}
