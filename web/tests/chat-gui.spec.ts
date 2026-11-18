import { test,expect,type Page } from "@playwright/test";
import { readFileSync,writeFileSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { join } from "node:path";

const fixtureRoot=process.env.CODEXM_CHAT_GUI_FIXTURE;
const binary=process.env.CODEXM_CHAT_GUI_BINARY;
test.skip(!fixtureRoot||!binary,"requires the isolated official-runtime browser fixture");
const url=process.env.CODEXM_WEB_TEST_URL||"http://127.0.0.1:15431";
function fixture(){return JSON.parse(readFileSync(join(fixtureRoot!,"fixture.json"),"utf8"));}

async function authenticate(page:Page){
  const f=fixture();
  execFileSync(binary!,["--config",f.config,"web","--no-open"],{stdio:"pipe",env:{...process.env,HOME:f.home}});
  await page.goto(url);
  await page.getByLabel("로컬 접속 코드",{exact:true}).fill(readFileSync(join(f.state,"access"),"utf8").trim());
  await page.getByRole("button",{name:"연결",exact:true}).click();
  await expect(page.getByRole("button",{name:"새 채팅",exact:true})).toBeAttached();
}

async function menu(page:Page){if(await page.getByRole("button",{name:"메뉴 열기",exact:true}).isVisible())await page.getByRole("button",{name:"메뉴 열기",exact:true}).click();}

test("recent-only history, anchored upward pagination, safe Markdown and lazy details",async({page})=>{
  await authenticate(page);
  await page.evaluate(()=>{
    const m=(window as any).Meteor,apply=m.apply;
    (window as any).__calls=[];
    m.apply=function(...args:any[]){if(args[0]==="codexm.call")(window as any).__calls.push(args[1][0]);return apply.apply(this,args);};
  });
  await page.getByRole("button",{name:/긴 이력 페이지 테스트/}).click();
  await expect(page.locator(".turn")).toHaveCount(8);
  await expect(page.getByText("페이지 이력 26",{exact:true})).toBeVisible();
  await expect(page.getByText("페이지 이력 01",{exact:true})).toHaveCount(0);
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

test("new chat, Enter intervention, Command+Enter queue and browser reconnect",async({page,context})=>{
  await authenticate(page);
  await page.getByRole("button",{name:"새 채팅",exact:true}).click();
  const dialog=page.getByRole("dialog",{name:"새 채팅",exact:true});
  await dialog.getByLabel("프로젝트",{exact:true}).selectOption(fixture().project);
  await dialog.getByRole("button",{name:"채팅 만들기",exact:true}).click();
  await expect(page.getByRole("heading", { name: "표시할 메시지가 없습니다" })).toBeVisible();
  const input=page.getByLabel("메시지",{exact:true});
  await input.fill("GUI_FIXTURE_ACTIVE_BROWSER");
  await input.dispatchEvent("keydown",{key:"Enter",keyCode:229,isComposing:true});
  await expect(input).toHaveValue("GUI_FIXTURE_ACTIVE_BROWSER");
  await input.press("Enter");
  await expect(page.locator(".working")).toBeVisible();
  const id=await page.locator("main.conversation").getAttribute("data-thread-id");
  await input.fill("지금 방향을 바로 수정합니다");await input.press("Enter");
  await expect(page.getByText("현재 작업에 전달했습니다.",{exact:true})).toBeVisible();
  await input.fill("다음 단계는 큐로 진행합니다");
  await input.press("End");
  await input.press("Shift+Enter");
  await expect(input).toHaveValue("다음 단계는 큐로 진행합니다\n");
  await expect(page.locator(".queue-preview")).toHaveCount(0);
  await input.fill("다음 단계는 큐로 진행합니다");
  await input.press("Meta+Enter");
  await expect(page.getByText("큐에 추가했습니다.",{exact:true})).toBeVisible();
  await expect(page.locator(".queue-preview summary")).toContainText("1개");
  await page.close();
  const next=await context.newPage();await next.goto(url);
  await next.getByRole("button",{name:/GUI_FIXTURE_ACTIVE_BROWSER/}).click();
  await expect(next.locator("main.conversation")).toHaveAttribute("data-thread-id",id!);
  await expect(next.locator(".working")).toBeVisible();
  writeFileSync(join(fixtureRoot!,"release-model"),"release fixture only");
  await expect(next.locator(".working")).toHaveCount(0,{timeout:20000});
  await expect(next.getByText("다음 단계는 큐로 진행합니다",{exact:true})).toBeVisible();
  await next.screenshot({path:join(fixtureRoot!,"desktop-conversation.png")});
});

test("chat model controls and saved shortcut assignments", async ({ page }) => {
  await authenticate(page);
  await page.getByRole("button", { name: /긴 이력 페이지 테스트/ }).click();
  const model = page.getByRole("combobox", { name: "모델", exact: true });
  const effort = page.getByRole("combobox", { name: "Reasoning effort", exact: true });
  await expect(model).toBeEnabled();
  const choice = (await model.locator("option").nth(1).getAttribute("value"))!;
  await model.selectOption(choice);
  await expect(model).toHaveValue(choice);
  const supported = await effort.locator("option[value=high]").count();
  const effortValue = supported ? "high" : (await effort.locator("option").nth(1).getAttribute("value"))!;
  await effort.selectOption(effortValue);
  const input = page.getByLabel("메시지", { exact: true });
  await input.fill("보내지 않은 초안");
  await page.getByRole("button", { name: "설정", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "단축키 설정", exact: true });
  await expect(dialog.getByLabel("⌘3 할당", { exact: true })).toHaveValue("");
  await dialog.getByLabel("⌘7 할당", { exact: true }).selectOption(`model:${choice}`);
  await dialog.getByLabel("⌘8 할당", { exact: true }).selectOption(`effort:${effortValue}`);
  await dialog.getByRole("button", { name: "저장", exact: true }).click();
  await model.selectOption("");
  await input.press("Meta+7");
  await expect(model).toHaveValue(choice);
  await effort.selectOption("");
  await input.press("Control+Shift+8");
  await expect(effort).toHaveValue(effortValue);
  await expect(input).toHaveValue("보내지 않은 초안");
  await page.reload();
  await page.getByRole("button", { name: "설정", exact: true }).click();
  await expect(dialog.getByLabel("⌘7 할당", { exact: true })).toHaveValue(`model:${choice}`);
  await expect(dialog.getByLabel("⌘8 할당", { exact: true })).toHaveValue(`effort:${effortValue}`);
});

test("long command output is fetched only when its nested accordion opens",async({page})=>{
  await authenticate(page);
  await page.evaluate(()=>{const m=(window as any).Meteor,apply=m.apply;(window as any).__calls=[];m.apply=function(...args:any[]){if(args[0]==="codexm.call")(window as any).__calls.push(args[1][0]);return apply.apply(this,args);};});
  await page.getByRole("button",{name:/긴 명령 출력 검증/}).click();
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
  await expect(page.getByRole("heading",{name:"작업을 확인했습니다",exact:true})).toBeVisible();
  const id=await page.locator("main.conversation").getAttribute("data-thread-id");
  await page.getByRole("button",{name:"채팅 메뉴",exact:true}).click();await page.getByRole("menuitem",{name:"고정하기",exact:true}).click();
  await expect(page.getByText("고정 상태를 변경했습니다.")).toBeVisible();
  await page.getByRole("button",{name:/Pinned/}).click();await page.getByRole("button",{name:/관리 기능 검증용 채팅/}).click();
  await page.getByRole("button",{name:"채팅 메뉴",exact:true}).click();await page.getByRole("menuitem",{name:"프로젝트로 이동",exact:true}).click();
  dialog=page.getByRole("dialog",{name:"프로젝트로 이동",exact:true});await dialog.getByLabel("프로젝트",{exact:true}).selectOption("");await dialog.getByRole("button",{name:"적용",exact:true}).click();
  await expect(page.getByText("프로젝트를 옮겼습니다.")).toBeVisible();
  await page.getByRole("button",{name:"채팅 메뉴",exact:true}).click();await page.getByRole("menuitem",{name:"보관하기",exact:true}).click();
  dialog=page.getByRole("dialog",{name:"채팅 보관",exact:true});await dialog.getByRole("button",{name:"취소",exact:true}).click();
  await expect(page.locator("main.conversation")).toHaveAttribute("data-thread-id",id!);
  await page.getByRole("button",{name:"채팅 메뉴",exact:true}).click();await page.getByRole("menuitem",{name:"보관하기",exact:true}).click();await page.getByRole("dialog").getByRole("button",{name:"적용",exact:true}).click();
  await page.getByRole("button",{name:"보관함",exact:true}).click();await page.getByRole("button",{name:/관리 기능 검증용 채팅/}).click();
  await expect(page.getByRole("heading",{name:"작업을 확인했습니다",exact:true})).toBeVisible({timeout:20000});
  await expect(input).toHaveCount(0);await page.getByRole("button",{name:"보관 해제",exact:true}).click();await page.getByRole("dialog").getByRole("button",{name:"적용",exact:true}).click();
  await page.getByRole("button",{name:"Recents",exact:true}).click();await page.getByRole("button",{name:/관리 기능 검증용 채팅/}).click();
  await page.getByRole("button",{name:"채팅 메뉴",exact:true}).click();await page.getByRole("menuitem",{name:"영구 삭제",exact:true}).click();
  dialog=page.getByRole("dialog",{name:"채팅 영구 삭제",exact:true});await expect(dialog.getByText(/되돌릴 수 없습니다/)).toBeVisible();await dialog.getByRole("button",{name:"영구 삭제",exact:true}).click();
  await expect(page.getByRole("button",{name:/관리 기능 검증용 채팅/})).toHaveCount(0);
});

test("project creation, filtered new chat, search and logout gate",async({page})=>{
  await authenticate(page);
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
  await expect(page.getByRole("button",{name:/프로젝트 검색 검증 메시지/})).toBeVisible();
  await search.fill("a-nonexistent-fixture-only-title");await expect(page.getByText("표시할 채팅이 없습니다.",{exact:true})).toBeVisible();
  await page.getByRole("button",{name:"로그아웃",exact:true}).click();
  await expect(page.getByRole("button",{name:"연결",exact:true})).toBeVisible();
  const denied=await page.evaluate(()=>new Promise(resolve=>(window as any).Meteor.call("codexm.call","web.catalog",{},"",(error:any)=>resolve(error?.error))));
  expect(denied).toBe("authentication_required");
  expect(await page.evaluate(()=>fetch("/auth/ticket",{method:"POST",headers:{"Content-Type":"application/json"},body:"{}"}).then(r=>r.status))).toBe(401);
});

for(const viewport of [{width:375,height:667},{width:390,height:844}])test(`mobile navigation, readable transcript and composer at ${viewport.width}`,async({page})=>{
  await page.setViewportSize(viewport);await page.emulateMedia({reducedMotion:"reduce"});await authenticate(page);await menu(page);
  await expect(page.getByLabel("채팅 목록",{exact:true})).toBeVisible();
  await page.getByRole("button",{name:/긴 이력 페이지 테스트/}).click();
  await expect(page.getByLabel("메시지",{exact:true})).toBeVisible();
  await expect(page.getByRole("button",{name:"큐에 추가",exact:true})).toBeVisible();
  await expect(page.locator(".turn")).toHaveCount(8);
  const sizes=await page.evaluate(()=>({page:document.documentElement.scrollWidth,width:innerWidth,height:document.documentElement.scrollHeight,viewport:innerHeight}));expect(sizes.page).toBeLessThanOrEqual(sizes.width);expect(sizes.height).toBeLessThanOrEqual(sizes.viewport);
  await page.screenshot({path:join(fixtureRoot!,`mobile-${viewport.width}.png`)});
});
