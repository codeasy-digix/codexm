import { expect, test, type Page } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { readFileSync, realpathSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { MongoClient } from "mongodb";

const fixture = process.env.CODEXM_CHAT_GUI_FIXTURE;
const binary = process.env.CODEXM_CHAT_GUI_BINARY;
const url = process.env.CODEXM_WEB_TEST_URL;
test.skip(!fixture || !binary || !url || process.env.CODEXM_WEB_TEST_MODE !== "workspace",
  "requires the isolated workspace, official runtime and private MongoDB");

function row(page: Page, name: string) {
  return page.getByLabel("채팅 목록", { exact: true }).getByRole("group", { name, exact: true });
}

test("native workspace catalog, selection, drafts and goals survive browser navigation", async ({ page }) => {
  page.setDefaultTimeout(12_000);
  const f = JSON.parse(readFileSync(join(fixture!, "fixture.json"), "utf8"));
  execFileSync(binary!, ["--config", f.config, "web", "--no-open"], { stdio: "pipe", env: { ...process.env, HOME: f.home } });
  await page.emulateMedia({ colorScheme: "light" });
  await page.goto(url!);
  await page.getByLabel("로컬 접속 코드", { exact: true }).fill(readFileSync(join(f.state, "access"), "utf8").trim());
  await page.getByRole("button", { name: "연결", exact: true }).click();

  const firstName = "긴 이력 페이지 테스트";
  const otherName = "긴 명령 출력 검증";
  const first = row(page, firstName), other = row(page, otherName);
  const rows = page.getByLabel("채팅 목록", { exact: true }).getByRole("group");
  await expect(first).toBeVisible();
  await expect(first).toHaveCSS("background-color", "rgba(0, 0, 0, 0)");
  await expect(first).toHaveCSS("border-top-width", "1px");
  await expect(first).toHaveCSS("border-top-color", "rgb(223, 228, 223)");
  const browserOrder = await rows.evaluateAll(items => items.map(item => item.querySelector("a")?.getAttribute("href")?.split("/").at(-1)));
  if (process.env.CODEXM_WEB_TEST_MONGO === "1") {
    const socket = join(realpathSync(join(f.state, "history-cache")), "mongodb.sock");
    const client = new MongoClient(`mongodb://${encodeURIComponent(socket)}/codexm`, { directConnection: true });
    try {
      await expect.poll(async () => {
        const db = client.db();
        const current = await db.collection<{ _id: string; generation: string }>("catalog_manifest_v1").findOne({ _id: "current" });
        if (!current) return [];
        const chunks = await db.collection("catalog_pages_v1").find({ generation: current.generation }).sort({ index: 1 }).toArray();
        const saved = chunks.flatMap(chunk => chunk.rows as Array<{ at: number; thread: { id: string; archived?: boolean } }>);
        expect(saved.map(item => item.at)).toEqual(saved.map(item => item.at).sort((a, b) => b - a));
        expect(saved.some(item => item.thread.id === f.archived && item.thread.archived)).toBe(true);
        return saved.filter(item => !item.thread.archived).map(item => item.thread.id);
      }).toEqual(browserOrder);
    } finally { await client.close(); }
  }

  await first.getByRole("link").click();
  const chatURL = new URL(`/chat/${f.thread}`, url!).href;
  await expect(page).toHaveURL(chatURL);
  await expect(first.getByRole("link")).toHaveAttribute("aria-current", "page");
  await expect(first).toHaveCSS("background-color", "rgb(220, 235, 225)");
  await expect(other).toHaveCSS("background-color", "rgba(0, 0, 0, 0)");
  await expect(page.locator(".user-message p").last()).toHaveCSS("background-color", "rgb(242, 247, 244)");
  await expect(page.locator(".user-message p").last()).toHaveCSS("border-top-width", "1px");
  const header = page.locator(".chat-header");
  const quota = header.getByRole("group", { name: "선택 계정의 남은 한도" });
  const [account, limits, model] = await Promise.all([header.getByLabel("이번에 사용할 계정").boundingBox(),
    quota.boundingBox(),
    header.getByRole("combobox", { name: "모델", exact: true }).boundingBox()]);
  expect(account).toBeTruthy(); expect(limits).toBeTruthy(); expect(model).toBeTruthy();
  expect(limits!.x + limits!.width).toBeLessThan(account!.x);
  expect(limits!.y + limits!.height).toBeCloseTo(account!.y + account!.height, 0);
  expect(model!.y).toBeCloseTo(account!.y, 0);
  const bars = await quota.locator("meter, .limit-track").evaluateAll(items => items.map(item => item.getBoundingClientRect().toJSON()));
  expect(bars).toHaveLength(2);
  expect(bars[1].y).toBeGreaterThan(bars[0].y);
  expect(bars[1].x).toBe(bars[0].x);
  expect(bars[0].width).toBeGreaterThanOrEqual(account!.width * 0.7);

  const generated = page.getByRole("link", { name: "fixture-comparison.html", exact: true });
  await expect(generated).toBeVisible();
  const artifactPath = (await generated.getAttribute("href"))!;
  const artifact = await page.request.get(new URL(artifactPath, url!).href);
  expect(artifact.ok()).toBe(true);
  expect(artifact.headers()["content-type"]).toBe("application/octet-stream");
  expect(artifact.headers()["content-disposition"]).toMatch(/^attachment;.*fixture-comparison\.html/);
  expect(await artifact.text()).toContain("<h1>Browser artifact fixture</h1>");
  expect((await fetch(new URL(artifactPath, url!))).status).toBe(401);
  expect((await page.request.get(new URL(artifactPath.replace(f.thread, f.second), url!).href)).status()).toBe(404);
  writeFileSync(join(f.home, "workspace-image.png"), Buffer.from(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aEioAAAAASUVORK5CYII=", "base64"));
  await page.getByRole("button", { name: "채팅 메뉴" }).click();
  await page.getByRole("menuitem", { name: "채팅 파일" }).click();
  const files = page.getByRole("dialog", { name: "채팅 파일" });
  await files.getByLabel("작업 폴더 기준 파일 경로").fill("../workspace-image.png");
  await files.getByRole("button", { name: "파일 등록" }).click();
  await expect(files.getByRole("alert")).toContainText("작업 폴더 기준");
  await files.getByLabel("작업 폴더 기준 파일 경로").fill("./workspace-image.png");
  await files.getByRole("button", { name: "파일 등록" }).click();
  const image = files.getByRole("link", { name: "workspace-image.png", exact: true });
  await expect(image).toBeVisible();
  const imagePath = (await image.getAttribute("href"))!;
  const inline = await page.request.head(new URL(imagePath, url!).href);
  expect(inline.ok()).toBe(true);
  expect(inline.headers()["content-type"]).toBe("image/png");
  expect(inline.headers()["content-disposition"]).toMatch(/^inline;/);
  await files.getByRole("button", { name: "닫기", exact: true }).last().click();
  const rendered = page.locator(".assistant-message img[alt='workspace-image.png']");
  await expect(rendered).toBeVisible();
  await expect.poll(() => rendered.evaluate(img => (img as HTMLImageElement).naturalWidth)).toBe(1);

  const composer = page.getByRole("textbox", { name: "메시지", exact: true });
  const sourceTurnId = await page.locator(".turn").last().getAttribute("data-turn-id");
  const suggested = "다음 작업으로 검증 결과를 정리해 주세요.";
  await page.evaluate(args => {
    const testWindow = window as any;
    const meteor = testWindow.Meteor, apply = meteor.apply;
    meteor.apply = function (method: string, params: any[], options: any, callback: any) {
      if (method !== "codexm.call" || params[0] !== "web.activity" || params[1]?.thread_id !== args.thread) {
        return apply.call(this, method, params, options, callback);
      }
      return apply.call(this, method, params, options, (error: any, result: any) => callback(error, error ? result : {
        ...result, prediction: { sourceTurnId: args.sourceTurnId, text: args.suggested },
      }));
    };
    testWindow.restorePredictions = () => { meteor.apply = apply; };
  }, { thread: f.thread, sourceTurnId, suggested });
  await expect(composer).toHaveAttribute("placeholder", suggested);
  await expect(composer).toHaveAttribute("aria-keyshortcuts", "Tab");
  await composer.press("a");
  await expect(composer).toHaveValue("a");
  await expect(composer).not.toHaveAttribute("placeholder", suggested);
  await composer.press("Backspace");
  await expect(composer).toHaveAttribute("placeholder", suggested);
  await composer.press("Tab");
  await expect(composer).toHaveValue(suggested);
  await expect(page.locator(".message-scroll").getByText(suggested, { exact: true })).toHaveCount(0);
  await composer.fill("");
  await expect(composer).toHaveAttribute("placeholder", suggested);
  await page.evaluate(() => (window as any).restorePredictions());

  await other.getByRole("button", { name: "고정하기", exact: true }).click();
  await expect(other.getByLabel("고정됨", { exact: true })).toBeVisible();
  await expect(page).toHaveURL(chatURL);
  await page.getByRole("button", { name: "고정", exact: true }).click();
  await other.getByRole("button", { name: "고정 해제", exact: true }).click();
  await expect(other).toHaveCount(0);
  await page.getByRole("button", { name: "전체 채팅", exact: true }).click();
  await other.getByRole("button", { name: "보관하기", exact: true }).click();
  await page.getByRole("dialog", { name: "채팅 보관" }).getByRole("button", { name: "적용" }).click();
  await expect(other).toHaveCount(0);
  await expect(page).toHaveURL(chatURL);
  await page.getByRole("button", { name: "보관함", exact: true }).click();
  await other.getByRole("button", { name: "보관 해제", exact: true }).click();
  await page.getByRole("dialog", { name: "보관 해제" }).getByRole("button", { name: "적용" }).click();
  await expect(other).toHaveCount(0);
  await page.getByRole("button", { name: "전체 채팅", exact: true }).click();

  await page.emulateMedia({ colorScheme: "dark" });
  await expect(page.locator("body")).toHaveCSS("background-color", "rgb(28, 29, 32)");
  await expect(page.locator('meta[name="theme-color"]')).toHaveAttribute("content", "#1c1d20");
  await expect(first).toHaveCSS("background-color", "rgb(56, 58, 65)");
  await expect(other).toHaveCSS("background-color", "rgba(0, 0, 0, 0)");
  await expect(other).toHaveCSS("border-top-color", "rgb(66, 68, 75)");
  const settings = page.getByRole("dialog", { name: "설정", exact: true });
  await page.getByRole("button", { name: "설정", exact: true }).click();
  await settings.getByRole("combobox", { name: "테마", exact: true }).selectOption("light");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await settings.getByRole("button", { name: "알림", exact: true }).click();
  await expect(settings.getByText(/채팅 작업이 끝나면/)).toBeVisible();
  await settings.getByRole("button", { name: "단축키", exact: true }).click();
  await expect(settings.getByLabel("단축키 1 키 조합", { exact: true })).toBeVisible();
  await settings.getByRole("button", { name: "계정 관리", exact: true }).click();
  await expect(settings.getByRole("button", { name: "로그아웃", exact: true })).toBeVisible();
  await page.keyboard.press("Escape");

  await page.getByRole("button", { name: "프로젝트 추가", exact: true }).click();
  const project = page.getByRole("dialog", { name: "프로젝트 추가" });
  await project.getByLabel("프로젝트 이름").fill("선택한 폴더 프로젝트");
  await project.getByLabel("이 머신의 작업 폴더").fill(f.home);
  await project.getByRole("button", { name: "폴더 찾기" }).click();
  await project.getByRole("button", { name: "browse-target", exact: true }).click();
  await project.getByRole("button", { name: "이 폴더 선택", exact: true }).click();
  await expect(project.getByLabel("이 머신의 작업 폴더")).toHaveValue(realpathSync(join(f.home, "browse-target")));
  await project.getByRole("button", { name: "프로젝트 추가", exact: true }).click();
  await expect(page.getByLabel("프로젝트 필터").locator("option:checked")).toHaveText("선택한 폴더 프로젝트");
  await page.getByLabel("프로젝트 필터").selectOption("");

  const input = page.getByRole("textbox", { name: "메시지", exact: true });
  const draft = "GUI_FIXTURE_ACTIVE_BROWSER_WORKSPACE 목표 진행 확인";
  await input.fill(draft);
  async function insertCommand(command: string) {
    await input.focus();
    await input.evaluate((textarea: HTMLTextAreaElement) => {
      const caret = textarea.value.indexOf("목표");
      textarea.setSelectionRange(caret, caret);
    });
    await page.keyboard.insertText(command);
  }
  await insertCommand("/go");
  await expect(page.getByRole("option", { name: /^\/goal/ })).toBeVisible();
  await input.press("Tab");
  await expect(input).toHaveValue(draft);
  await insertCommand("/fixture");
  await page.getByRole("option", { name: /^\/fixture-proof/ }).click();
  await expect(input).toHaveValue(draft);
  await expect(page.getByRole("button", { name: "목표 지정 제거" })).toBeVisible();
  await expect(page.getByRole("button", { name: "fixture-proof 스킬 제거" })).toBeVisible();

  const scroll = page.locator(".message-scroll");
  if (!(await page.getByText("페이지 이력 17", { exact: true }).count()))
    await page.getByRole("button", { name: "이전 메시지 불러오기", exact: true }).click();
  await expect(page.getByText("페이지 이력 17", { exact: true })).toHaveCount(1);
  const previousScroll = await scroll.evaluate(node => {
    node.scrollTop = Math.floor((node.scrollHeight - node.clientHeight) * 0.4);
    return node.scrollTop;
  });
  await other.getByRole("link").click();
  await expect(other).toHaveClass(/is-current/);
  await expect(first).toHaveCSS("background-color", "rgba(0, 0, 0, 0)");
  await expect(input).toHaveValue("");
  await page.evaluate(thread => {
    const testWindow = window as any;
    const meteor = testWindow.Meteor, apply = meteor.apply;
    testWindow.heldHistory = [];
    testWindow.heldCache = 0;
    meteor.apply = function (...args: any[]) {
      if (args[0] === "codexm.cachedHistory" && args[1][0]?.thread_id === thread) testWindow.heldCache++;
      if (args[0] === "codexm.call" && args[1][0] === "web.history" && args[1][1]?.thread_id === thread) {
        testWindow.heldHistory.push({ send: () => apply.apply(this, args), basis: args[1][1].history_basis });
        return;
      }
      return apply.apply(this, args);
    };
    testWindow.restoreHistory = () => {
      meteor.apply = apply;
      for (const pending of testWindow.heldHistory) pending.send();
    };
  }, f.thread);
  await first.getByRole("link").click();
  await expect(input).toHaveValue(draft);
  await expect(page.getByText("페이지 이력 17", { exact: true })).toHaveCount(1);
  await expect(page.getByText("최근 대화를 불러오는 중…")).toHaveCount(0);
  await expect.poll(() => scroll.evaluate(node => node.scrollTop)).toBeCloseTo(previousScroll, 0);
  expect(await page.evaluate(() => (window as any).heldCache)).toBe(0);
  await expect.poll(() => page.evaluate(() => (window as any).heldHistory[0]?.basis?.turns?.length ?? 0)).toBeGreaterThan(0);
  await page.evaluate(() => (window as any).restoreHistory());

  // Force a lost socket and one temporary ticket failure without reloading.
  let ticketAttempts = 0;
  await page.route("**/auth/ticket", route => ++ticketAttempts === 1
    ? route.fulfill({ status: 503, json: { error: "temporary fixture failure" } })
    : route.continue());
  await page.evaluate(() => (window as any).Meteor.disconnect());
  await expect(page.locator(".connection-banner")).toContainText("서버 연결을 복구하는 중…");
  await expect(page.getByText("페이지 이력 17", { exact: true })).toHaveCount(1);
  await expect(input).toHaveValue(draft);
  await expect(page.getByRole("button", { name: "보내기", exact: true })).toBeDisabled();
  await page.evaluate(() => (window as any).Meteor.reconnect());
  await expect.poll(() => ticketAttempts).toBeGreaterThanOrEqual(2);
  await expect(page.locator(".connection-banner")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "보내기", exact: true })).toBeEnabled();
  await expect(input).toHaveValue(draft);
  await expect(page).toHaveURL(chatURL);
  const context = page.context();
  await page.close();
  const reopened = await context.newPage();
  await reopened.addInitScript(() => Object.defineProperty(navigator, "platform", { value: "Win32" }));
  await reopened.emulateMedia({ colorScheme: "dark" });
  await reopened.goto(chatURL);
  await expect(reopened.locator("html")).toHaveAttribute("data-theme", "light");
  await expect(row(reopened, firstName).getByRole("link")).toHaveAttribute("aria-current", "page");
  const restored = reopened.getByRole("textbox", { name: "메시지", exact: true });
  await expect(restored).toHaveValue(draft);
  await expect(reopened.getByRole("button", { name: "목표 지정 제거" })).toBeVisible();
  await expect(reopened.getByRole("button", { name: "fixture-proof 스킬 제거" })).toBeVisible();
  await reopened.getByRole("button", { name: "보내기", exact: true }).click();
  const goal = reopened.locator(".goal-indicator");
  await expect(goal).toHaveCount(1);
  await expect(goal).toHaveAttribute("data-status", "active");
  await goal.locator("summary").click();
  await expect(goal.locator(".goal-details")).toContainText(draft);
  await expect(restored).toHaveValue("");
  await restored.fill("다음에 실행할 대기 요청");
  await expect(reopened.getByRole("button", { name: "큐에 추가", exact: true })).toHaveAttribute("aria-keyshortcuts", "Control+Enter");
  await restored.press("Shift+Enter");
  await expect(restored).toHaveValue("다음에 실행할 대기 요청\n");
  await restored.fill("다음에 실행할 대기 요청");
  await restored.press("Meta+Enter");
  await expect(restored).toHaveValue("다음에 실행할 대기 요청");
  await restored.press("Control+Enter");
  const queue = reopened.locator(".queue-preview");
  await expect(queue.locator("summary")).toContainText("대기 중인 입력 1개");
  await expect(queue.locator("summary")).toContainText("다음에 실행할 대기 요청");
  await queue.locator("summary").click();
  await expect(queue.locator(".queue-entry")).toContainText("다음에 실행할 대기 요청");
  await goal.getByRole("button", { name: "일시 정지", exact: true }).click();
  await expect(goal).toHaveAttribute("data-status", "paused");
  await goal.getByRole("button", { name: "목표 재개", exact: true }).click();
  await expect(goal).toHaveAttribute("data-status", "active");
  await goal.getByRole("button", { name: "목표 삭제", exact: true }).click();
  const deleteGoal = reopened.getByRole("dialog", { name: "목표 삭제", exact: true });
  await deleteGoal.getByRole("button", { name: "취소", exact: true }).click();
  await expect(goal).toHaveCount(1);
  await goal.getByRole("button", { name: "목표 삭제", exact: true }).click();
  await deleteGoal.getByRole("button", { name: "목표 삭제", exact: true }).click();
  await expect(goal).toHaveCount(0);
  await expect(reopened.getByText("페이지 이력 26", { exact: true })).toHaveCount(1);
});
