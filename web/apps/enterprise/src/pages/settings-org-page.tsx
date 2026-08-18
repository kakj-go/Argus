import { useState } from "react";
import { useTranslation } from "react-i18next";
import { PageShell, Tabs, TabsContent, TabsList, TabsTrigger } from "@argus/ui";
import "../i18n/settings";
import "../styles/settings.css";
import { OrgAccessTab } from "../components/settings/org-access-tab";
import { OrgBindingsTab } from "../components/settings/org-bindings-tab";
import { OrgDepartmentsTab } from "../components/settings/org-departments-tab";
import { OrgPoliciesTab } from "../components/settings/org-policies-tab";
import { OrgRolesTab } from "../components/settings/org-roles-tab";
import { OrgScopesTab } from "../components/settings/org-scopes-tab";
import { OrgUsersTab } from "../components/settings/org-users-tab";
import { OrgRemoteAccessTab } from "../components/settings/org-remote-access-tab";

const TAB_KEYS = [
  "users",
  "departments",
  "roles",
  "bindings",
  "scopes",
  "policies",
  "access",
  "remote_access",
] as const;

/** 组织与权限：用户、部门、项目、角色、授权绑定、数据权限、审批策略、ServiceAccount/APIKey。 */
export function SettingsOrgPage() {
  const { t } = useTranslation();
  const [tab, setTab] = useState<(typeof TAB_KEYS)[number]>("users");

  return (
    <PageShell
      description={t("settings.org.description")}
      title={t("settings.org.title")}
    >
      <Tabs onValueChange={(value) => setTab(value as typeof tab)} value={tab}>
        <TabsList>
          {TAB_KEYS.map((key) => (
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
        <TabsContent value="bindings">
          <OrgBindingsTab />
        </TabsContent>
        <TabsContent value="scopes">
          <OrgScopesTab />
        </TabsContent>
        <TabsContent value="policies">
          <OrgPoliciesTab />
        </TabsContent>
        <TabsContent value="access">
          <OrgAccessTab />
        </TabsContent>
        <TabsContent value="remote_access">
          <OrgRemoteAccessTab />
        </TabsContent>
      </Tabs>
    </PageShell>
  );
}
