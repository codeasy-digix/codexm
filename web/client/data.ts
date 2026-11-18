import { Meteor } from "meteor/meteor";
import { Tracker } from "meteor/tracker";
import { useSyncExternalStore } from "react";
import type { Data } from "../imports/contracts/types";
import { currentSubscription } from "./push";
export const queueDraftKeyPrefix = "codexm.queueDraft.";
type AccessStatus = "checking" | "authenticated" | "anonymous" | "unavailable";
let state: Data = {};
let access: { status: AccessStatus; message: string; password: boolean } =
  { status: "checking", message: "", password: true };
const listeners = new Set<() => void>(),
  authListeners = new Set<() => void>();
const documents = new Map<string, Data>();
(Meteor as any).connection.registerStoreClient("codexm_view", {
  beginUpdate(_count: number, reset: boolean) {
    if (reset) documents.clear();
  },
  update(message: Data) {
    if (message.msg === "removed") documents.delete(message.id);
    else
      documents.set(message.id, {
        ...documents.get(message.id),
        ...message.fields,
      });
  },
  endUpdate() {
    state = documents.get("state") || {};
    for (const listener of listeners) listener();
  },
  saveOriginals() {},
  retrieveOriginals() {
    return new Map();
  },
});
export const useStateData = () =>
  useSyncExternalStore(
    (cb) => {
      listeners.add(cb);
      return () => {
        listeners.delete(cb);
      };
    },
    () => state,
  );
export const useAccess = () =>
  useSyncExternalStore(
    (cb) => {
      authListeners.add(cb);
      return () => {
        authListeners.delete(cb);
      };
    },
    () => access,
  );
function setAccess(status: AccessStatus, message = "", password = access.password) {
  access = { status, message, password };
  for (const listener of authListeners) listener();
}
export function method<T = any>(name: string, args: any[]): Promise<T> {
  return new Promise((resolve, reject) =>
    Meteor.apply(name, args, { noRetry: true }, (error: any, result: any) =>
      error ? reject(error) : resolve(result),
    ),
  );
}
export async function call<T = any>(
  name: string,
  params: Data = {},
  operation = "",
): Promise<T> {
  return method("codexm.call", [name, params, operation]);
}
export function mutationUnconfirmed(message: string) {
  return /outcome|확인하지 못|대기 시간이 초과|연결이 끊|unknown|timed out|deadline|EOF|disconnected|invocation-failed/i.test(message);
}
let subscription: Meteor.SubscriptionHandle | undefined;
let authEpoch = 0;
let checking: Promise<void> | undefined;

function resetConnection() {
  authEpoch++;
  checking = undefined;
  subscription?.stop();
  subscription = undefined;
}

function authPost(endpoint: string, data: Record<string, string> = {}) {
  return fetch(`/auth/${endpoint}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(data),
    signal: AbortSignal.timeout(12_000),
  });
}

// LocalAuth uses the existing HttpOnly cookie and a one-time DDP ticket.
export function authenticate(message?: string): Promise<void> {
  if (!Meteor.status().connected) return Promise.resolve();
  if (checking) return checking;
  const epoch = authEpoch;
  setAccess("checking");
  checking = (async () => {
    try {
      const response = await authPost("ticket");
      if (epoch !== authEpoch) return;
      if (response.status === 401) {
        const mode = await fetch("/auth/mode", { signal: AbortSignal.timeout(12_000) });
        if (!mode.ok) throw new Error("로그인 설정을 읽을 수 없습니다.");
        const { password } = await mode.json();
        if (epoch === authEpoch) setAccess("anonymous", message || "로그인이 필요합니다.", password !== false);
        return;
      }
      const data = await response.json();
      if (!response.ok) throw new Error(data.error);
      if (epoch !== authEpoch) return;
      await method("codexm.authenticate", [data.ticket]);
      if (epoch !== authEpoch || !Meteor.status().connected) return;
      subscription?.stop();
      subscription = Meteor.subscribe("codexm.state", {
        onStop(error: any) {
          if (error && epoch === authEpoch && Meteor.status().connected) {
            setAccess("unavailable", error.reason || error.message);
          }
        },
      });
      setAccess("authenticated");
    } catch (error) {
      if (epoch === authEpoch) setAccess("unavailable", (error as Error).message);
    } finally {
      if (epoch === authEpoch) checking = undefined;
    }
  })();
  return checking;
}

async function openSession(endpoint: "exchange" | "login", input: Record<string, string>) {
  const response = await authPost(endpoint, input);
  if (!response.ok) throw new Error((await response.json()).error);
}

export async function exchange(token: string) {
  await openSession("exchange", { token });
  await authenticate();
}

export async function login(username: string, password: string) {
  await openSession("login", { username, password });
  await authenticate();
}

export async function logout() {
  const subscription = await currentSubscription().catch(() => null);
  const response = await authPost("logout", subscription ? { endpoint: subscription.endpoint } : {});
  if (!response.ok) throw new Error("로그아웃하지 못했습니다.");
  void subscription?.unsubscribe().catch(() => {});
  try {
    for (let i = sessionStorage.length - 1; i >= 0; i--) {
      const key = sessionStorage.key(i);
      if (key?.startsWith(queueDraftKeyPrefix)) sessionStorage.removeItem(key);
    }
  } catch { /* The server already ended this session. */ }
  resetConnection();
  documents.clear();
  state = {};
  for (const listener of listeners) listener();
  setAccess("checking");
  await authenticate("로그아웃했습니다.");
}

async function startAccess() {
  const token = location.hash.slice(1);
  let message: string | undefined;
  if (token) {
    history.replaceState(history.state, "", location.pathname + location.search);
    try {
      await openSession("exchange", { token: decodeURIComponent(token) });
    } catch (error) {
      message = (error as Error).message;
    }
  }
  let wasConnected = false;
  Tracker.autorun(() => {
    const connected = Meteor.status().connected;
    if (connected === wasConnected) return;
    wasConnected = connected;
    if (connected) {
      void authenticate(message);
      message = undefined;
    } else {
      resetConnection();
      setAccess("checking", "서버 연결 중…");
    }
  });
}
Meteor.startup(() => { void startAccess(); });
