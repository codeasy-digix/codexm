import { test,expect,type BrowserContext,type Page } from "@playwright/test";
import { existsSync, readFileSync, realpathSync, writeFileSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { createECDH, randomBytes } from "node:crypto";
import { join } from "node:path";
import { MongoClient } from "mongodb";
import type { ChatHistoryPage } from "../imports/contracts/web";

const fixtureRoot=process.env.CODEXM_CHAT_GUI_FIXTURE;
const binary=process.env.CODEXM_CHAT_GUI_BINARY;
test.skip(!fixtureRoot||!binary,"requires the isolated official-runtime browser fixture");
const url=process.env.CODEXM_WEB_TEST_URL||"http://127.0.0.1:15431";
function fixture(){return JSON.parse(readFileSync(join(fixtureRoot!,"fixture.json"),"utf8"));}
let sessionCookies: Awaited<ReturnType<BrowserContext["cookies"]>> | undefined;

async function authenticate(page:Page, fresh = false){
  if (!fresh && sessionCookies) {
    await page.context().addCookies(sessionCookies);
    await page.goto(url);
    await expect(page.getByRole("button",{name:"새 채팅",exact:true})).toBeAttached();
    return;
  }
  const f=fixture();
  execFileSync(binary!,["--config",f.config,"web","--no-open"],{stdio:"pipe",env:{...process.env,HOME:f.home}});
  await page.goto(url);
  await expect(page.getByRole("alert")).toHaveText("로그인이 필요합니다.",{timeout:12000});
  await page.getByLabel("로컬 접속 코드",{exact:true}).fill(readFileSync(join(f.state,"access"),"utf8").trim());
  await page.getByRole("button",{name:"연결",exact:true}).click();
  await expect(page.getByRole("button",{name:"새 채팅",exact:true})).toBeAttached();
  if (!fresh) sessionCookies = await page.context().cookies(url);
}

async function menu(page:Page){if(await page.getByRole("button",{name:"메뉴 열기",exact:true}).isVisible())await page.getByRole("button",{name:"메뉴 열기",exact:true}).click();}

test("the sidebar confirms GUI quit each time and the account dropdown marks the default", async ({ page }) => {
  await authenticate(page);
  // Use a synthetic preview with an invalid native ticket. This fixture never
  // requests a real GUI shutdown, even if the client shim fails.
  await page.evaluate(() => {
    const m = (window as any).Meteor;
    const apply = m.apply;
    (window as any).__quitConfirmations = [];
    m.apply = function (method: string, args: any[], options: any, callback: any) {
      if (method !== "codexm.call") return apply.call(this, method, args, options, callback);
      if (args[0] === "web.guiQuitPreview") {
        queueMicrotask(() => callback(null, { application: "Isolated Desktop Fixture", confirmation: "invalid-fixture-ticket" }));
        return;
      }
      if (args[0] === "web.guiQuit") {
        (window as any).__quitConfirmations.push(args[1].confirmation);
        queueMicrotask(() => callback(null, true));
        return;
      }
      if (args[0] === "web.catalog") {
        return apply.call(this, method, args, options, (error: any, result: any) => callback(error, error ? result : {
          ...result, defaultAccount: "first-fixture", guiQuitAvailable: true,
          accounts: [{ name: "first-fixture", default: true }, { name: "second-fixture" }],
        }));
      }
      return apply.call(this, method, args, options, callback);
    };
  });
  await page.getByRole("textbox", {name: "채팅 검색", exact: true}).fill("긴 이력");
  const header = page.locator(".chat-header");
  const account = header.getByRole("combobox", {name: "이번에 사용할 계정", exact: true});
  const defaultOption = account.locator('option[value="default"]');
  await expect(defaultOption).toHaveText("first-fixture (기본)");
  await expect(account).toHaveAttribute("title", "first-fixture (기본)");
  await account.selectOption("second-fixture");
  await expect(defaultOption).toHaveText("first-fixture (기본)");
  await expect(account.locator("option:checked")).toHaveText("second-fixture");
  await expect(header).not.toContainText("기본 계정 · first-fixture");
  const quit = page.locator(".sidebar .brand").getByRole("button", {name: "GUI 종료", exact: true});
  await expect(header.getByRole("button", {name: "GUI 종료", exact: true})).toHaveCount(0);
  await quit.click();
  const dialog = page.getByRole("dialog", {name: "Codex GUI 전체 종료"});
  await expect(dialog).toContainText("모든 작업이 중단됩니다");
  await expect(dialog).toContainText("Isolated Desktop Fixture");
  const cancel = dialog.getByRole("button", {name: "취소", exact: true});
  await expect(cancel).toBeFocused();
  await cancel.click();
  expect(await page.evaluate(() => (window as any).__quitConfirmations)).toEqual([]);
  await quit.click();
  await expect(dialog).toContainText("Isolated Desktop Fixture");
  await dialog.screenshot({path: join(fixtureRoot!, "confirm-gui-quit.png")});
  await dialog.getByRole("button", {name: "GUI 전체 종료", exact: true}).click();
  await expect(page.getByRole("status").filter({hasText: "Codex GUI를 종료했습니다"})).toBeVisible();
  expect(await page.evaluate(() => (window as any).__quitConfirmations)).toEqual(["invalid-fixture-ticket"]);
  await expect(account).toHaveValue("second-fixture");
});

test("Send preserves accepted relay text across reload and keeps the explicit queue shortcut", async ({ page }) => {
  await authenticate(page);
  const thread = fixture().second;
  const original = "현재 GUI 작업에 전달할 원문\n<문자 그대로> `echo fixture`";
  await page.evaluate(thread => {
    const m = (window as any).Meteor;
    const apply = m.apply;
    const sent: string[] = [];
    const state = (window as any).__relayFixture = { sent, status: "pending", idle: false };
    m.apply = function (method: string, args: any[], options: any, callback: any) {
      if (method !== "codexm.call" || args[1]?.thread_id !== thread) return apply.call(this, method, args, options, callback);
      if (args[0] === "web.history") {
        return apply.call(this, method, args, options, (error: any, result: any) => callback(error, error ? result : {
          ...result, thread: { ...result.thread, externalOwner: true }, delivery: { state: "relay" },
        }));
      }
      if (args[0] === "web.send") {
        sent.push(args[1].mode);
        const response = args[1].mode === "queue" || state.idle ? { mode: "queue" } : { mode: "relay", relay_id: "fake-relay-receipt" };
        queueMicrotask(() => callback(null, response));
        return;
      }
      if (args[0] === "web.guiRelayStatus") {
        queueMicrotask(() => callback(null, { status: state.status }));
        return;
      }
      return apply.call(this, method, args, options, callback);
    };
  }, thread);
  await page.getByRole("link", {name:/진행 중 개입 테스트/}).click();
  await expect(page.getByText("GUI 채팅에 해당 창의 계정으로 전달합니다.")).toBeVisible();
  const input = page.getByLabel("메시지", {exact:true});
  await input.fill(original);
  await input.press("Enter");
  await expect(page.getByText("GUI 채팅에 전달하고 있습니다…", {exact:true})).toBeVisible();
  await expect(input).toHaveValue(original);
  await input.press("Enter");
  expect(await page.evaluate(() => (window as any).__relayFixture.sent)).toEqual(["immediate"]);
  const displayed = page.locator(".user-message p").filter({ hasText: "현재 GUI 작업에 전달할 원문" });
  await expect(displayed).toHaveCount(0);
  writeFileSync(join(fixtureRoot!, "accept-synthetic-relay"), "accepted fixture");
  await expect.poll(() => existsSync(join(fixtureRoot!, "synthetic-relay-accepted"))).toBe(true);
  await page.evaluate(() => { (window as any).__relayFixture.status = "accepted"; });
  await expect(page.getByText("GUI 채팅에 전달했습니다.", {exact:true})).toBeVisible();
  await expect(input).toHaveValue("");
  await expect(displayed).toHaveCount(1);
  expect(await displayed.textContent()).toBe(original);
  await input.fill("다음 작업에 넣을 입력");
  await input.press("ControlOrMeta+Enter");
  await expect(page.getByText("큐에 추가했습니다.", {exact:true})).toBeVisible();
  expect(await page.evaluate(() => (window as any).__relayFixture.sent)).toEqual(["immediate", "queue"]);
  await page.evaluate(() => { (window as any).__relayFixture.idle = true; });
  await input.fill("쉬고 있는 GUI 채팅의 요청");
  await page.getByRole("button", {name: "보내기", exact: true}).click();
  await expect(page.getByText("새 작업 요청을 전달했습니다.", {exact:true})).toBeVisible();
  expect(await page.evaluate(() => (window as any).__relayFixture.sent)).toEqual(["immediate", "queue", "immediate"]);
  await page.reload();
  await expect(displayed).toHaveCount(1);
  expect(await displayed.textContent()).toBe(original);
});

test("mobile PWA loads icons and registers completion alerts for this browser", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const request = await page.request.post(`${url}/auth/push`, {
    headers: { Origin: url, "Content-Type": "application/json" }, data: { action: "status" },
  });
  expect(request.status()).toBe(401);
  const manifest = await (await page.request.get(`${url}/manifest.webmanifest`)).json();
  expect(manifest.display).toBe("standalone");
  for (const icon of manifest.icons) {
    const response = await page.request.get(`${url}${icon.src}`);
    expect(response.ok()).toBe(true);
    expect(response.headers()["content-type"]).toContain("image/png");
  }
  const keys = createECDH("prime256v1");
  keys.generateKeys();
  const device = { endpoint: "https://fcm.googleapis.com/fcm/send/synthetic-browser-endpoint",
    keys: { p256dh: keys.getPublicKey().toString("base64url"), auth: randomBytes(16).toString("base64url") } };
  await page.addInitScript(device => {
    let subscription: { endpoint: string; toJSON: () => object; unsubscribe: () => Promise<boolean> } | null = null;
    PushManager.prototype.getSubscription = async () => subscription as PushSubscription | null;
    PushManager.prototype.subscribe = async options => {
      if (!(options?.applicationServerKey instanceof Uint8Array) || options.applicationServerKey.length !== 65) throw new Error("VAPID key missing");
      subscription = { endpoint: device.endpoint, toJSON: () => device, unsubscribe: async () => { subscription = null; return true; } };
      return subscription as PushSubscription;
    };
  }, device);
  let requestedTest = false;
  await page.route("**/auth/push", async route => {
    const body = route.request().postDataJSON();
    if (body?.action !== "test") return route.continue();
    requestedTest = body.endpoint === device.endpoint;
    // No network delivery is made to the synthetic browser endpoint.
    return route.fulfill({ json: { accepted: true } });
  });
  await authenticate(page);
  await menu(page);
  await page.getByRole("button", { name: "설정", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "설정", exact: true });
  await dialog.getByRole("button", { name: "알림", exact: true }).click();
  await dialog.getByRole("button", { name: "이 기기에서 알림 켜기" }).click();
  await expect(dialog.getByRole("status")).toHaveText("이 기기에서 완료 알림을 받습니다.");
  const worker = await page.evaluate(async () => (await navigator.serviceWorker.ready).active?.scriptURL);
  expect(worker).toBe(`${url}/sw.js`);
  await dialog.getByRole("button", { name: "테스트 알림" }).click();
  await expect(dialog.getByRole("status")).toHaveText("테스트 알림을 보냈습니다.");
  expect(requestedTest).toBe(true);
  await dialog.getByRole("button", { name: "알림 끄기" }).click();
  await expect(dialog.getByRole("status")).toHaveText("이 기기의 완료 알림을 껐습니다.");
});

