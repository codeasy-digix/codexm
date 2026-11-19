import { expect, test, type Page } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { readFileSync, realpathSync } from "node:fs";
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
  await expect(first).toHaveCSS("background-color", "rgb(230, 241, 233)");
  await expect(other).toHaveCSS("background-color", "rgba(0, 0, 0, 0)");
  await expect(page.locator(".user-message p").last()).toHaveCSS("background-color", "rgb(242, 247, 244)");
  await expect(page.locator(".user-message p").last()).toHaveCSS("border-top-width", "1px");
  const header = page.locator(".chat-header");
  const [account, limits, model] = await Promise.all([header.getByLabel("이번에 사용할 계정").boundingBox(),
    header.getByRole("group", { name: "선택 계정의 남은 한도" }).boundingBox(),
    header.getByRole("combobox", { name: "모델", exact: true }).boundingBox()]);
  expect(account).toBeTruthy(); expect(limits).toBeTruthy(); expect(model).toBeTruthy();
  expect(limits!.y + limits!.height).toBeCloseTo(account!.y + account!.height, 0);
  expect(model!.y).toBeCloseTo(account!.y, 0);

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
  await expect(first).toHaveCSS("background-color", "rgb(46, 69, 56)");
  await expect(other).toHaveCSS("background-color", "rgba(0, 0, 0, 0)");
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

  await other.getByRole("link").click();
  await expect(other).toHaveClass(/is-current/);
  await expect(first).toHaveCSS("background-color", "rgba(0, 0, 0, 0)");
  await expect(input).toHaveValue("");
  await first.getByRole("link").click();
  await expect(input).toHaveValue(draft);
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
});
