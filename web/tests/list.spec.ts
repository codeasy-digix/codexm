import { test, expect } from "@playwright/test";
import { readFileSync } from "node:fs";
const url = process.env.CODEXM_WEB_TEST_URL || "http://127.0.0.1:14317";
const passwordFile = process.env.CODEXM_WEB_TEST_PASSWORD_FILE;
test.skip(!process.env.CODEXM_WEB_DENSE_LIST_TEST || !passwordFile, "dense isolated conversation fixture required");

for (const viewport of [{ width: 375, height: 667 }, { width: 390, height: 844 }, { width: 1360, height: 900 }]) {
  test(`many chats remain visible and selectable at ${viewport.width}x${viewport.height}`, async ({ page }) => {
    await page.setViewportSize(viewport);
    await page.goto(url);
    await page.getByLabel("아이디", { exact: true }).fill("fixture");
    await page.getByLabel("비밀번호", { exact: true }).fill(readFileSync(passwordFile!, "utf8").trim());
    await page.getByRole("button", { name: "로그인", exact: true }).click();
    const list = page.getByLabel("채팅 목록", { exact: true });
    await expect(list.locator("button")).toHaveCount(122);
    const metrics = await list.evaluate(el => {
      const rect = el.getBoundingClientRect();
      const rows = [...el.querySelectorAll("button")].map(row => row.getBoundingClientRect());
      return { height: rect.height, fullyVisible: rows.filter(row => row.top >= rect.top && row.bottom <= rect.bottom).length, tallest: Math.max(...rows.map(row => row.height)) };
    });
    expect(metrics.height).toBeGreaterThan(350);
    expect(metrics.fullyVisible).toBeGreaterThanOrEqual(4);
    expect(metrics.tallest).toBeLessThan(125);
    await list.evaluate(el => { el.scrollTop = el.scrollHeight; });
    const last = list.locator("button").last(), title = await last.locator("strong").textContent();
    const offset = await list.evaluate(el => el.scrollTop);
    await expect(last).toBeInViewport();
    await last.click();
    await expect(page.getByRole("heading", { name: title!, exact: true })).toBeVisible();
    await expect(page.getByLabel("이번에 사용할 계정")).toBeVisible();
    if (viewport.width < 580) {
      await expect(page.getByLabel("채팅 검색")).toBeHidden();
      await page.getByRole("button", { name: "← 채팅 목록", exact: true }).click();
      await expect(last).toBeInViewport();
      expect(await list.evaluate(el => el.scrollTop)).toBeCloseTo(offset, 0);
    }
    await page.getByLabel("채팅 검색").fill("한글 프로젝트 계획");
    await expect(list.locator("button")).toHaveCount(1);
    await page.getByLabel("채팅 검색").fill("");
    await expect(list.locator("button")).toHaveCount(122);
    await page.getByLabel("보관된 채팅 포함").check();
    await expect(list.locator("button")).toHaveCount(123);
    await page.getByRole("button", { name: "로그아웃", exact: true }).click();
  });
}
