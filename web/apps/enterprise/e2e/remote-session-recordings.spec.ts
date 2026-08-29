import { expect, test, type Page } from "@playwright/test";

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    if (!window.localStorage.getItem("argus.locale")) {
      window.localStorage.setItem("argus.locale", "zh-CN");
    }
    if (!window.localStorage.getItem("argus.theme")) {
      window.localStorage.setItem("argus.theme", "dark");
    }
  });
});

async function login(page: Page, username = "root") {
  await page.goto("/login");
  await page.getByLabel("用户名").fill(username);
  await page.getByLabel("密码").fill("123456");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page).not.toHaveURL(/\/login/);
}

test("recordings: wide detail dialog replays the session and exposes raw events", async ({ page }) => {
  await login(page);
  await page.goto("/remote-sessions");
  await page.getByRole("tab", { name: "会话录像" }).click();

  // mock 种子录像（available）出现在列表中。
  const row = page.getByRole("row", { name: /ras-seed-1/ });
  await expect(row).toBeVisible();
  await row.getByRole("button", { name: "查看录像" }).click();

  // 详情弹框为宽版（--lg），不再被 520px 默认宽度裁剪。
  const dialog = page.locator(".argus-dialog--lg");
  await expect(dialog).toBeVisible();
  const box = await dialog.boundingBox();
  expect(box).not.toBeNull();
  expect(box!.width).toBeGreaterThan(700);

  // 事件自动翻页拉全后进入回放视图：终端画面 + 视频式控制条。
  const player = dialog.locator(".argus-terminal-player");
  await expect(player).toBeVisible({ timeout: 15_000 });
  await expect(player.getByRole("slider", { name: "录像进度" })).toBeVisible();
  await expect(player.getByText(/^00:00 \/ 00:10$/)).toBeVisible();
  await expect(player.getByRole("button", { name: "4x" })).toBeVisible();

  // 8 倍速播放 10.4s 录像约 1.3s 到达终点并自动停止。
  await player.getByRole("button", { name: "8x" }).click();
  await player.getByRole("button", { name: "播放", exact: true }).click();
  await expect(player.getByText("回放结束")).toBeVisible({ timeout: 10_000 });
  await expect(player.getByText(/^00:10 \/ 00:10$/)).toBeVisible();

  // 从头重放可用；拖动进度条 seek 到中间位置。
  await player.getByRole("button", { name: "从头播放" }).click();
  await expect(player.getByText(/^00:00 \/ 00:10$/)).toBeVisible();
  await player.getByRole("slider", { name: "录像进度" }).fill("5");
  await expect(player.getByText(/^00:05 \/ 00:10$/)).toBeVisible();

  // 原始事件视图保留审计用的明文事件流。
  await dialog.getByRole("tab", { name: "原始事件" }).click();
  await expect(dialog.locator(".argus-recording-events")).toContainText("uptime");
  await expect(dialog.locator(".argus-recording-events")).toContainText("120x30");
});