test("recent-only history, anchored upward pagination, safe Markdown and lazy details",async({page})=>{
  await authenticate(page);
  await page.evaluate(()=>{
    const m=(window as any).Meteor,apply=m.apply;
    (window as any).__calls=[];
    m.apply=function(...args:any[]){if(args[0]==="codexm.call")(window as any).__calls.push(args[1][0]);return apply.apply(this,args);};
  });
  await page.getByRole("link",{name:/긴 이력 페이지 테스트/}).click();
  await expect(page.locator(".turn")).toHaveCount(8);
  await expect(page.getByText("페이지 이력 26",{exact:true})).toBeVisible();
  await expect(page.getByText("페이지 이력 01",{exact:true})).toHaveCount(0);
  const table = page.locator(".turn").last().getByRole("table");
  await expect(table.getByRole("row")).toHaveCount(3);
  await expect(table.getByRole("columnheader", { name: "항목", exact: true })).toBeVisible();
  await expect(table.getByRole("cell", { name: "12", exact: true })).toHaveCSS("text-align", "right");
  await table.screenshot({ path: join(fixtureRoot!, "markdown-table.png") });
  expect(await page.evaluate(()=>(window as any).__unsafe)).toBeUndefined();
  expect(await page.evaluate(()=>(window as any).__calls.filter((m:string)=>m==="web.items").length)).toBe(0);
  await page.locator(".turn-details").last().locator("summary").click();
  await expect(page.getByText("별도 작업 과정이 없습니다.")).toBeVisible();
  expect(await page.evaluate(()=>(window as any).__calls.filter((m:string)=>m==="web.items").length)).toBe(1);
  const scroll=page.getByLabel("대화 내용",{exact:true});
  await scroll.evaluate(el=>{el.scrollTop=0;});
  await expect(page.locator(".turn")).toHaveCount(16);
  expect(await scroll.evaluate(el=>el.scrollTop)).toBeGreaterThan(200);
  await scroll.evaluate(el=>{el.scrollTop=0;});
  await expect(page.locator(".turn")).toHaveCount(24);
  await scroll.evaluate(el=>{el.scrollTop=0;});
  await expect(page.locator(".turn")).toHaveCount(26);
  await scroll.evaluate(el=>{el.scrollTop=0;});
  await expect(page.getByText("페이지 이력 01",{exact:true})).toBeVisible();
  await expect(page.getByRole("button",{name:"이전 메시지 불러오기",exact:true})).toHaveCount(0);
  await page.screenshot({path:join(fixtureRoot!,"desktop-history.png")});
});

