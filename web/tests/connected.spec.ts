import { test, expect, type Page } from "@playwright/test";
import { readFileSync, writeFileSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { resolve } from "node:path";

const control = process.env.CODEXM_WEB_FIXTURE;
const base = process.env.CODEXM_WEB_URL || "http://127.0.0.1:4317";
async function rpc(
  page: Page,
  method: string,
  params: any = {},
  operation = "",
): Promise<any> {
  const reply = await page.evaluate(
    ({ method, params, operation }) =>
      new Promise<any>((resolve) => {
        (window as any).Meteor.apply(
          "codexm.call",
          [method, params, operation],
          { noRetry: true },
          (error: any, result: any) =>
            resolve(
              error
                ? { error: { code: error.error, message: error.reason } }
                : { result },
            ),
        );
      }),
    { method, params, operation },
  );
  if (reply.error)
    throw Object.assign(new Error(reply.error.message), reply.error);
  return reply.result;
}
test("native fixture: two tabs, settings conflict, registration, paused goal account change, bridge and web restart", async ({
  page,
  context,
}) => {
  test.skip(!control, "opt-in isolated native fixture");
  const fixture = JSON.parse(readFileSync(control + "/ready.json", "utf8"));
  const binary = process.env.CODEXM_BINARY!;
  const launch = () =>
    execFileSync(
      binary,
      [
        "--config",
        fixture.config,
        "web",
        "--port",
        new URL(base).port || "4317",
        "--bundle",
        process.env.CODEXM_WEB_BUNDLE!,
        "--no-open",
      ],
      { timeout: 30000 },
    );
  launch();
  const code = readFileSync(fixture.state + "/web/access", "utf8").trim();
  await page.goto(base);
  await page.getByLabel("로컬 접속 코드").fill(code);
  await page.getByRole("button", { name: "연결", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "새 채팅", exact: true }),
  ).toBeVisible({ timeout: 20000 });
  await expect
    .poll(async () => !!(await rpc(page, "snapshot")).connected)
    .toBe(true);
  const tab = await context.newPage();
  await tab.goto(base);
  await expect(
    tab.getByRole("button", { name: "새 채팅", exact: true }),
  ).toBeVisible();
  const timings: Record<string, number> = {};
  await rpc(page, "sessions.list");
  let started = Date.now();
  const list = await rpc(page, "sessions.list");
  timings.list = Date.now() - started;
  expect(list.sessions.some((s: any) => s.job_id === fixture.first_job)).toBe(
    true,
  );
  await rpc(page, "jobs.details", { id: fixture.first_job });
  started = Date.now();
  const before = await rpc(page, "jobs.details", { id: fixture.first_job });
  timings.history = Date.now() - started;
  expect(timings.history).toBeLessThan(1000);
  const goalBefore = await rpc(page, "jobs.details", {
    id: fixture.second_job,
  });
  expect(goalBefore.goal.tokenBudget).toBe(30000);
  expect(goalBefore.goal.status).toBe("paused");
  const change = randomUUID(),
    args = {
      id: fixture.second_job,
      run_id: goalBefore.job.run_id,
      expected_account: goalBefore.job.account,
      account: "account-work",
    };
  const results = await Promise.allSettled([
    rpc(page, "jobs.account", args, change),
    rpc(tab, "jobs.account", args, change),
  ]);
  expect(results.some((r) => r.status === "fulfilled")).toBe(true);
  await expect
    .poll(
      async () =>
        (await rpc(page, "jobs.details", { id: fixture.second_job })).job
          .account,
      { timeout: 30000 },
    )
    .toBe("account-work");
  const after = await rpc(page, "jobs.details", { id: fixture.second_job });
  expect(after.job.thread_id).toBe(goalBefore.job.thread_id);
  expect(after.goal.tokenBudget).toBe(30000);
  expect(after.goal.tokensUsed).toBe(goalBefore.goal.tokensUsed);
  expect(after.goal.status).toBe("paused");
  expect(after.job.paused).toBe(true);
  const replay = await rpc(tab, "jobs.account", args, change);
  expect(replay.id).toBe(change);
  await page.getByRole("button", { name: /예산 보존 시험 목표/ }).click();
  await page.getByRole("button", { name: "작업 상세", exact: true }).click();
  await page.getByText("목표 편집", { exact: true }).click();
  const goalForm = page.locator(".details form");
  await expect(
    goalForm.getByRole("textbox", { name: "목표", exact: true }),
  ).toHaveValue(goalBefore.goal.objective);
  const status = goalForm.getByRole("combobox", {
    name: "목표 상태",
    exact: true,
  });
  await expect(status).toHaveValue("");
  await goalForm
    .getByRole("spinbutton", { name: "토큰 예산", exact: true })
    .fill("31000");
  await goalForm
    .getByRole("button", { name: "목표 저장", exact: true })
    .click();
  await expect
    .poll(
      async () =>
        (await rpc(page, "jobs.details", { id: fixture.second_job })).goal
          .tokenBudget,
      { timeout: 20000 },
    )
    .toBe(31000);
  const edited = await rpc(page, "jobs.details", { id: fixture.second_job });
  expect(edited.goal.status).toBe("paused");
  expect(edited.goal.tokensUsed).toBe(goalBefore.goal.tokensUsed);
  expect(edited.thread.turns.length).toBe(0);
  await goalForm
    .getByRole("spinbutton", { name: "토큰 예산", exact: true })
    .fill("30000");
  await goalForm
    .getByRole("button", { name: "목표 저장", exact: true })
    .click();
  await expect
    .poll(
      async () =>
        (await rpc(page, "jobs.details", { id: fixture.second_job })).goal
          .tokenBudget,
      { timeout: 20000 },
    )
    .toBe(30000);
  const saved = await rpc(page, "settings.read");
  const updated = {
    ...saved.values,
    max_parallel_jobs: saved.values.max_parallel_jobs + 1,
  };
  await rpc(
    page,
    "settings.save",
    { revision: saved.revision, values: updated },
    randomUUID(),
  );
  await expect(
    rpc(
      tab,
      "settings.save",
      { revision: saved.revision, values: saved.values },
      randomUUID(),
    ),
  ).rejects.toMatchObject({ code: "settings_conflict" });
  const latest = await rpc(page, "settings.read");
  await rpc(
    page,
    "settings.save",
    { revision: latest.revision, values: saved.values },
    randomUUID(),
  );
  const registered = await rpc(
    page,
    "jobs.register",
    { thread_id: fixture.native_thread },
    randomUUID(),
  );
  expect(registered.thread_id).toBe(fixture.native_thread);
  await rpc(page, "jobs.unregister", { id: registered.id }, randomUUID());
  await page.getByRole("button", { name: "새 채팅", exact: true }).click();
  await page.getByText("새 작업 설정", { exact: true }).click();
  await page
    .locator(".empty")
    .getByLabel("프로젝트 경로", { exact: true })
    .fill(before.job.cwd);
  await page
    .getByRole("combobox", { name: "종류", exact: true })
    .selectOption("group");
  const title = "브라우저 그룹 " + randomUUID().slice(0, 8);
  await page.getByLabel("제목", { exact: true }).fill(title);
  started = Date.now();
  await page.getByRole("button", { name: "그룹 만들기", exact: true }).click();
  await expect(
    tab.getByRole("button", { name: title, exact: false }),
  ).toBeVisible({ timeout: 5000 });
  timings.event = Date.now() - started;
  // Verify the publication separately from the catalog's periodic refresh.
  const group = (await rpc(page, "snapshot")).state.jobs.find(
    (j: any) => j.title === title,
  );
  expect(group.kind).toBe("group");
  await page.getByRole("button", { name: "계정", exact: true }).click();
  for (const [account, reason] of [["account-backup", "관리자"], ["account-work", "작업에서 사용하는"]]) {
    await page.getByRole("button", { name: `${account} 등록 삭제`, exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "계정 등록 삭제" });
    await dialog.getByRole("button", { name: "등록 삭제", exact: true }).click();
    await expect(dialog.getByRole("alert")).toContainText(reason);
    await dialog.getByRole("button", { name: "취소", exact: true }).click();
  }
  const authPath = resolve(fixture.state, "../accounts/account-main/auth.json");
  const originalAuth = readFileSync(authPath);
  await page.getByRole("button", { name: "account-main 등록 삭제", exact: true }).click();
  await page.getByRole("dialog").getByRole("button", { name: "취소", exact: true }).click();
  expect((await rpc(page, "snapshot")).account_names).toContain("account-main");
  await page.getByRole("button", { name: "account-main 등록 삭제", exact: true }).click();
  const removal = page.getByRole("dialog", { name: "계정 등록 삭제" });
  await expect(removal).toContainText("로그인 파일·공용 기본 로그인·대화 기록은 보존");
  await page.screenshot({ path: control + "/account-removal-dialog.png" });
  await removal.getByRole("button", { name: "등록 삭제", exact: true }).click();
  await expect(removal).not.toBeVisible();
  await expect(page.getByRole("button", { name: "account-main 등록 삭제", exact: true })).toHaveCount(0);
  expect(readFileSync(authPath).equals(originalAuth)).toBe(true);
  expect((await rpc(tab, "snapshot")).account_names).not.toContain("account-main");
  expect((await rpc(page, "snapshot")).priority).toEqual(["account-work", "account-secondary", "account-backup"]);
  await page.screenshot({ path: control + "/accounts-after-removal.png" });
  const generation = (await rpc(page, "capabilities")).generation;
  const web = JSON.parse(
    readFileSync(fixture.state + "/web/ready.json", "utf8"),
  );
  const children = execFileSync("pgrep", ["-P", String(web.pid)], {
    encoding: "utf8",
  })
    .trim()
    .split(/\s+/)
    .map(Number);
  const bridge = children.find((pid) =>
    execFileSync("ps", ["-p", String(pid), "-o", "command="], {
      encoding: "utf8",
    }).includes("bridge --stdio"),
  );
  expect(bridge).toBeTruthy();
  process.kill(bridge!, "SIGKILL");
  await expect
    .poll(
      async () => {
        try {
          return (await rpc(page, "capabilities")).generation !== generation;
        } catch {
          return false;
        }
      },
      { timeout: 15000 },
    )
    .toBe(true);
  expect(
    (await rpc(page, "jobs.details", { id: fixture.first_job })).job.thread_id,
  ).toBe(before.job.thread_id);
  const stop = execFileSync(
    binary,
    ["--config", fixture.config, "web", "stop"],
    { encoding: "utf8", timeout: 20000 },
  );
  expect(stop).toContain("Managed jobs keep running");
  process.kill(fixture.pid, 0);
  launch();
  await page.reload();
  await expect(
    page.getByRole("button", { name: "새 채팅", exact: true }),
  ).toBeVisible({ timeout: 20000 });
  expect((await rpc(page, "snapshot")).account_names).not.toContain("account-main");
  expect(
    (await rpc(page, "jobs.details", { id: fixture.first_job })).job.thread_id,
  ).toBe(before.job.thread_id);
  expect(
    (await rpc(page, "jobs.details", { id: fixture.first_job })).job.run_id,
  ).toBe(before.job.run_id);
  await page.setViewportSize({ width: 390, height: 844 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({ path: control + "/mobile.png" });
  writeFileSync(
    control + "/browser-timings.json",
    JSON.stringify(timings, null, 2),
  );
});
