import { expect, test } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const fixture = process.env.CODEXM_CHAT_GUI_FIXTURE;
const binary = process.env.CODEXM_CHAT_GUI_BINARY;
const url = process.env.CODEXM_WEB_TEST_URL;
const baseline = process.env.CODEXM_WEB_QUOTA_BASELINE === "1";
test.skip(!fixture || !binary || !url || process.env.CODEXM_WEB_TEST_MODE !== "quota",
  "requires the isolated quota backend and official runtime");

test("a real Codex 429 leaves a writer; using another account keeps the same chat", async ({ page }) => {
  const config = JSON.parse(readFileSync(join(fixture!, "fixture.json"), "utf8"));
  const blocked = baseline || config.ownerTransport === "stdio";
  execFileSync(binary!, ["--config", config.config, "web", "--no-open"], {
    env: { ...process.env, HOME: config.home }, stdio: "pipe",
  });
  const address = new URL("/chat/" + config.thread, url!).href;
  await page.goto(address);
  await page.getByLabel("로컬 접속 코드", { exact: true }).fill(readFileSync(join(config.state, "access"), "utf8").trim());
  await page.getByRole("button", { name: "연결", exact: true }).click();
  await expect(page.getByRole("heading", { name: "한도 0% 재현", exact: true })).toBeVisible();
  const limits = page.getByRole("group", { name: "선택 계정의 남은 한도" });
  const bar = limits.getByRole("meter", { name: config.window === "5h" ? "5시간 남은 한도" : "주간 남은 한도" });
  await expect(bar).toHaveAttribute("aria-valuetext", "0% 남음");
  await expect(page.locator(".turn")).toHaveCount(1);
  await expect(page.getByText("WEB_FIXTURE_QUOTA_429", { exact: true })).toBeVisible();
  await page.screenshot({ path: join(fixture!, "quota-exhausted.png") });

  const account = page.getByRole("combobox", { name: "이번에 사용할 계정" });
  await account.selectOption(config.target);
  await expect(bar).toHaveAttribute("aria-valuetext", config.window === "5h" ? "80% 남음" : "70% 남음");
  const prompt = "QUOTA_FIXTURE_HEALTHY_FOLLOWUP";
  const input = page.getByRole("textbox", { name: "메시지", exact: true });
  await input.fill(prompt);
  await input.press("Enter");
  if (blocked) {
    await expect(page.locator(".composer-wrap").getByRole("alert").first()).toContainText("다른 앱");
    await expect(input).toHaveValue(prompt);
  } else {
    await expect(page.getByText(prompt, { exact: true })).toHaveCount(1);
    await expect(input).toHaveValue("");
    await expect(account).toHaveValue(config.target);
  }
  await expect(page).toHaveURL(address);
  writeFileSync(join(fixture!, "verify-quota-handoff"), "", { mode: 0o600 });
  await expect.poll(() => {
    try { return JSON.parse(readFileSync(join(fixture!, "quota-handoff.json"), "utf8")); }
    catch { return null; }
  }).toEqual({ oldOwnerKeepsThread: blocked, neighborContinues: true,
    healthyRequests: blocked ? 0 : 1, nativeUsageLimit: true });
  await page.screenshot({ path: join(fixture!, blocked ? "handoff-blocked.png" : "handoff-complete.png") });
});
