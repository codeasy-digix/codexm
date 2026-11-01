#!/usr/bin/env node
// Copy into a private fixture directory containing .fixture-only. Never handles
// real credentials or models; all auth values here are inert test data.
const fs = require("node:fs"), path = require("node:path"), readline = require("node:readline");
const root = __dirname, args = process.argv.slice(2);
if (args.includes("--version")) { console.log("codex-cli fixture"); process.exit(0); }
if (!fs.existsSync(path.join(root, ".fixture-only"))) process.exit(9);
if (args.includes("app-server")) {
  let active = false, sent = false;
  const reply = (id, result) => process.stdout.write(JSON.stringify({id,result}) + "\n");
  const timer = setInterval(() => {
    if (!active || sent || !fs.existsSync(path.join(root,"complete-login"))) return;
    fs.unlinkSync(path.join(root,"complete-login")); sent = true;
    fs.writeFileSync(path.join(process.env.CODEX_HOME,"auth.json"), JSON.stringify({auth_mode:"chatgpt",tokens:{id_token:"fixture-id",access_token:"fixture-access",refresh_token:"fixture-refresh",account_id:"fixture-account"}}),{mode:0o600});
    process.stdout.write(JSON.stringify({method:"account/login/completed",params:{loginId:"fixture-login",success:true}})+"\n");
  },50);
  const lines = readline.createInterface({input:process.stdin});
  lines.on("line", line => {
    const r = JSON.parse(line); if (r.id == null) return;
    if (r.method === "initialize") reply(r.id,{});
    else if (r.method === "account/login/start") {
      active=true; sent=false;
      reply(r.id,{type:r.params.type,loginId:"fixture-login",...(r.params.type==="chatgpt"?{authUrl:"https://auth.openai.com/oauth/authorize?state=FIXTURE"}:{verificationUrl:"https://auth.openai.com/codex/device",userCode:"FIXTURE-CODE"})});
    } else if (r.method === "account/read") reply(r.id,{account:{type:"chatgpt"},workspaceRouting:{chatgptAccountId:"fixture-account"}});
    else if (r.method === "account/login/cancel") {active=false;reply(r.id,{status:"canceled"});}
    else {process.stderr.write("unexpected fixture method\n");process.exit(8);}
  });
  lines.on("close",()=>{clearInterval(timer);process.exit(0);});
} else if (args.includes("archive")) {
  if (fs.existsSync(path.join(root,"fail-archive"))) {console.error("Error: failed to archive session");process.exit(1);}
} else if (args.includes("unarchive")) {
  process.exit(0);
} else if (args.includes("resume")) {
  console.log("Official CLI fixture history\nSelected account: " + process.env.CODEX_ACCOUNT + "\nPrevious assistant: 준비된 작업을 이어갑니다.");
  readline.createInterface({input:process.stdin}).on("line",line=>console.log("Received: " + line));
} else process.exit(7);
