import { Meteor } from "meteor/meteor";
import { Tracker } from "meteor/tracker";
import { useSyncExternalStore } from "react";
import type { Data } from "../imports/contracts/types";
let state: Data = {},
  access = { ready: false, message: "" };
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
function setAccess(ready: boolean, message = "") {
  access = { ready, message };
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
let subscription: Meteor.SubscriptionHandle | undefined,
  connecting = false;
export async function authenticate() {
  if (connecting || !Meteor.status().connected) return;
  connecting = true;
  try {
    const response = await fetch("/auth/ticket", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: "{}",
    });
    const data = await response.json();
    if (!response.ok) throw new Error(data.error);
    await method("codexm.authenticate", [data.ticket]);
    subscription?.stop();
    subscription = Meteor.subscribe("codexm.state", {
      onStop(error: any) {
        if (error) setAccess(false, error.reason || error.message);
      },
    });
    setAccess(true);
  } catch (error) {
    setAccess(false, (error as Error).message);
  } finally {
    connecting = false;
  }
}
export async function exchange(token: string) {
  const response = await fetch("/auth/exchange", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ token }),
  });
  const data = await response.json();
  if (!response.ok) throw new Error(data.error);
  await authenticate();
}
Meteor.startup(() => {
  let token = location.hash.slice(1);
  history.replaceState(null, "", location.pathname);
  Tracker.autorun(() => {
    if (Meteor.status().connected) {
      if (!token) void authenticate();
    } else setAccess(false, "로컬 서버 연결 중…");
  });
  if (token) {
    const initial = decodeURIComponent(token);
    token = "";
    void exchange(initial).catch((e) => setAccess(false, e.message));
  }
});
