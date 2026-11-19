import { expect, test } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const fixture = process.env.CODEXM_CHAT_GUI_FIXTURE;
const binary = process.env.CODEXM_CHAT_GUI_BINARY;
const url = process.env.CODEXM_WEB_TEST_URL;
test.skip(!fixture || !binary || !url || process.env.CODEXM_WEB_TEST_MODE !== "web-only",
  "requires the isolated web-only app and official runtime");

test("an account change only takes effect when a chat input is submitted", async ({ page }) => {
  const config = JSON.parse(readFileSync(join(fixture!, "fixture.json"), "utf8"));
  let holdMode: "immediate" | "queue" | undefined;
  let release: (() => void) | undefined;
  await page.routeWebSocket(target => target.pathname.endsWith("/websocket"), socket => {
    const server = socket.connectToServer();
    socket.onMessage(message => {
      const frame = JSON.parse(String(message));
      const requests = Array.isArray(frame) ? frame.map(item => JSON.parse(item)) : [frame];
      if (holdMode && requests.some(request => request.method === "codexm.call" &&
        request.params?.[0] === "web.send" && request.params[1].mode === holdMode)) {
        holdMode = undefined;
        release = () => server.send(message);
      } else {
        server.send(message);
      }
    });
  });
  const cli = (args: string[]) => execFileSync(binary!, ["--config", config.config, "web", ...args], {
    stdio: "pipe", env: { ...process.env, HOME: config.home }, encoding: "utf8",
  });
  const status = JSON.parse(cli(["status"]));
  expect(status).toMatchObject({ running: true, os_service: true, external_supervisor: true, web_only: true });
  cli(["--no-open"]);
  await page.goto(url!);
  await page.getByLabel("로컬 접속 코드", { exact: true }).fill(readFileSync(join(config.state, "access"), "utf8").trim());
  await page.getByRole("button", { name: "연결", exact: true }).click();
  const account = page.getByRole("combobox", { name: "이번에 사용할 계정" });
  await expect(account).toHaveValue("default");
  const quit = page.locator(".sidebar .brand").getByRole("button", { name: "GUI 종료", exact: true });
  await expect(quit).toHaveCount(process.platform === "darwin" ? 1 : 0);
  await page.getByRole("button", { name: "새 채팅", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("이 머신의 작업 폴더").fill(config.home);
  await dialog.getByLabel("채팅을 시작할 계정").selectOption("first-browser-login");
  await dialog.getByRole("button", { name: "채팅 만들기", exact: true }).click();
  await expect(page).toHaveURL(/\/chat\/[a-f0-9-]+$/);
  const draftAddress = page.url();
  const input = page.locator(".composer textarea");
  async function checkLocked(message: string, label: string) {
    await expect.poll(() => !!release).toBe(true);
    await expect(input).toBeDisabled();
    await expect(input).toHaveValue(message);
    await expect(page.getByRole("button", { name: label, exact: true })).toBeDisabled();
    release!();
    release = undefined;
    await expect(input).toBeEditable();
    await expect(input).toHaveValue("");
  }
  const firstMessage = "GUI_FIXTURE_ACTIVE_BROWSER ACCOUNT_A";
  await input.fill(firstMessage);
  holdMode = "immediate";
  await page.getByRole("button", { name: "보내기", exact: true }).click();
  await checkLocked(firstMessage, "전달 중…");
  await expect(page.getByText("현재 실행 first-browser-login", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "현재 턴 중지" })).toBeVisible();
  await input.fill("계정 B에서 실행할 지시");
  await account.selectOption("second-browser-login");
  await expect(input).toHaveValue("계정 B에서 실행할 지시");
  await expect(page.getByText("현재 실행 first-browser-login", { exact: true })).toBeVisible();
  await input.press("Enter");
  await expect(page.getByText("현재 실행 second-browser-login", { exact: true })).toBeVisible();
  await expect(page).toHaveURL(draftAddress);

  const queueMessage = "등록한 계정으로만 실행할 대기 입력";
  await input.fill(queueMessage);
  holdMode = "queue";
  await input.press("ControlOrMeta+Enter");
  await checkLocked(queueMessage, "큐에 추가 중…");
  await expect(page.getByText("second-browser-login 계정의 입력을 큐에 추가했습니다.")).toBeVisible();
  await page.locator(".queue-preview summary").click();
  await expect(page.locator(".queue-entry")).toContainText("second-browser-login");
  await page.locator(".queue-entry").getByRole("button", { name: "수정", exact: true }).click();
  await page.getByLabel("대기 메시지 수정").fill("편집 후 대기한 입력");
  await page.getByRole("button", { name: "수정 저장", exact: true }).click();
  await expect(page.locator(".queue-entry")).toContainText("편집 후 대기한 입력");
  await page.goto("about:blank");
  writeFileSync(join(fixture!, "release-model"), "continue only isolated fixtures", { mode: 0o600 });
  // Verify dispatch with no browser connected before restoring the page.
  const queueFile = join(config.state, "..", "web-chat", "queue", draftAddress.split("/").at(-1)! + ".json");
  await expect.poll(() => {
    try { return JSON.parse(readFileSync(queueFile, "utf8")).length; }
    catch (error) { if ((error as NodeJS.ErrnoException).code === "ENOENT") return 0; throw error; }
  }).toBe(0);
  await page.goto(draftAddress);
  await expect(account).toHaveValue("second-browser-login");
  await expect(page.getByText("편집 후 대기한 입력", { exact: true })).toHaveCount(1);
});