test("MongoDB history renders while the native refresh is pending", async ({ page }) => {
  test.skip(process.env.CODEXM_WEB_TEST_MONGO !== "1", "requires the isolated MongoDB fixture");
  await page.goto(url);
  await expect(page.getByRole("alert")).toHaveText("로그인이 필요합니다.");
  const thread = fixture().thread;
  const denied = await page.evaluate(id => new Promise(resolve => {
    (window as any).Meteor.apply("codexm.cachedHistory", [{ thread_id: id }], { noRetry: true },
      (error: any) => resolve(error?.error));
  }), thread);
  expect(denied).toBe("authentication_required");
  await authenticate(page);
  await page.evaluate(id => new Promise((resolve, reject) => {
    (window as any).Meteor.apply("codexm.call", ["web.history", { thread_id: id }, ""], { noRetry: true },
      (error: unknown, value: unknown) => error ? reject(error) : resolve(value));
  }), thread);
  await page.evaluate(() => {
    const m = (window as any).Meteor, original = m.apply;
    m.apply = function (name: string, args: unknown[], options: unknown, callback: unknown) {
      if (name === "codexm.call" && args[0] === "web.history") {
        (window as any).__releaseHistory = () => {
          m.apply = original;
          return original.call(this, name, args, options, callback);
        };
        return;
      }
      return original.call(this, name, args, options, callback);
    };
  });
  try {
    await page.getByRole("link", { name: /긴 이력 페이지 테스트/ }).click();
    await expect(page.getByText("페이지 이력 26", { exact: true })).toBeVisible();
    await expect(page.locator(".turn")).toHaveCount(8);
    await expect.poll(() => page.evaluate(() => typeof (window as any).__releaseHistory)).toBe("function");
  } finally {
    await page.evaluate(() => { (window as any).__releaseHistory?.(); });
  }
});

test("MongoDB catches up all native turns after the browser closes and retains old records", async ({ context }) => {
  test.skip(process.env.CODEXM_WEB_TEST_MONGO !== "1", "requires the isolated MongoDB fixture");
  const f = fixture();
  const socket = realpathSync(join(f.state, "history-cache", "mongodb.sock"));
  const mongo = new MongoClient("mongodb://" + encodeURIComponent(socket) + "/codexm", { directConnection: true });
  try {
    const db = mongo.db();
    const checkpoints = db.collection<{ _id: string; revision: string; nextCursor: string | null }>("history_progress_v1");
    const pages = db.collection<{ _id: string; thread?: string; revision?: string; format?: number; page?: ChatHistoryPage; expiresAt?: Date }>("history_pages_v1");
    await expect.poll(async () => (await checkpoints.findOne({ _id: f.catchup }))?.nextCursor, { timeout: 25_000 }).toBeNull();
    const previous = (await checkpoints.findOne({ _id: f.catchup }))?.revision;
    const before = await context.newPage();
    await authenticate(before);
    await before.getByRole("link", { name: /오프라인 갱신 테스트/ }).click();
    await expect(before.getByText("브라우저가 열리기 전 이력", { exact: true })).toBeVisible();
    await before.close();
    writeFileSync(join(fixtureRoot!, "append-offline-history"), "append through original official app-server");
    await expect.poll(() => existsSync(join(fixtureRoot!, "appended-offline-history")), { timeout: 15_000 }).toBe(true);
    const saved = () => checkpoints.findOne({ _id: f.catchup, revision: { $ne: previous }, nextCursor: null });
    await expect.poll(async () => !!await saved(), { timeout: 30_000 }).toBe(true);
    const revision = (await saved())?.revision;
    const chunks = await pages.find({ thread: f.catchup, revision, format: 2 }).toArray();
    const turns = chunks.flatMap(chunk => chunk.page?.turns ?? []);
    expect(new Set(turns.map(turn => turn.id)).size).toBe(19);
    for (let i = 1; i <= 18; i++) {
      expect(JSON.stringify(turns)).toContain("웹을 닫은 동안 추가된 이력 " + String(i).padStart(2, "0"));
    }
    const legacy = await pages.findOne({ _id: "older-cache-fixture" });
    expect(legacy).not.toBeNull();
    expect(legacy?.expiresAt).toBeUndefined();
    expect((await pages.indexes()).some(index => index.expireAfterSeconds !== undefined)).toBe(false);
    const after = await context.newPage();
    await authenticate(after);
    await after.getByRole("link", { name: /오프라인 갱신 테스트/ }).click();
    await expect(after.getByText("웹을 닫은 동안 추가된 이력 18", { exact: true })).toBeVisible();
    const older = after.getByRole("button", { name: "이전 메시지 불러오기", exact: true });
    await older.click();
    await expect(after.locator(".turn")).toHaveCount(16);
    await older.click();
    await expect(after.locator(".turn")).toHaveCount(19);
    await expect(after.getByText("브라우저가 열리기 전 이력", { exact: true })).toBeVisible();
    await after.close();
  } finally {
    await mongo.close();
  }
});

