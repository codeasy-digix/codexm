import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { isAbsolute } from "node:path";
import { EventEmitter } from "node:events";
import { randomUUID } from "node:crypto";
import { existsSync } from "node:fs";
export type BridgeError = Error & { code?: string; retryable?: boolean };
type Pending = {
  message: string;
  resolve: (value: any) => void;
  reject: (reason: Error) => void;
  timer: NodeJS.Timeout;
  mutation: boolean;
};
export class BridgeClient extends EventEmitter {
  private child?: ChildProcessWithoutNullStreams;
  private pending = new Map<string, Pending>();
  private active?: string;
  private closing = false;
  private buffer = "";
  private restart?: NodeJS.Timeout;
  get busy() { return this.active !== undefined || this.pending.size !== 0; }
  constructor(
    readonly binary: string,
    readonly config: string,
  ) {
    super();
    if (!isAbsolute(binary) || !isAbsolute(config) || !existsSync(binary))
      throw new Error("CODEXM_BINARY and CODEXM_CONFIG must be absolute paths");
  }
  start() {
    if (this.child || this.closing) return;
    const child = spawn(
      this.binary,
      ["--config", this.config, "bridge", "--stdio", "--web"],
      { shell: false, stdio: "pipe" },
    );
    this.child = child;
    this.buffer = "";
    child.stdin.on("error", () => child.kill());
    child.stderr.resume(); // Never forward native authentication output to browser/logs.
    child.stdout.setEncoding("utf8");
    child.stdout.on("data", (chunk) => {
      this.buffer += chunk;
      if (this.buffer.length > 16 * 1024 * 1024) {
        child.kill();
        return;
      }
      let newline: number;
      while ((newline = this.buffer.indexOf("\n")) >= 0) {
        const line = this.buffer.slice(0, newline);
        this.buffer = this.buffer.slice(newline + 1);
        try {
          const reply = JSON.parse(line);
          if (reply.protocolVersion !== 1) throw new Error("protocol mismatch");
          if (reply.method) {
            this.emit("notification", reply);
            continue;
          }
          if (reply.id !== this.active) continue;
          this.active = undefined;
          const pending = this.pending.get(reply.id);
          if (pending) {
            clearTimeout(pending.timer);
            this.pending.delete(reply.id);
            if (reply.error)
              pending.reject(Object.assign(new Error(reply.error.message), reply.error));
            else pending.resolve(reply.result);
          }
          this.sendNext();
        } catch {
          child.kill();
        }
      }
    });
    child.on("error", () => child.kill());
    child.on("close", () => {
      if (this.child !== child) return;
      this.child = undefined;
      for (const [id, p] of this.pending) {
        clearTimeout(p.timer);
        const unknown = p.mutation && id === this.active;
        p.reject(
          Object.assign(
            new Error(
              unknown
                ? "응답을 확인하지 못했습니다. 작업 상태를 확인한 뒤 진행하세요."
                : "관리 연결이 준비되지 않았습니다. 다시 시도해 주세요.",
            ),
            { code: unknown ? "outcome_unknown" : "service_unavailable" },
          ),
        );
      }
      this.active = undefined;
      this.pending.clear();
      this.emit("notification", { method: "resync" });
      if (!this.closing)
        this.restart = setTimeout(() => {
          this.start();
          this.emit("notification", { method: "resync" });
        }, 1000);
    });
  }
  private sendNext() {
    const child = this.child;
    if (!child || this.active !== undefined || this.closing) return;
    let next: [string, Pending] | undefined;
    // The native bridge is serial. Keep queued reads here so user mutations
    // can overtake them while preserving the order of those mutations.
    for (const entry of this.pending) {
      next ??= entry;
      if (entry[1].mutation) { next = entry; break; }
    }
    if (!next) return;
    const [id, request] = next;
    this.active = id;
    child.stdin.write(request.message, (error) => {
      if (error) child.kill();
    });
  }
  call(
    method: string,
    params: Record<string, unknown> = {},
    owner = "",
    operation = "",
    mutation = false,
  ): Promise<any> {
    this.start();
    if (!this.child || this.pending.size >= 32)
      return Promise.reject(
        Object.assign(new Error("관리 연결이 준비되지 않았습니다."), {
          code: "busy",
        }),
      );
    const id = randomUUID(),
      message = JSON.stringify({
        protocolVersion: 1,
        id,
        method,
        owner,
        operation_id: operation,
        params,
      });
    if (message.length > 512 * 1024)
      return Promise.reject(new Error("요청이 너무 큽니다."));
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        const sent = id === this.active;
        this.pending.delete(id);
        reject(
          Object.assign(
            new Error(sent
              ? "응답 대기 시간이 초과되었습니다. 작업 상태를 확인하세요."
              : "서버가 바빠 요청을 보내지 못했습니다. 다시 시도해 주세요."),
            { code: sent && mutation ? "outcome_unknown" : "timeout" },
          ),
        );
      }, 125_000);
      this.pending.set(id, { message: message + "\n", resolve, reject, timer, mutation });
      this.sendNext();
    });
  }
  close() {
    this.closing = true;
    clearTimeout(this.restart);
    this.child?.stdin.end();
    this.child?.kill("SIGTERM");
  }
}
