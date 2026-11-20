import type { IncomingMessage, ServerResponse } from "node:http";
import { constants } from "node:fs";
import { open, rm, type FileHandle } from "node:fs/promises";
import { join } from "node:path";
import { pipeline } from "node:stream/promises";
import { artifactPreview, chatIdFromPath, type ChatArtifact } from "../imports/contracts/web";
import type { LocalAuth } from "./auth";
import type { BridgeClient } from "./bridge";

const fileID = /^[0-9a-f]{64}$/;
const registry = "artifacts-v1";

function disposition(name: string, inline: boolean) {
  const ascii = name.replace(/[^A-Za-z0-9._ -]/g, "_") || "file";
  const encoded = encodeURIComponent(name).replace(/['()*]/g, value => "%" + value.charCodeAt(0).toString(16));
  return (inline ? "inline" : "attachment") + '; filename="' + ascii + '"; filename*=UTF-8' + "''" + encoded;
}

export function artifactRange(header: string, size: number): { start: number; end: number } | undefined {
  const match = /^bytes=(\d*)-(\d*)$/i.exec(header);
  if (!match || size === 0 || (!match[1] && !match[2])) return;
  const start = match[1] ? Number(match[1]) : Math.max(0, size - Number(match[2]));
  const end = match[1] && match[2] ? Math.min(size - 1, Number(match[2])) : size - 1;
  if (start < size && start <= end) return { start, end };
}

export async function serveArtifact(req: IncomingMessage, res: ServerResponse, state: string, auth: LocalAuth, bridge: BridgeClient) {
  const finish = (code: number) => {
    if (code >= 400) {
      res.removeHeader("Content-Length");
      res.removeHeader("Content-Disposition");
    }
    res.statusCode = code;
    res.end();
  };
  res.setHeader("Cache-Control", "private, no-store");
  res.setHeader("Cross-Origin-Resource-Policy", "same-origin");
  res.setHeader("Content-Security-Policy", "sandbox; default-src 'none'");
  if (req.method !== "GET" && req.method !== "HEAD") {
    res.setHeader("Allow", "GET, HEAD");
    return finish(405);
  }
  const session = auth.session(req.headers.cookie);
  if (!session) return finish(401);
  const match = /^\/artifacts\/([^/]+)\/([0-9a-f]{64})$/.exec(req.url ?? "");
  if (!match) return finish(404);
  const [, thread, id] = match;
  if (chatIdFromPath("/chat/" + thread) !== thread || !fileID.test(id)) return finish(404);
  let handle: FileHandle | undefined;
  try {
    const file = await bridge.call("web.artifact", { thread_id: thread, artifact_id: id }) as (ChatArtifact & { version: string }) | null;
    if (!file || file.id !== id || !fileID.test(file.version)) return finish(404);
    handle = await open(join(state, registry, thread, id + "-" + file.version), constants.O_RDONLY | constants.O_NOFOLLOW | constants.O_NONBLOCK);
    const info = await handle.stat();
    if (!info.isFile() || info.size !== file.size) return finish(404);
    if (auth.session(req.headers.cookie) !== session) return finish(401);
    const inline = !!artifactPreview(file.mime);
    const etag = '"' + file.version + '"';
    const requested = req.method === "GET" && (!req.headers["if-range"] || req.headers["if-range"] === etag) ? req.headers.range : undefined;
    const range = requested ? artifactRange(requested, info.size) : undefined;
    res.setHeader("Accept-Ranges", "bytes");
    res.setHeader("ETag", etag);
    if (requested && !range) {
      res.setHeader("Content-Range", `bytes */${info.size}`);
      return finish(416);
    }
    res.setHeader("Content-Type", inline ? file.mime : "application/octet-stream");
    res.setHeader("Content-Disposition", disposition(file.name, inline));
    res.setHeader("Content-Length", String(range ? range.end - range.start + 1 : info.size));
    if (req.method === "HEAD") return finish(200);
    if (range) {
      res.statusCode = 206;
      res.setHeader("Content-Range", `bytes ${range.start}-${range.end}/${info.size}`);
    }
    await pipeline(handle.createReadStream({ autoClose: false, ...range }), res);
  } catch {
    if (!res.headersSent && !res.destroyed) finish(404);
    else res.destroy();
  } finally {
    await handle?.close().catch(() => {});
  }
}

export function deleteArtifacts(state: string, thread: string) {
  if (chatIdFromPath("/chat/" + thread) !== thread) return;
  return rm(join(state, registry, thread), { recursive: true, force: true }).catch(() => {});
}