test("project picker, idle send, Enter intervention and the queue shortcut for a separate native writer", async ({page}) => {
  const row = page.getByRole("link",{name:/진행 중 개입 테스트/});
  const input = page.getByLabel("메시지", {exact: true});
  const send = page.getByRole("button", {name: "보내기", exact: true});
  const originalTurn = page.locator("article.turn").filter({
    has: page.getByText("GUI_FIXTURE_ACTIVE_FOREIGN_BROWSER", {exact: true}),
  });
  try {
    await authenticate(page);
    const picker = page.getByRole("combobox", {name:"프로젝트 필터",exact:true});
    await expect(picker).toHaveValue("");
    await picker.selectOption(fixture().project);
    await expect(page.locator(".list-heading")).toContainText("브라우저 테스트 프로젝트 채팅");
    await expect(page.getByRole("link",{name:/긴 이력 페이지 테스트/})).toBeVisible();
    await expect(row).toHaveCount(0);
    await picker.selectOption("");
    await expect(row).toContainText("다른 창 · 작업 중",{timeout:12000});
    await row.click();
    await expect(page.getByRole("status",{name:"현재 작업 상태",exact:true})).toHaveText("다른 창 · 작업 중");
    await expect(page.getByText("외부 작업이 기록한 진행 안내",{exact:true})).toBeVisible();
    const progress = originalTurn.locator(".commentary-message").getByText("작업 도중 새로 저장된 진행 안내", {exact: true});
    await expect(progress).toHaveCount(0);
    writeFileSync(join(fixtureRoot!,"show-foreign-progress"),"send the second fixture progress message");
    await expect(progress).toBeVisible({timeout: 7000});
    await expect(page.getByRole("status",{name:"현재 작업 상태",exact:true})).toHaveText("다른 창 · 작업 중");
    await expect(page.locator(".turn-details[open]")).toHaveCount(0);
    await progress.screenshot({path: join(fixtureRoot!, "live-commentary.png")});
    await input.fill("외부에서 진행 중인 턴을 수정합니다");
    await expect(send).toBeEnabled();
    await input.press("Enter");
    await expect(page.getByText("현재 작업에 전달했습니다.", {exact: true})).toBeVisible();
    await expect(page.locator(".queue-preview")).toHaveCount(0);
    await input.fill("외부 작업이 끝난 뒤 실행합니다");
    await input.press("ControlOrMeta+Enter");
    await expect(page.getByText("큐에 추가했습니다.", {exact: true})).toBeVisible();
    await expect(page.locator(".queue-preview summary")).toContainText("1개");
    await page.locator(".composer-wrap").screenshot({path: join(fixtureRoot!, "external-delivery.png")});
  } finally {
    writeFileSync(join(fixtureRoot!,"release-foreign"),"release isolated external writer");
  }
  await expect(row).toContainText("다른 창에서 열림",{timeout:12000});
  await expect(page.getByText("외부에서 진행 중인 턴을 수정합니다", {exact: true})).toBeVisible();
  await expect(page.getByText("외부 작업이 끝난 뒤 실행합니다", {exact: true})).toBeVisible();
  const chatURL = page.url();
  const idleMessage = "대기 중인 채팅에서 새 작업을 시작합니다";
  await input.fill(idleMessage);
  await expect(send).toBeEnabled();
  await send.click();
  await expect(page.getByText("작업을 시작했습니다.", {exact: true})).toBeVisible();
  await expect(page.getByText(idleMessage, {exact: true})).toBeVisible();
  await expect(page).toHaveURL(chatURL);
  await page.reload();
  await expect(originalTurn.locator(".commentary-message").getByText("외부 작업이 기록한 진행 안내", {exact: true})).toHaveCount(1);
  await expect(originalTurn.locator(".commentary-message").getByText("작업 도중 새로 저장된 진행 안내", {exact: true})).toHaveCount(1);
});

test("a pending Codex message can be edited, recalled and requeued with its new text", async ({ page }) => {
  await authenticate(page);
  await page.getByRole("link", { name: /대기 메시지 관리 테스트/ }).click();
  const original = "큐에서 수정할 원래 요청";
  const edited = "대기 순서를 유지하며 수정한 요청";
  const requeued = "대기 취소한 메시지를 편집해서 다시 등록합니다";
  const input = page.getByLabel("메시지", { exact: true });
  const queue = page.locator(".queue-preview");
  await expect(queue.locator("summary")).toContainText("1개");
  await queue.locator("summary").click();
  await queue.locator(".queue-entry").filter({ hasText: original })
    .getByRole("button", { name: "수정", exact: true }).click();
  const edit = page.getByLabel("대기 메시지 수정", { exact: true });
  await expect(edit).toHaveValue(original);
  await edit.fill(edited);
  await page.getByRole("button", { name: "수정 저장", exact: true }).click();
  const editedMessage = queue.locator(".queue-entry").filter({ hasText: edited });
  await expect(editedMessage).toBeVisible();
  await input.fill("별도로 작성 중인 입력");
  await editedMessage.getByRole("button", { name: "대기 취소", exact: true }).click();
  const recalled = page.getByLabel("큐에서 뺀 메시지", { exact: true });
  await expect(recalled).toHaveValue(edited);
  await expect(input).toHaveValue("별도로 작성 중인 입력");
  await expect(queue).toHaveCount(0);
  await recalled.fill(requeued);
  await page.reload();
  await expect(recalled).toHaveValue(requeued);
  await page.getByRole("button", { name: "다시 큐에 추가", exact: true }).click();
  await expect(recalled).toHaveCount(0);
  const started = page.locator(".turn .user-message").filter({ hasText: requeued });
  const waiting = queue.locator(".queue-entry").filter({ hasText: requeued });
  await expect(started.or(waiting).first()).toBeAttached();
  await expect(page.locator(".turn .user-message").filter({ hasText: original })).toHaveCount(0);
  await expect(page.locator(".turn .user-message").filter({ hasText: edited })).toHaveCount(0);
});

