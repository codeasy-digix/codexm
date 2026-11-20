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
  const thread = new URL(draftAddress).pathname.split("/").at(-1)!;
  const input = page.locator(".composer textarea");
  const executionAccount = () => page.evaluate(threadId => new Promise<string | undefined>((resolve, reject) => {
    (window as any).Meteor.apply("codexm.call", ["web.history", { thread_id: threadId, account: "first-browser-login" }, ""],
      { noRetry: true }, (error: Error | null, history: { executionAccount?: string }) => {
        if (error) reject(error);
        else resolve(history.executionAccount);
      });
  }), thread);
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
  await expect.poll(executionAccount).toBe("first-browser-login");
  await expect(page.getByRole("button", { name: "현재 턴 중지" })).toBeVisible();
  await expect(page.locator(".chat-header")).not.toContainText("현재 실행");
  await input.fill("계정 B에서 실행할 지시");
  await account.selectOption("second-browser-login");
  await expect(input).toHaveValue("계정 B에서 실행할 지시");
  await expect.poll(executionAccount).toBe("first-browser-login");
  await input.press("Enter");
  await expect.poll(executionAccount).toBe("second-browser-login");
  await expect(page).toHaveURL(draftAddress);

  const queueMessage = "이전 작업이 끝난 뒤 실행할 대기 입력";
  await account.selectOption("first-browser-login");
  await input.fill(queueMessage);
  holdMode = "queue";
  await input.press("ControlOrMeta+Enter");
  await checkLocked(queueMessage, "큐에 추가 중…");
  await expect(page.getByText("first-browser-login 계정의 입력을 큐에 추가했습니다. 이전 작업을 먼저 이어갑니다.")).toBeVisible();
  await expect.poll(executionAccount).toBe("first-browser-login");
  await expect(page.locator(".user-message").getByText(queueMessage, { exact: true })).toHaveCount(0);
  await page.locator(".queue-preview summary").click();
  const continuation = page.locator(".queue-entry").filter({ hasText: "이전 작업 이어가기" });
  await expect(continuation).toContainText("진행 중");
  const queued = page.locator(".queue-entry").filter({ has: page.getByRole("button", { name: "수정", exact: true }) });
  await expect(queued).toContainText("first-browser-login");
  await queued.getByRole("button", { name: "수정", exact: true }).click();
  await page.getByLabel("대기 메시지 수정").fill("편집 후 대기한 입력");
  await page.getByRole("button", { name: "수정 저장", exact: true }).click();
  await expect(queued).toContainText("편집 후 대기한 입력");

  await page.getByRole("button", { name: "현재 턴 중지", exact: true }).click();
  const paused = page.getByText("대기열 일시 중지", { exact: true });
  await expect(paused).toBeVisible();
  await expect(page.getByRole("button", { name: "현재 턴 중지", exact: true })).toHaveCount(0);
  await page.reload();
  await expect(paused).toBeVisible();
  const later = "일시 중지 중에 등록한 입력";
  await input.fill(later);
  await page.getByRole("button", { name: "큐에 추가", exact: true }).click();
  await expect(input).toHaveValue("");
  await expect(page.getByText("대기 중인 입력 2개", { exact: true })).toBeVisible();
  await page.goto("about:blank");
  writeFileSync(join(fixture!, "release-model"), "continue only isolated fixtures", { mode: 0o600 });
  const queueFile = join(config.state, "..", "web-chat", "queue", thread + ".json");
  const pendingCount = () => {
    try { return JSON.parse(readFileSync(queueFile, "utf8")).filter((entry: {state: string}) => entry.state !== "held").length; }
    catch (error) { if ((error as NodeJS.ErrnoException).code === "ENOENT") return 0; throw error; }
  };
  // Let the detached worker poll while the queue is paused.
  await new Promise(resolve => setTimeout(resolve, 2500));
  expect(pendingCount()).toBe(2);
  await page.goto(draftAddress);
  await expect(paused).toBeVisible();
  await expect(account).toHaveValue("first-browser-login");
  await page.getByRole("button", { name: "대기열 재개", exact: true }).click();
  await expect(paused).toHaveCount(0);
  await expect.poll(pendingCount).toBe(0);
  await expect(page.locator(".user-message").getByText(/^이 채팅에서 진행하던 미완료 작업을 계속 진행하세요/)).toHaveCount(1);
  await expect(page.locator(".user-message p").filter({ hasText: /편집 후 대기한 입력|일시 중지 중에 등록한 입력/ }))
    .toHaveText(["편집 후 대기한 입력", later]);
});
