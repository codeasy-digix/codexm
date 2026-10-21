import { test, expect } from "@playwright/test";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

test("real MCP panel removes registration and preserves saved login", async ({ page }) => {
  const host = process.env.CODEXM_PANEL_URL;
  const control = process.env.CODEXM_WEB_FIXTURE;
  test.skip(!host || !control, "explicit isolated panel host required");
  const fixture = JSON.parse(readFileSync(control + "/ready.json", "utf8"));
  const authPath = resolve(fixture.state, "../accounts/account-secondary/auth.json");
  const before = readFileSync(authPath);
  await page.goto(host!);
  await page.getByRole("button", { name: "사이드바: codexm 관리", exact: true }).click();
  const frame = page.frameLocator("#panel");
  await frame.getByRole("button", { name: "계정", exact: true }).click();
  await frame.getByRole("button", { name: "account-secondary 등록 삭제", exact: true }).click();
  const dialog = frame.getByRole("dialog");
  await expect(dialog).toContainText("로그인 파일·공용 기본 로그인·대화 기록은 보존");
  await dialog.getByRole("button", { name: "취소", exact: true }).click();
  await frame.getByRole("button", { name: "account-secondary 등록 삭제", exact: true }).click();
  await dialog.getByRole("button", { name: "등록 삭제", exact: true }).click();
  await expect(frame.getByRole("button", { name: "account-secondary 등록 삭제", exact: true })).toHaveCount(0);
  expect(readFileSync(authPath).equals(before)).toBe(true);
  await expect(frame.getByRole("button", { name: "account-work 등록 삭제", exact: true })).toBeVisible();
  await page.screenshot({ path: control + "/panel-after-removal.png" });
});