test("an unconfirmed requeue preserves the draft across reload without resending it", async ({ page }) => {
  await authenticate(page);
  const original = "재등록 응답을 기다리던 초안";
  await page.evaluate(({ thread, original }) => {
    const m = (window as any).Meteor;
    const apply = m.apply;
    let withdrawn = false;
    m.apply = function (method: string, args: any[], options: any, callback: any) {
      if (method !== "codexm.call" || args[1]?.thread_id !== thread) return apply.call(this, method, args, options, callback);
      if (args[0] === "web.history") {
        return apply.call(this, method, args, options, (error: any, result: any) => callback(error, error ? result : {
          ...result, queue: withdrawn ? [] : [{ id: "fixture-queue-id", input: [{ type: "text", text: original }] }],
        }));
      }
      if (args[0] === "web.queueCancel") {
        withdrawn = true;
        queueMicrotask(() => callback(null, { deleted: true }));
        return;
      }
      if (args[0] === "web.send") {
        queueMicrotask(() => callback(new Error("응답을 확인하지 못했습니다.")));
        return;
      }
      return apply.call(this, method, args, options, callback);
    };
  }, { thread: fixture().thread, original });
  await page.getByRole("link", { name: /긴 이력 페이지 테스트/ }).click();
  await page.locator(".queue-preview summary").click();
  await page.getByRole("button", { name: "대기 취소", exact: true }).click();
  await expect(page.getByLabel("큐에서 뺀 메시지", { exact: true })).toHaveValue(original);
  const requeue = page.getByRole("button", { name: "다시 큐에 추가", exact: true });
  await requeue.click();
  await expect(page.getByRole("alert").filter({ hasText: "응답을 확인하지 못했습니다." })).toBeVisible();
  await page.reload();
  await expect(page.getByLabel("보관된 입력 초안", { exact: true })).toHaveValue(original);
  await expect(requeue).toBeDisabled();
  await page.getByRole("button", { name: "초안 버리기", exact: true }).click();
  await expect(requeue).toHaveCount(0);
});

test("chat URLs restore the same chat after session check and browser navigation", async ({ page }) => {
  await authenticate(page);
  const first = page.getByRole("link", { name: /긴 이력 페이지 테스트/ });
  const firstPath = `/chat/${fixture().thread}`;
  await expect(first).toHaveAttribute("href", firstPath);
  await first.click();
  await expect(page).toHaveURL(url + firstPath);
  await expect(first).toHaveAttribute("aria-current", "page");
  await expect(page.getByRole("group", { name: "선택 계정의 남은 한도" })).toBeVisible();

  let releaseTicket = () => {};
  const gate = new Promise<void>(resolve => { releaseTicket = resolve; });
  await page.route("**/auth/ticket", async route => {
    await gate;
    await route.continue();
  }, { times: 1 });
  try {
    await page.reload();
    await expect(page.getByText("로그인 상태를 확인하고 있습니다…", { exact: true })).toBeVisible();
    await expect(page.getByLabel("로컬 접속 코드", { exact: true })).toHaveCount(0);
    await expect(page.getByLabel("비밀번호", { exact: true })).toHaveCount(0);
  } finally {
    releaseTicket();
  }
  await expect(page.locator("main.conversation")).toHaveAttribute("data-thread-id", fixture().thread);
  await expect(page.getByRole("heading", { name: "긴 이력 페이지 테스트", exact: true })).toBeVisible();
  const second = page.getByRole("link", { name: /진행 중 개입 테스트/ });
  await second.click();
  await expect(page).toHaveURL(`${url}/chat/${fixture().second}`);
  await page.goBack();
  await expect(page.locator("main.conversation")).toHaveAttribute("data-thread-id", fixture().thread);
  await page.goForward();
  await expect(page.locator("main.conversation")).toHaveAttribute("data-thread-id", fixture().second);

  await page.goto(`${url}/chat/${fixture().archived}`);
  await expect(page.getByRole("heading", { name: "보관된 fixture 채팅", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "보관함", exact: true })).toHaveAttribute("aria-pressed", "true");
  await expect(page.getByLabel("메시지", { exact: true })).toHaveCount(0);
});

