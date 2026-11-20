import { expect, test } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { readFileSync, realpathSync } from "node:fs";
import { join } from "node:path";
import { WebSocket } from "ws";

const fixture = process.env.CODEXM_CHAT_GUI_FIXTURE;
const binary = process.env.CODEXM_CHAT_GUI_BINARY;
const origin = process.env.CODEXM_WEB_TEST_URL;
test.skip(!fixture || !binary || !origin || process.env.CODEXM_WEB_TEST_MODE !== "terminal", "requires the bundled Rust PTY fixture");

test("the dock opens an authenticated real shell at the project or home and hides without disconnecting", async ({ page, context }) => {
  const f = JSON.parse(readFileSync(join(fixture!, "fixture.json"), "utf8"));
  const home = realpathSync(f.home), cwd = realpathSync(join(f.home, "browse-target"));
  execFileSync(binary!, ["--config", f.config, "web", "--no-open"], { stdio: "pipe", env: { ...process.env, HOME: f.home } });
  await page.addInitScript(() => Object.defineProperty(navigator, "platform", { value: "Win32" }));
  await page.goto(new URL(`/chat/${f.thread}`, origin!).href);
  await page.getByLabel("로컬 접속 코드", { exact: true }).fill(readFileSync(join(f.state, "access"), "utf8").trim());
  await page.getByRole("button", { name: "연결", exact: true }).click();
  const toggle = page.getByRole("button", { name: "터미널", exact: true });
  await expect(toggle).toBeEnabled();
  await expect(toggle).toHaveAttribute("aria-keyshortcuts", "Control+J");
  const sidebar = page.getByRole("complementary", { name: "탐색" });
  await expect(sidebar).toBeVisible();
  await page.keyboard.press("Control+b");
  await expect(sidebar).toBeHidden();
  await expect(page.getByRole("button", { name: "채팅 목록 열기" })).toHaveAttribute("aria-expanded", "false");
  await page.keyboard.press("Control+b");
  await expect(sidebar).toBeVisible();
  const source = page.getByRole("group", { name: "긴 명령 출력 검증", exact: true });
  await expect(source).toBeVisible();
  const sourcePath = (await source.getByRole("link").getAttribute("href"))!;
  const buttons = await source.getByRole("button").evaluateAll(items => items.map(item => {
    const { x, y } = item.getBoundingClientRect();
    return { x, y };
  }));
  expect(buttons).toHaveLength(4);
  expect(buttons[0].y).toBe(buttons[1].y);
  expect(buttons[2].y).toBe(buttons[3].y);
  expect(buttons[0].x).toBe(buttons[2].x);
  await source.getByRole("button", { name: "이름 변경" }).click();
  const rename = page.getByRole("dialog", { name: "채팅 이름 변경" });
  await rename.getByLabel("채팅 이름").fill("새로운 채팅 이름 검증");
  await rename.getByRole("button", { name: "저장", exact: true }).click();
  await expect(page).toHaveURL(new URL(`/chat/${f.thread}`, origin!).href);
  const names = page.getByRole("group", { name: "새로운 채팅 이름 검증", exact: true });
  const renamed = names.filter({ has: page.locator(`a[href="${sourcePath}"]`) });
  await renamed.getByRole("button", { name: "채팅 복제 (Fork)", exact: true }).click();
  await page.getByRole("dialog", { name: "채팅 복제 (Fork)" }).getByRole("button", { name: "복제", exact: true }).click();
  await expect(page.getByText("채팅을 복제했습니다.")).toBeVisible();
  await expect(page.locator("main.conversation")).toHaveAttribute("data-thread-id", /[0-9a-f-]{36}/);
  await expect(page).not.toHaveURL(new URL(sourcePath, origin!).href);
  await expect(renamed).toBeVisible();
  const forkAddress = page.url();
  await page.getByRole("button", { name: "프로젝트 추가", exact: true }).click();
  const add = page.getByRole("dialog", { name: "프로젝트 추가", exact: true });
  await add.getByLabel("프로젝트 이름").fill("터미널 용 작업 폴더");
  await add.getByLabel("이 머신의 작업 폴더").fill(cwd);
  await add.getByRole("button", { name: "프로젝트 추가", exact: true }).click();
  await expect(page.getByLabel("프로젝트 필터").locator("option:checked")).toHaveText("터미널 용 작업 폴더");
  await page.getByRole("button", { name: "새 채팅", exact: true }).click();
  await page.getByRole("button", { name: "채팅 만들기", exact: true }).click();
  await expect(page).not.toHaveURL(forkAddress);
  await expect(toggle).toBeEnabled();
  const projectChat = page.url();
  const cookie = (await context.cookies()).find(item => item.name === "codexm_session")!;
  const endpoint = new URL("/auth/terminal-ticket", origin!).href;
  const socketURL = new URL("/terminal/ws", origin!);
  socketURL.protocol = "ws:";
  const assertRefused = (origin: string, cookieHeader?: string) => new Promise<number>((resolve, reject) => {
    const socket = new WebSocket(socketURL, { origin, headers: cookieHeader ? { Cookie: cookieHeader } : {}, handshakeTimeout: 3000 });
    socket.on("error", error => reject(error));
    socket.once("unexpected-response", (request, response) => { response.resume(); request.destroy(); resolve(response.statusCode!); });
    socket.once("open", () => { socket.close(); reject(new Error("unexpected terminal upgrade")); });
  });
  expect((await fetch(endpoint, { method: "POST", headers: { Origin: origin!, "Content-Type": "application/json" }, body: "{}" })).status).toBe(401);
  expect(await assertRefused(origin!)).toBe(401);
  expect(await page.evaluate(() => fetch("/auth/terminal-ticket", { method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ thread: "../untrusted-directory" }) }).then(response => response.status))).toBe(400);

  let output = "";
  let sockets = 0;
  let authFrame: string | undefined;
  page.on("websocket", socket => {
    if (!socket.url().endsWith("/terminal/ws")) return;
    sockets++;
    socket.on("framesent", event => {
      if (JSON.parse(String(event.payload)).type === "auth") authFrame = String(event.payload);
    });
    socket.on("framereceived", event => {
      const message = JSON.parse(String(event.payload));
      if (message.type === "output") output += message.data;
    });
  });
  const dock = page.getByRole("region", { name: "터미널", exact: true });
  await page.keyboard.press("Control+j");
  await expect(dock).toBeVisible();
  await expect(dock.getByRole("status")).toHaveText(cwd);
  const field = dock.getByRole("textbox", { name: "터미널 입력" });
  await expect(field).toBeFocused();
  const write = async (command: string) => {
    await field.focus();
    await page.keyboard.insertText(command);
    await page.keyboard.press("Enter");
  };
  await write("printf '%s\\n' CODEXM_PROJECT_PATH:$(pwd -P)");
  await expect.poll(() => output).toContain("CODEXM_PROJECT_PATH:" + cwd);
  await write("printf 'CODEXM_SHELL_PID:%s\\n' $$");
  await expect.poll(() => output).toMatch(/CODEXM_SHELL_PID:[1-9]\d*/);
  const pid = Number(output.match(/CODEXM_SHELL_PID:([1-9]\d*)/)![1]);
  const alive = () => {
    try { process.kill(pid, 0); return true; }
    catch { return false; }
  };
  expect(alive()).toBe(true);
  await page.keyboard.press("Control+j");
  await expect(dock).toBeHidden();
  await page.keyboard.press("Control+j");
  await expect(dock).toBeVisible();
  expect(sockets).toBe(1);
  expect(alive()).toBe(true);

  const replayed = await new Promise<number>((resolve, reject) => {
    const replay = new WebSocket(socketURL, { origin, headers: { Cookie: `${cookie.name}=${cookie.value}` } });
    replay.on("open", () => replay.send(authFrame!));
    replay.on("message", event => {
      if (JSON.parse(event.toString()).type === "ready") { replay.close(); reject(new Error("terminal ticket was replayable")); }
    });
    replay.on("error", reject);
    replay.on("close", resolve);
  });
  expect(replayed).toBe(1008);

  await dock.getByRole("button", { name: "터미널 종료", exact: true }).click();
  await expect.poll(alive).toBe(false);
  await page.getByLabel("프로젝트 필터").selectOption("");
  await page.getByRole("button", { name: "새 채팅", exact: true }).click();
  await page.getByLabel("이 머신의 작업 폴더").fill(cwd);
  await page.getByRole("button", { name: "채팅 만들기", exact: true }).click();
  await expect(page).not.toHaveURL(projectChat);
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.locator("#chat-sidebar")).not.toBeInViewport();
  await page.keyboard.press("Control+b");
  await expect(page.locator("#chat-sidebar")).toBeInViewport();
  await page.keyboard.press("Control+b");
  await expect(page.locator("#chat-sidebar")).not.toBeInViewport();
  await toggle.click();
  await expect(dock.getByRole("status")).toHaveText(home);
  await expect(dock.getByRole("button", { name: "Ctrl+C" })).toBeVisible();
  await write("printf '%s\\n' CODEXM_HOME_PATH:$(pwd -P)");
  await expect.poll(() => output).toContain("CODEXM_HOME_PATH:" + home);
});
