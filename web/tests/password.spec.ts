import { test, expect } from "@playwright/test";
import { readFileSync } from "node:fs";
const url = process.env.CODEXM_WEB_TEST_URL || "http://127.0.0.1:14317";
const filename = process.env.CODEXM_WEB_TEST_PASSWORD_FILE;
test.skip(!filename, "isolated HTTPS password fixture required");
test("HTTPS login denies anonymous/incorrect/foreign-origin requests and uses a secure cookie", async ({ page, context, request }) => {
  const denied = await request.post(url + "/auth/login", { headers: { Origin: "https://evil.example" }, data: { username: "fixture", password: "wrong" } });
  expect(denied.status()).toBe(403);
  await page.goto(url);
  await expect(page.getByLabel("아이디", { exact: true })).toBeVisible();
  await expect(page.getByLabel("채팅 검색")).toHaveCount(0);
  expect(await page.evaluate(() => fetch("/auth/ticket", { method: "POST", headers: { "Content-Type": "application/json" }, body: "{}" }).then(r => r.status))).toBe(401);
  const blocked = await page.evaluate(() => new Promise(resolve => (window as any).Meteor.call("codexm.call", "snapshot", {}, "", (error: any) => resolve(error?.error))));
  expect(blocked).toBe("authentication_required");
  await page.getByLabel("아이디", { exact: true }).fill("fixture");
  const passwordInput = page.getByLabel("비밀번호", { exact: true });
  await passwordInput.fill("wrong-password");
  await expect(passwordInput).toBeFocused();
  await page.evaluate(() => window.dispatchEvent(new Event("blur")));
  await expect(passwordInput).not.toBeFocused();
  let releaseLogin!: () => void;
  const pendingLogin = new Promise<void>(resolve => { releaseLogin = resolve; });
  await page.route(url + "/auth/login", async route => {
    await pendingLogin;
    await route.continue();
  }, { times: 1 });
  try {
    await passwordInput.press("Enter");
    await expect(page.getByRole("button", { name: "연결 중…", exact: true })).toBeDisabled();
    await expect(passwordInput).not.toBeFocused();
  } finally {
    releaseLogin();
  }
  await expect(page.getByRole("alert")).toContainText("아이디 또는 비밀번호");
  await expect(passwordInput).not.toBeFocused();
  await passwordInput.fill(readFileSync(filename!, "utf8").trim());
  await page.getByRole("button", { name: "로그인", exact: true }).click();
  await expect(page.getByLabel("채팅 검색")).toBeVisible();
  const cookie = (await context.cookies()).find(c => c.name === "codexm_session")!;
  expect(cookie.httpOnly).toBe(true); expect(cookie.secure).toBe(true); expect(cookie.sameSite).toBe("Strict");
  expect(cookie.expires - Date.now() / 1000).toBeGreaterThan(89 * 24 * 3600);
  const reopened = await context.browser()!.newContext({ ignoreHTTPSErrors: true, storageState: { cookies: [cookie], origins: [] } });
  try {
    const resumed = await reopened.newPage();
    await resumed.goto(url);
    await expect(resumed.getByLabel("채팅 검색")).toBeVisible();
    await expect(resumed.getByLabel("아이디", { exact: true })).toHaveCount(0);
    const renew = () => resumed.evaluate(() => fetch("/auth/renew", {
      method: "POST", headers: { "Content-Type": "application/json" }, body: "{}",
    }).then(response => response.status));
    expect(await renew()).toBe(200);
    await page.getByRole("button", { name: "설정", exact: true }).click();
    const settings = page.getByRole("dialog", { name: "설정", exact: true });
    await settings.getByRole("button", { name: "계정 관리", exact: true }).click();
    await settings.getByRole("button", { name: "로그아웃", exact: true }).click();
    await expect(page.getByLabel("채팅 검색")).toHaveCount(0);
    expect(await renew()).toBe(401);
    await expect(resumed.getByLabel("아이디", { exact: true })).toBeVisible();
  } finally {
    await reopened.close();
  }
});