test("streamed answer, intervention, queue, browser reconnect and default login renewal",async({page,context})=>{
  await authenticate(page);
  await page.getByRole("button",{name:"새 채팅",exact:true}).click();
  const dialog=page.getByRole("dialog",{name:"새 채팅",exact:true});
  await dialog.getByLabel("프로젝트",{exact:true}).selectOption(fixture().project);
  await dialog.getByRole("button",{name:"채팅 만들기",exact:true}).click();
  await expect(page.getByRole("heading", { name: "표시할 메시지가 없습니다" })).toBeVisible({timeout:20000});
  const input=page.getByLabel("메시지",{exact:true});
  await input.fill("GUI_FIXTURE_ACTIVE_BROWSER");
  await input.dispatchEvent("keydown",{key:"Enter",keyCode:229,isComposing:true});
  await expect(input).toHaveValue("GUI_FIXTURE_ACTIVE_BROWSER");
  await input.press("Enter");
  const workStatus=page.getByRole("status",{name:"현재 작업 상태",exact:true});
  await expect(workStatus).toHaveText("응답 생성 중",{timeout:12000});
  await expect(page.locator(".live-message")).toContainText("완료 전에도 보이는 답변");
  const id=await page.locator("main.conversation").getAttribute("data-thread-id");
  await input.fill("지금 방향을 바로 수정합니다");await input.press("Enter");
  await expect(page.getByText("현재 작업에 전달했습니다.",{exact:true})).toBeVisible({timeout:20000});
  await input.fill("다음 단계는 큐로 진행합니다");
  await input.press("End");
  await input.press("Shift+Enter");
  await expect(input).toHaveValue("다음 단계는 큐로 진행합니다\n");
  await expect(page.locator(".queue-preview")).toHaveCount(0);
  await input.fill("다음 단계는 큐로 진행합니다");
  await input.press("ControlOrMeta+Enter");
  await expect(page.getByText("큐에 추가했습니다.",{exact:true})).toBeVisible();
  await expect(page.locator(".queue-preview summary")).toContainText("1개");
  const renewal = "기존 작업과 대기 입력이 끝나면 변경된 웹 계정이 자동 적용됩니다.";
  // Switch the default link between valid synthetic logins. Replacing the
  // owner's credentials with an invalid file revokes its native permissions.
  const config = fixture();
  const reply = execFileSync(binary!, ["--config", config.config, "bridge", "--stdio"], {
    env: { ...process.env, HOME: config.home }, encoding: "utf8",
    input: JSON.stringify({ protocolVersion: 1, id: 1, owner: "browser-fixture", operation_id: "change-fixture-default", method: "accounts.setDefault", params: { account: "second-browser-login" } }) + "\n",
  });
  expect(JSON.parse(reply).error).toBeUndefined();
  await expect(page.getByText(renewal, {exact: true})).toBeVisible();
  await page.close();
  const next=await context.newPage();await next.goto(url);
  await next.getByRole("link",{name:/GUI_FIXTURE_ACTIVE_BROWSER/}).click();
  await expect(next.locator("main.conversation")).toHaveAttribute("data-thread-id",id!);
  const nextStatus=next.getByRole("status",{name:"현재 작업 상태",exact:true});
  await expect(nextStatus).toHaveText(/작업 중|응답 생성 중/);
  await expect(next.getByText(renewal, {exact: true})).toBeVisible();
  writeFileSync(join(fixtureRoot!,"release-model"),"release fixture only");
  await expect(nextStatus).toHaveText("작업 완료",{timeout:20000});
  await expect(next.locator(".turn .user-message").getByText("다음 단계는 큐로 진행합니다",{exact:true})).toBeVisible();
  await expect(next.getByText(renewal, {exact: true})).toHaveCount(0, {timeout: 10000});
  await next.screenshot({path:join(fixtureRoot!,"desktop-conversation.png")});
});

test("native questions appear above the composer and submit the selected or typed answers", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await authenticate(page);
  await menu(page);
  await page.getByRole("button", { name: "새 채팅", exact: true }).click();
  await page.getByRole("button", { name: "채팅 만들기", exact: true }).click();
  const message = page.getByRole("textbox", { name: "메시지", exact: true });
  await message.fill("WEB_QUESTION_FIXTURE");
  await message.press("Enter");
  const card = page.getByRole("region", { name: "추가 확인" });
  await expect(card).toBeVisible();
  await expect(card.getByText("어떤 방식으로 진행할까요?")).toBeVisible();
  await expect(card.getByText("기본 순서대로 진행합니다.")).toBeVisible();
  const answer = card.getByRole("button", { name: "답변 전달" });
  await expect(answer).toBeDisabled();
  await card.getByRole("group", { name: "진행 방식" }).getByText("직접 입력", { exact: true }).click();
  await card.getByLabel("진행 방식 답변").fill("fixture-own-choice");
  await card.getByRole("group", { name: "확인 값" }).getByText("직접 입력", { exact: true }).click();
  const secret = card.getByLabel("확인 값 답변");
  await expect(secret).toHaveAttribute("type", "password");
  await secret.fill("fixture-confirmed-answer");
  await expect(answer).toBeEnabled();
  const chat = page.url();
  await answer.click();
  await expect(card).toHaveCount(0);
  await expect(page.getByText("웹에서 보낸 답변을 받았습니다.", { exact: true })).toBeVisible();
  await expect(page).toHaveURL(chat);
});

test("header model controls and editable keyboard combinations", async ({ page }) => {
  await authenticate(page);
  await page.getByRole("link", { name: /긴 이력 페이지 테스트/ }).click();
  const header = page.locator(".chat-header");
  const model = header.getByRole("combobox", { name: "모델", exact: true });
  const effort = header.getByRole("combobox", { name: "Reasoning effort", exact: true });
  await expect(header.getByRole("combobox", { name: "이번에 사용할 계정", exact: true })).toBeVisible();
  await expect(model).toBeEnabled({timeout:20000});
  await expect(page.locator(".composer select")).toHaveCount(0);
  const input = page.getByLabel("메시지", { exact: true });
  await input.press("Meta+1");
  await expect(model).toHaveValue("");
  const hasAstra = await model.locator('option[value="gpt-6.2-astra"]').count();
  await input.press("Meta+Shift+2");
  if (hasAstra) await expect(model).toHaveValue("gpt-6.2-astra");
  else await expect(header.getByRole("alert")).toHaveText("이 계정에서 선택할 수 없는 모델입니다.");
  const choice = (await model.locator("option").nth(1).getAttribute("value"))!;
  await model.selectOption(choice);
  await expect(model).toHaveValue(choice);
  const hasMax = await effort.locator("option[value=max]").count();
  await input.press("Meta+Shift+4");
  if (hasMax) await expect(effort).toHaveValue("max");
  else await expect(header.getByRole("alert")).toHaveText("선택한 모델에서 사용할 수 없는 effort입니다.");
  const supported = await effort.locator("option[value=high]").count();
  const effortValue = supported ? "high" : (await effort.locator("option").nth(1).getAttribute("value"))!;
  await effort.selectOption(effortValue);
  await input.fill("보내지 않은 초안");
  await input.press("Meta+Shift+0");
  await expect(model).toHaveValue("");
  await expect(effort).toHaveValue("");
  await expect(input).toHaveValue("보내지 않은 초안");
  await page.getByRole("button", { name: "설정", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "설정", exact: true });
  await dialog.getByRole("button", { name: "단축키", exact: true }).click();
  const keys = (index: number) => dialog.getByLabel(`단축키 ${index} 키 조합`, { exact: true });
  await expect(keys(1)).toHaveValue("⌘+⇧+1");
  await expect(keys(6)).toHaveValue("⌘+⇧+0");
  await expect(dialog.getByLabel("단축키 6 할당", { exact: true })).toHaveValue("reset");
  await keys(1).press("Meta+Shift+2");
  await expect(dialog.getByRole("alert")).toContainText("이미 할당");
  await keys(1).press("Meta+Enter");
  await expect(keys(1)).toHaveValue("⌘+⇧+1");
  await expect(dialog.getByRole("alert")).toContainText("Enter");
  await keys(1).press("Meta+Shift+k");
  await expect(keys(1)).toHaveValue("⌘+⇧+K");
  await dialog.getByLabel("단축키 1 할당", { exact: true }).selectOption(`model:${choice}`);
  await dialog.getByRole("button", { name: /단축키 추가/ }).click();
  await keys(7).press("Control+Shift+j");
  await dialog.getByLabel("단축키 7 할당", { exact: true }).selectOption(`effort:${effortValue}`);
  await dialog.getByRole("button", { name: "저장", exact: true }).click();
  await model.selectOption("");
  await input.press("Meta+Shift+1");
  await expect(model).toHaveValue("");
  await input.press("Meta+Shift+k");
  await expect(model).toHaveValue(choice);
  await effort.selectOption("");
  await input.press("Control+Shift+j");
  await expect(effort).toHaveValue(effortValue);
  await expect(input).toHaveValue("보내지 않은 초안");
  await page.reload();
  await page.getByRole("button", { name: "설정", exact: true }).click();
  await dialog.getByRole("button", { name: "단축키", exact: true }).click();
  await expect(keys(1)).toHaveValue("⌘+⇧+K");
  await expect(keys(7)).toHaveValue("Ctrl+⇧+J");
  await expect(dialog.getByLabel("단축키 1 할당", { exact: true })).toHaveValue(`model:${choice}`);
  await expect(dialog.getByLabel("단축키 7 할당", { exact: true })).toHaveValue(`effort:${effortValue}`);
  await dialog.getByRole("button", { name: "단축키 7 삭제", exact: true }).click();
  await expect(keys(7)).toHaveCount(0);
  await dialog.getByRole("button", { name: "기본값 복원", exact: true }).click();
  await expect(keys(1)).toHaveValue("⌘+⇧+1");
});

