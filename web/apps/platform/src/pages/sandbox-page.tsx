import { useTranslation } from "react-i18next";
import { PageShell, Tabs, TabsContent, TabsList, TabsTrigger } from "@argus/ui";
import { BackendsTab } from "../components/sandbox/backends-tab";
import { ImagesTab } from "../components/sandbox/images-tab";
import { ProfilesTab } from "../components/sandbox/profiles-tab";
import { QuotasTab } from "../components/sandbox/quotas-tab";
import { SessionsTab } from "../components/sandbox/sessions-tab";
import { UsageTab } from "../components/sandbox/usage-tab";

/** OpenSandbox 基座（docs/08）：服务连接 / 镜像 / Profile / 配额 / 会话 / 用量。 */
export function SandboxPage() {
  const { t } = useTranslation();
  return (
    <PageShell
      description={t("sandbox.description")}
      title={t("sandbox.title")}
    >
      <Tabs defaultValue="backends">
        <TabsList>
          <TabsTrigger value="backends">
            {t("sandbox.tabs.backends")}
          </TabsTrigger>
          <TabsTrigger value="images">{t("sandbox.tabs.images")}</TabsTrigger>
          <TabsTrigger value="profiles">
            {t("sandbox.tabs.profiles")}
          </TabsTrigger>
          <TabsTrigger value="quotas">{t("sandbox.tabs.quotas")}</TabsTrigger>
          <TabsTrigger value="sessions">
            {t("sandbox.tabs.sessions")}
          </TabsTrigger>
          <TabsTrigger value="usage">{t("sandbox.tabs.usage")}</TabsTrigger>
        </TabsList>
        <TabsContent value="backends">
          <BackendsTab />
        </TabsContent>
        <TabsContent value="images">
          <ImagesTab />
        </TabsContent>
        <TabsContent value="profiles">
          <ProfilesTab />
        </TabsContent>
        <TabsContent value="quotas">
          <QuotasTab />
        </TabsContent>
        <TabsContent value="sessions">
          <SessionsTab />
        </TabsContent>
        <TabsContent value="usage">
          <UsageTab />
        </TabsContent>
      </Tabs>
    </PageShell>
  );
}
