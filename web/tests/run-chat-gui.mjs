// Run from an owned development job, e.g. a named default/codexm tmux window.
// The runtime, model, conversations and service registration are all isolated.
import { spawn } from "node:child_process";
import { createServer } from "node:net";
import { mkdir, mkdtemp, readFile, writeFile, open } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, isAbsolute, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const source = dirname(root);
const args = process.argv.slice(2);
const options = {};
for (let i = 0; i < args.length; i += 2) {
  if (!["--binary", "--runtime", "--bundle", "--output"].includes(args[i]) || !args[i + 1])
    throw new Error("usage: node tests/run-chat-gui.mjs --binary ABS --runtime ABS --bundle ABS [--output ABS]");
  options[args[i].slice(2)] = args[i + 1];
}
for (const key of ["binary", "runtime", "bundle"])
  if (!isAbsolute(options[key] || "")) throw new Error(`${key} must be an absolute path`);
if (!process.version.startsWith("v24.")) throw new Error("use the web's Node 24 runtime");
const output = options.output || await mkdtemp(join(tmpdir(), "codexm-web-check-"));
if (!isAbsolute(output) || !relative(source, output).startsWith(".."))
  throw new Error("use a private output directory outside the source checkout");
await mkdir(output, { recursive: true, mode: 0o700 });
const fixtureDir = await mkdtemp(join(output, "fixture-"));
const log = await open(join(output, "check.log"), "a", 0o600);
let fixture, fixtureExit, manifest;
const children = new Set();
function run(program, argv, env = process.env, cwd = source) {
  const child = spawn(program, argv, { env, cwd, stdio: ["ignore", log.fd, log.fd] });
  children.add(child);
  const done = new Promise((ok, fail) => {
    child.once("error", fail);
    child.once("exit", (code, signal) => {
      children.delete(child);
      code === 0 ? ok() : fail(new Error(`${program} exited ${code ?? signal}; inspect ${join(output, "check.log")}`));
    });
  });
  done.catch(() => {}); // The fixture is awaited during cleanup.
  return { child, done };
}
async function waitFile(path) {
  for (let i = 0; i < 300; i++) {
    try { return JSON.parse(await readFile(path, "utf8")); }
    catch (error) {
      if (error.code !== "ENOENT" && !(error instanceof SyntaxError)) throw error;
      if (fixture.exitCode !== null) throw new Error("fixture exited before readiness");
      await new Promise(ok => setTimeout(ok, 100));
    }
  }
  throw new Error("fixture readiness timed out");
}
async function freePort() {
  const server = createServer();
  await new Promise((ok, fail) => { server.once("error", fail); server.listen(0, "127.0.0.1", ok); });
  const port = server.address().port;
  await new Promise(ok => server.close(ok));
  return port > 65532 ? freePort() : port;
}
const signals = () => { for (const child of children) child.kill("SIGTERM"); };
process.once("SIGINT", signals);
process.once("SIGTERM", signals);
try {
  const started = run("go", ["test", "./internal/nativecli", "-run", "^TestWebChatBrowserFixture$", "-count=1", "-timeout=47m", "-v"], {
    ...process.env, CODEXM_GUI_CONTROL_TEST: "1", CODEXM_NATIVE_RUNTIME: options.runtime,
    CODEXM_WEB_BROWSER_FIXTURE: fixtureDir,
  });
  fixture = started.child; fixtureExit = started.done;
  manifest = await waitFile(join(fixtureDir, "fixture.json"));
  const nativeConfig = join(manifest.home, "shared", "config.toml");
  const settings = await readFile(nativeConfig, "utf8");
  if (!settings.includes("env_key='CODEXM_GUI_FIXTURE_KEY'")) throw new Error("unrecognized synthetic model configuration");
  await writeFile(nativeConfig, settings.replace("env_key='CODEXM_GUI_FIXTURE_KEY'", "http_headers={Authorization='Bearer fixture-browser'}"), { mode: 0o600 });
  const port = await freePort();
  const env = { ...process.env, HOME: manifest.home, PATH: `${dirname(process.execPath)}:${process.env.PATH}` };
  await run(options.binary, ["--config", manifest.config, "web", "--bundle", options.bundle, "--port", String(port), "--no-open"], env).done;
  await run(process.execPath, [join(root, "node_modules/@playwright/test/cli.js"), "test", "tests/chat-gui.spec.ts"], {
    ...process.env, CODEXM_CHAT_GUI_FIXTURE: fixtureDir, CODEXM_CHAT_GUI_BINARY: options.binary,
    CODEXM_WEB_TEST_URL: `http://127.0.0.1:${port}`,
  }, root).done;
  console.log(`Chat web checks passed. Private artifacts: ${fixtureDir}`);
} finally {
  if (manifest) {
    const env = { ...process.env, HOME: manifest.home };
    await run(options.binary, ["--config", manifest.config, "web", "uninstall"], env).done;
    await writeFile(join(fixtureDir, "stop-fixture"), "isolated web service removed");
  }
  if (fixtureExit) await fixtureExit;
  await log.close();
  process.removeListener("SIGINT", signals);
  process.removeListener("SIGTERM", signals);
}
