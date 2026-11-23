import { expect, test } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, realpathSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { MongoClient } from "mongodb";

const fixture = process.env.CODEXM_CHAT_GUI_FIXTURE;
const binary = process.env.CODEXM_CHAT_GUI_BINARY;
const url = process.env.CODEXM_WEB_TEST_URL;
test.skip(!fixture || !binary || !url || process.env.CODEXM_WEB_TEST_MODE !== "catalog" ||
  process.env.CODEXM_WEB_TEST_MONGO !== "1", "requires the isolated official runtime and private MongoDB");

test("native list changes follow browser entry, chat navigation, and the background poll", async ({ page }) => {
  test.setTimeout(125_000);
  const f = JSON.parse(readFileSync(join(fixture!, "fixture.json"), "utf8"));
  const socket = join(realpathSync(join(f.state, "history-cache")), "mongodb.sock");
  const client = new MongoClient("mongodb://" + encodeURIComponent(socket) + "/codexm", { directConnection: true });
  const database = client.db();
  async function cachedTitle() {
    const active = await database.collection<{ _id: string; generation: string }>("catalog_manifest_v1").findOne({ _id: "current" });
    if (!active) return;
    const chunks = await database.collection<{
      generation: string; rows: Array<{ thread: { id: string; name: string } }>;
    }>("catalog_pages_v1").find({ generation: active.generation }).toArray();
    return chunks.flatMap(chunk => chunk.rows).find(row => row.thread.id === f.catchup)?.thread.name;
  }
  async function renameInNative(event: string) {
    writeFileSync(join(fixture!, "rename-catalog-" + event), "", { mode: 0o600 });
    await expect.poll(() => existsSync(join(fixture!, "catalog-renamed-" + event))).toBe(true);
  }
  try {
    await expect.poll(cachedTitle).toBe("오프라인 갱신 테스트");
    await test.step("publish a change made before the browser enters", async () => {
      await renameInNative("login");
      execFileSync(binary!, ["--config", f.config, "web", "--no-open"], {
        stdio: "pipe", env: { ...process.env, HOME: f.home },
      });
      await page.goto(url!);
      await page.getByLabel("로컬 접속 코드", { exact: true }).fill(readFileSync(join(f.state, "access"), "utf8").trim());
      await page.getByRole("button", { name: "연결", exact: true }).click();
      await expect(page.getByRole("link", { name: /오프라인 갱신 테스트/ })).toBeVisible({ timeout: 7000 });
      await expect(page.getByRole("link", { name: /원본에서 갱신한 목록 · 접속/ })).toBeVisible({ timeout: 25_000 });
      await expect.poll(cachedTitle).toBe("원본에서 갱신한 목록 · 접속");
    });
    await test.step("publish an external change on entering a chat", async () => {
      await renameInNative("open");
      await page.getByRole("link", { name: /긴 이력 페이지 테스트/ }).click();
      await expect(page.getByRole("heading", { name: "긴 이력 페이지 테스트", exact: true })).toBeVisible();
      await expect(page.getByText("페이지 이력 26", { exact: true })).toBeVisible({ timeout: 7000 });
      await expect(page.getByRole("link", { name: /원본에서 갱신한 목록 · 채팅 진입/ })).toBeVisible({ timeout: 25_000 });
      await expect.poll(cachedTitle).toBe("원본에서 갱신한 목록 · 채팅 진입");
    });
    await test.step("persist the native catalog while every browser is closed", async () => {
      await page.close();
      await renameInNative("periodic");
      await expect.poll(cachedTitle, { timeout: 70_000, intervals: [1000, 2000] }).toBe("원본에서 갱신한 목록 · 주기");
    });
  } finally {
    await client.close();
  }
});