test("long command output is fetched only when its nested accordion opens",async({page})=>{
  await authenticate(page);
  await page.evaluate(()=>{const m=(window as any).Meteor,apply=m.apply;(window as any).__calls=[];m.apply=function(...args:any[]){if(args[0]==="codexm.call")(window as any).__calls.push(args[1][0]);return apply.apply(this,args);};});
  await page.getByRole("link",{name:/긴 명령 출력 검증/}).click();
  await expect(page.locator(".turn")).toHaveCount(1);
  expect(await page.evaluate(()=>(window as any).__calls.filter((m:string)=>m==="web.items"||m==="web.item").length)).toBe(0);
  await page.locator(".turn-details summary").click();
  const command=page.locator(".tool-item").filter({has:page.locator("summary",{hasText:"printf"})});
  await expect(command).toHaveCount(1);
  expect(await page.evaluate(()=>(window as any).__calls.filter((m:string)=>m==="web.item").length)).toBe(0);
  await command.locator("summary").click();
  await expect.poll(async()=>((await command.locator("pre").textContent())||"").length).toBeGreaterThan(40000);
  expect(await page.evaluate(()=>(window as any).__calls.filter((m:string)=>m==="web.item").length)).toBe(1);
});

test("pin, project move, archive, restore and confirmed permanent delete",async({page})=>{
  await authenticate(page);
  await page.getByRole("button",{name:"새 채팅",exact:true}).click();
  let dialog=page.getByRole("dialog",{name:"새 채팅",exact:true});
  await dialog.getByLabel("프로젝트",{exact:true}).selectOption(fixture().project);
  await dialog.getByRole("button",{name:"채팅 만들기",exact:true}).click();
  const input=page.getByLabel("메시지",{exact:true});await input.fill("관리 기능 검증용 채팅");await input.press("Enter");
  await expect(page.getByRole("heading",{name:"작업을 확인했습니다",exact:true})).toBeVisible({timeout:20000});
  const id=await page.locator("main.conversation").getAttribute("data-thread-id");
  const row = page.getByLabel("채팅 목록", { exact: true }).getByRole("group", { name: /관리 기능 검증용 채팅/ });
  await row.getByRole("button",{name:"고정하기",exact:true}).click();
  await expect(page.getByText("채팅을 고정했습니다.")).toBeVisible();
  await page.getByRole("button",{name:"고정",exact:true}).click();await page.getByRole("link",{name:/관리 기능 검증용 채팅/}).click();
  await row.getByRole("button",{name:"프로젝트로 이동",exact:true}).click();
  dialog=page.getByRole("dialog",{name:"프로젝트로 이동",exact:true});await dialog.getByLabel("프로젝트",{exact:true}).selectOption("");await dialog.getByRole("button",{name:"적용",exact:true}).click();
  await expect(page.getByText("프로젝트를 옮겼습니다.")).toBeVisible();
  await row.getByRole("button",{name:"보관하기",exact:true}).click();
  dialog=page.getByRole("dialog",{name:"채팅 보관",exact:true});await dialog.getByRole("button",{name:"취소",exact:true}).click();
  await expect(page.locator("main.conversation")).toHaveAttribute("data-thread-id",id!);
  await row.getByRole("button",{name:"보관하기",exact:true}).click();await page.getByRole("dialog").getByRole("button",{name:"적용",exact:true}).click();
  await page.getByRole("button",{name:"보관함",exact:true}).click();await page.getByRole("link",{name:/관리 기능 검증용 채팅/}).click();
  await expect(page.getByRole("heading",{name:"작업을 확인했습니다",exact:true})).toBeVisible({timeout:20000});
  await expect(input).toHaveCount(0);await row.getByRole("button",{name:"보관 해제",exact:true}).click();await page.getByRole("dialog").getByRole("button",{name:"적용",exact:true}).click();
  await page.getByRole("button",{name:"전체 채팅",exact:true}).click();await page.getByRole("link",{name:/관리 기능 검증용 채팅/}).click();
  await page.getByRole("button",{name:"채팅 메뉴",exact:true}).click();await page.getByRole("menuitem",{name:"영구 삭제",exact:true}).click();
  dialog=page.getByRole("dialog",{name:"채팅 영구 삭제",exact:true});await expect(dialog.getByText(/되돌릴 수 없습니다/)).toBeVisible();await dialog.getByRole("button",{name:"영구 삭제",exact:true}).click();
  await expect(page.getByRole("link",{name:/관리 기능 검증용 채팅/})).toHaveCount(0);
});

