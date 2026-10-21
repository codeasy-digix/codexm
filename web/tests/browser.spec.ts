import { test, expect } from "@playwright/test";
import { readFileSync } from "node:fs";
const base = process.env.CODEXM_WEB_URL || "http://127.0.0.1:4317";
const state = process.env.CODEXM_WEB_STATE || "";
test("real Meteor browser: auth, management without models, Korean input and reload", async ({
  page,
  context,
}) => {
  test.skip(!state, "explicit isolated CODEXM_WEB_STATE required");
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto(base);
  await expect(
    page.getByRole("heading", { name: "codexm", exact: true }),
  ).toBeVisible();
  const denied = await page.evaluate(async () => {
    const response = await fetch("/auth/ticket", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: "{}",
    });
    return response.status;
  });
  expect(denied).toBe(401);
  const deniedMethod = await page.evaluate(
    () =>
      new Promise<string>((resolve) => {
        (window as any).Meteor.apply(
          "codexm.call",
          ["snapshot", {}, ""],
          { noRetry: true },
          (error: any) => resolve(error?.error),
        );
      }),
  );
  expect(deniedMethod).toBe("authentication_required");
  const token = readFileSync(state + "/access", "utf8").trim();
  await page.getByLabel("로컬 접속 코드").fill(token);
  await page.getByRole("button", { name: "연결", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "새 채팅", exact: true }),
  ).toBeVisible({ timeout: 20000 });
  await page.getByRole("button", { name: "계정", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "계정 관리", exact: true }),
  ).toBeVisible();
  await expect(page.getByLabel("계정 별칭")).toBeVisible();
  await page.getByRole("button", { name: "설정", exact: true }).click();
  await expect(
    page.getByLabel("작업 계정 우선순위 · 쉼표로 구분"),
  ).toBeVisible();
  await page.getByRole("button", { name: "새 채팅", exact: true }).click();
  const input = page.getByRole("textbox", { name: "메시지", exact: true });
  await input.fill("한글 초안");
  await input.dispatchEvent("keydown", {
    key: "Enter",
    keyCode: 229,
    isComposing: true,
  });
  await expect(input).toHaveValue("한글 초안");
  await input.press("Shift+Enter");
  await input.press("End");
  await expect(input).toHaveValue("한글 초안\n");
  await page.reload();
  await expect(input).toHaveValue("한글 초안\n", { timeout: 15000 });
  await input.fill("/accounts");
  await input.press("Enter");
  await expect(
    page.getByRole("heading", { name: "계정 관리", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "새 채팅", exact: true }).click();
  await page.screenshot({ path: state + "/browser-chat.png" });
  const originDenied = await context.request.post(base + "/auth/ticket", {
    headers: {
      Origin: "https://evil.example",
      "Content-Type": "application/json",
    },
    data: "{}",
  });
  expect(originDenied.status()).toBe(403);
  const hostile = await context.newPage();
  await hostile.route("https://evil.example/**", (route) =>
    route.fulfill({
      contentType: "text/html",
      body: "<title>foreign origin test</title>",
    }),
  );
  await hostile.goto("https://evil.example/");
  const blocked = await hostile.evaluate(
    (url) =>
      new Promise<boolean>((resolve) => {
        const socket = new WebSocket(
          url.replace("http:", "ws:") + "/websocket",
        );
        socket.onopen = () => {
          socket.close();
          resolve(false);
        };
        socket.onerror = () => resolve(true);
      }),
    base,
  );
  expect(blocked).toBe(true);
  await hostile.close();
  expect(errors).toEqual([]);
});
