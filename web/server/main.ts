import type { IncomingMessage, ServerResponse } from "node:http";
import { Meteor } from "meteor/meteor";
import { WebApp } from "meteor/webapp";
import { join, isAbsolute } from "node:path";
import { mkdirSync, writeFileSync } from "node:fs";
import { LocalAuth, LoginError } from "./auth";
import { BridgeClient, type BridgeError } from "./bridge";
import { bridgeContract } from "../imports/contracts/native";

const origin = new URL(process.env.ROOT_URL || "http://127.0.0.1:4317").origin;
const state = process.env.CODEXM_WEB_STATE;
if (!state || !isAbsolute(state))
  throw new Error("CODEXM_WEB_STATE is required");
if (process.env.BIND_IP !== "127.0.0.1")
  throw new Error("BIND_IP must be 127.0.0.1");
mkdirSync(state, { recursive: true, mode: 0o700 });
const auth = new LocalAuth(state, origin);
const bridge = new BridgeClient(
  process.env.CODEXM_BINARY || "",
  process.env.CODEXM_CONFIG || "",
);
const connections = new Map<string, Meteor.Connection>();
const methods = new Map<string, { mutation: boolean; model: boolean }>();
let capabilities: any;
const owner = (id?: string) => {
  try {
    return auth.require(id || "");
  } catch {
    throw new Meteor.Error(
      "authentication_required",
      "로그인이 필요합니다.",
    );
  }
};
function bridgeError(error: unknown) {
  const e = error as BridgeError;
  return new Meteor.Error(e.code || "request_failed", e.message);
}
async function bootstrap() {
  const next = await bridge.call("capabilities");
  if (
    next.protocolVersion !== bridgeContract.protocolVersion ||
    JSON.stringify(next.methods) !== JSON.stringify(bridgeContract.methods)
  )
    throw Object.assign(
      new Error("웹과 Go 연결 계약이 다릅니다. 같은 버전으로 다시 빌드하세요."),
      { code: "version_mismatch" },
    );
  capabilities = next;
  methods.clear();
  for (const method of capabilities.methods) methods.set(method.name, method);
  return capabilities;
}
async function body(req: import("node:http").IncomingMessage) {
  let text = "";
  for await (const chunk of req) {
    text += chunk;
    if (text.length > 4096) throw new Error("request too large");
  }
  return JSON.parse(text);
}
(WebApp as any).rawHandlers.use(
  async (req: IncomingMessage, res: ServerResponse, next: () => void) => {
    const isAuth = req.url?.startsWith("/auth/");
    const isDDP =
      req.url?.startsWith("/sockjs") || req.url?.startsWith("/websocket");
    if (
      !auth.validRequest(
        req.headers.host,
        req.headers.origin,
        (req.method !== "GET" && req.method !== "HEAD") || !!isDDP,
      )
    ) {
      res.writeHead(403);
      res.end("Origin denied");
      return;
    }
    res.setHeader("X-Content-Type-Options", "nosniff");
    res.setHeader("Referrer-Policy", "no-referrer");
    res.setHeader("X-Frame-Options", "DENY");
    if (auth.secure) res.setHeader("Strict-Transport-Security", "max-age=86400");
    if (!isAuth) {
      next();
      return;
    }
    res.setHeader("Cache-Control", "no-store");
    res.setHeader("Content-Type", "application/json");
    if (req.url === "/auth/mode" && req.method === "GET") {
      res.end(JSON.stringify({ password: auth.passwordEnabled() }));
      return;
    }
    if (
      req.method !== "POST" ||
      req.headers["content-type"] !== "application/json"
    ) {
      res.writeHead(405);
      res.end("{}");
      return;
    }
    try {
      if (req.url === "/auth/login" || req.url === "/auth/exchange") {
        const data = await body(req);
        if (req.url === "/auth/exchange" && typeof data.token !== "string")
          throw new Error("접속 코드가 필요합니다.");
        // The listener is loopback-only. The documented proxy overwrites this
        // header, rather than trusting the Internet client's forwarded headers.
        const remote = auth.secure ? String(req.headers["x-codexm-client-ip"] || "proxy") : (req.socket.remoteAddress || "local");
        const cookie = req.url === "/auth/login" ? await auth.login(data.username, data.password, remote) : auth.exchange(data.token, remote);
        res.setHeader(
          "Set-Cookie",
          auth.cookie(cookie),
        );
        res.end("{}");
      } else {
        const session = auth.session(req.headers.cookie);
        if (!session) throw new Error("로그인이 필요합니다.");
        if (req.url === "/auth/ticket")
          res.end(JSON.stringify({ ticket: auth.ticket(session) }));
        else if (req.url === "/auth/logout") {
          auth.logout(session);
          res.setHeader(
            "Set-Cookie",
            auth.cookie("", true),
          );
          res.end("{}");
        } else {
          res.writeHead(404);
          res.end("{}");
        }
      }
    } catch (error) {
      const code = error instanceof LoginError ? error.status : 401;
      if (code === 429) res.setHeader("Retry-After", "60");
      res.writeHead(code);
      res.end(JSON.stringify({ error: error instanceof SyntaxError ? "잘못된 요청입니다." : (error as Error).message }));
    }
  },
);
WebApp.httpServer.prependListener("upgrade", (req, socket) => {
  if (!auth.validRequest(req.headers.host, req.headers.origin, true))
    socket.destroy();
});
// SockJS filters Origin out of httpHeaders. It was validated on the HTTP
// upgrade/request; DDP still requires its own single-use ticket below.
Meteor.onConnection((connection) => {
  if (
    !auth.validRequest(
      connection.httpHeaders.host,
      connection.httpHeaders.origin,
      false,
    )
  ) {
    connection.close();
    return;
  }
  connections.set(connection.id, connection);
  connection.onClose(() => {
    connections.delete(connection.id);
    auth.disconnect(connection.id);
  });
});
Meteor.methods({
  "codexm.authenticate"(ticket: string) {
    if (!this.connection) throw new Meteor.Error("authentication_required");
    try {
      auth.authenticate(this.connection.id, ticket, () =>
        this.connection?.close(),
      );
      return true;
    } catch {
      throw new Meteor.Error("authentication_required");
    }
  },
  async "codexm.call"(
    method: string,
    params: Record<string, unknown>,
    operation: string,
  ) {
    const browser = owner(this.connection?.id);
    if (
      typeof method !== "string" ||
      !params ||
      Array.isArray(params) ||
      typeof params !== "object" ||
      JSON.stringify(params).length > 500_000 ||
      typeof operation !== "string"
    )
      throw new Meteor.Error("invalid_input");
    if (!capabilities) await bootstrap();
    const spec = methods.get(method);
    if (!spec) throw new Meteor.Error("unsupported");
    if (spec.mutation && !/^[A-Za-z0-9_-]{16,128}$/.test(operation))
      throw new Meteor.Error("invalid_input", "요청 ID가 필요합니다.");
    try {
      return await bridge.call(
        method,
        params,
        browser,
        operation,
        spec.mutation,
      );
    } catch (error) {
      throw bridgeError(error);
    }
  },
});
Meteor.publish("codexm.state", function () {
  owner(this.connection.id);
  let stopped = false,
    running = false,
    dirty = false,
    previous = "",
    ready = false;
  const refresh = async () => {
    if (stopped) return;
    if (running) {
      dirty = true;
      return;
    }
    running = true;
    try {
      owner(this.connection.id);
      const caps = await bootstrap(),
        snapshot = await bridge.call("snapshot");
      if (stopped) return;
      const doc = { capabilities: caps, snapshot, connectionError: "" },
        serialized = JSON.stringify(doc, (key, value) =>
          key === "observed_at" ? undefined : value,
        );
      if (serialized !== previous) {
        if (ready) this.changed("codexm_view", "state", doc);
        else this.added("codexm_view", "state", doc);
        previous = serialized;
      }
      if (!ready) {
        this.ready();
        ready = true;
      }

    } catch (error) {
      if (!stopped) {
        if ((error as any).error === "authentication_required")
          this.error(error as Meteor.Error);
        else {
          const doc = { connectionError: (error as Error).message };
          if (ready) this.changed("codexm_view", "state", doc);
        }
      }
    } finally {
      running = false;
      if (dirty) {
        dirty = false;
        void refresh();
      }
    }
  };
  const notify = () => {
    void refresh();
  };
  bridge.on("notification", notify);
  const timer = setInterval(notify, 1000);
  void refresh();
  this.onStop(() => {
    stopped = true;
    clearInterval(timer);
    bridge.off("notification", notify);
  });
});
Meteor.startup(async () => {
  bridge.start();
  writeFileSync(
    join(state!, "ready.json"),
    JSON.stringify({ pid: process.pid, origin, protocolVersion: 1 }),
    { mode: 0o600 },
  );
  try {
    await bootstrap();
  } catch {
    /* State publication reports reconnects. */
  }
});
process.on("exit", () => bridge.close());
for (const signal of ["SIGTERM", "SIGINT"] as const)
  process.once(signal, () => {
    bridge.close();
    process.exit(0);
  });