test("project creation, filtered new chat, search and logout gate",async({page})=>{
  await authenticate(page, true);
  await page.getByRole("button",{name:"프로젝트 추가",exact:true}).click();
  let dialog=page.getByRole("dialog",{name:"프로젝트 추가",exact:true});
  await dialog.getByLabel("프로젝트 이름",{exact:true}).fill("새 프로젝트 검증");
  await dialog.getByLabel("이 머신의 작업 폴더",{exact:true}).fill(fixture().home);
  await dialog.getByRole("button",{name:"프로젝트 추가",exact:true}).click();
  await expect(page.getByRole("status")).toContainText("프로젝트를 추가했습니다.");
  await page.getByRole("button",{name:"새 채팅",exact:true}).click();
  dialog=page.getByRole("dialog",{name:"새 채팅",exact:true});
  await expect(dialog.getByLabel("프로젝트",{exact:true}).locator("option:checked")).toHaveText("새 프로젝트 검증");
  await dialog.getByRole("button",{name:"채팅 만들기",exact:true}).click();
  const input=page.getByLabel("메시지",{exact:true});await input.fill("프로젝트 검색 검증 메시지");await input.press("Enter");
  await expect(page.getByRole("heading",{name:"작업을 확인했습니다",exact:true})).toBeVisible({timeout:20000});
  const search=page.getByLabel("채팅 검색",{exact:true});await search.fill("프로젝트 검색 검증");
  await expect(page.getByRole("link",{name:/프로젝트 검색 검증 메시지/})).toBeVisible();
  await search.fill("a-nonexistent-fixture-only-title");await expect(page.getByText("표시할 채팅이 없습니다.",{exact:true})).toBeVisible();
  const clearSearch = page.getByRole("button", {name: "검색어 지우기", exact: true});
  await clearSearch.click();
  await expect(search).toHaveValue("");
  await expect(search).toBeFocused();
  await expect(clearSearch).toHaveCount(0);
  await expect(page.getByRole("link", {name: /프로젝트 검색 검증 메시지/})).toBeVisible();
  await page.getByRole("button",{name:"설정",exact:true}).click();
  const settings = page.getByRole("dialog", { name: "설정", exact: true });
  await settings.getByRole("button",{name:"계정 관리",exact:true}).click();
  await settings.getByRole("button",{name:"로그아웃",exact:true}).click();
  await expect(page.getByRole("button",{name:"연결",exact:true})).toBeVisible();
  const denied=await page.evaluate(()=>new Promise(resolve=>(window as any).Meteor.call("codexm.call","web.catalog",{},"",(error:any)=>resolve(error?.error))));
  expect(denied).toBe("authentication_required");
  expect(await page.evaluate(()=>fetch("/auth/ticket",{method:"POST",headers:{"Content-Type":"application/json"},body:"{}"}).then(r=>r.status))).toBe(401);
});

for(const viewport of [{width:375,height:667},{width:390,height:844}])test(`mobile navigation, readable transcript and composer at ${viewport.width}`,async({page})=>{
  await page.setViewportSize(viewport);await page.emulateMedia({reducedMotion:"reduce"});await authenticate(page);await menu(page);
  await expect(page.getByLabel("채팅 목록",{exact:true})).toBeVisible();
  if (process.platform === "darwin") {
    const brand = page.locator(".sidebar .brand");
    await expect(brand.getByRole("button", {name: "GUI 종료", exact: true})).toBeVisible();
    expect(await brand.evaluate(node => node.scrollWidth)).toBeLessThanOrEqual(await brand.evaluate(node => node.clientWidth));
  }
  await page.getByRole("link",{name:/긴 이력 페이지 테스트/}).click();
  await expect(page.getByLabel("메시지",{exact:true})).toBeVisible();
  const header = page.locator(".chat-header");
  const modelSelect = header.getByRole("combobox",{name:"모델",exact:true});
  await expect(modelSelect).toBeVisible();
  await expect(header.getByRole("combobox",{name:"Reasoning effort",exact:true})).toBeVisible();
  const quota = header.getByRole("group", { name: "선택 계정의 남은 한도" });
  const [limits, account, model] = await Promise.all([quota.boundingBox(), header.getByLabel("이번에 사용할 계정").boundingBox(), modelSelect.boundingBox()]);
  expect(limits).toBeTruthy(); expect(account).toBeTruthy(); expect(model).toBeTruthy();
  expect(limits!.x + limits!.width).toBeLessThan(account!.x);
  expect(limits!.y + limits!.height).toBeCloseTo(account!.y + account!.height, 0);
  expect(model!.y).toBeGreaterThan(limits!.y + limits!.height);
  const bars = await quota.locator("meter, .limit-track").evaluateAll(items => items.map(item => item.getBoundingClientRect().toJSON()));
  expect(bars).toHaveLength(2);
  expect(bars[1].y).toBeGreaterThan(bars[0].y);
  expect(bars[1].x).toBe(bars[0].x);
  expect(bars[0].width).toBeGreaterThanOrEqual(60);
  await expect(page.getByRole("button",{name:"큐에 추가",exact:true})).toBeVisible();
  await expect(page.locator(".turn")).toHaveCount(8);
  const tableScroller = page.locator(".turn").last().getByRole("region", { name: "채팅 표" });
  await expect(tableScroller.getByRole("table")).toBeVisible();
  const tableOverflow = await tableScroller.evaluate(el => {
    el.scrollLeft = el.scrollWidth;
    const overflow = { width: el.clientWidth, content: el.scrollWidth, offset: el.scrollLeft };
    el.scrollLeft = 0;
    return overflow;
  });
  expect(tableOverflow.content).toBeGreaterThan(tableOverflow.width);
  expect(tableOverflow.offset).toBeGreaterThan(0);
  const sizes=await page.evaluate(()=>({page:document.documentElement.scrollWidth,width:innerWidth,height:document.documentElement.scrollHeight,viewport:innerHeight}));expect(sizes.page).toBeLessThanOrEqual(sizes.width);expect(sizes.height).toBeLessThanOrEqual(sizes.viewport);
  await page.screenshot({path:join(fixtureRoot!,`mobile-${viewport.width}.png`)});
});
