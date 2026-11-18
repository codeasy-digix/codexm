export type ShortcutAction = { kind: "model" | "effort"; value: string } | { kind: "reset" };
export type ChatShortcuts = Record<string, ShortcutAction>;

export const chatShortcutStorageKey = "codexm.chat-shortcuts.v3";
const legacyChordStorageKey = "codexm.chat-shortcuts.v2";
const legacyDigitStorageKey = "codexm.chat-shortcuts.v1";
const commandDigit = (digit: string) => `Meta+Shift+Digit${digit}`;
const resetShortcut = commandDigit("0");

export const defaultChatShortcuts: ChatShortcuts = {
  [commandDigit("1")]: { kind: "model", value: "gpt-6.2-sol" },
  [commandDigit("2")]: { kind: "model", value: "gpt-6.2-astra" },
  [commandDigit("4")]: { kind: "effort", value: "max" },
  [commandDigit("5")]: { kind: "effort", value: "xhigh" },
  [commandDigit("6")]: { kind: "effort", value: "high" },
  [resetShortcut]: { kind: "reset" },
};

const modifiers = [
  { name: "Meta", property: "metaKey", label: "⌘" },
  { name: "Control", property: "ctrlKey", label: "Ctrl" },
  { name: "Alt", property: "altKey", label: "Alt" },
  { name: "Shift", property: "shiftKey", label: "⇧" },
] as const;
const keyLabels: Record<string, string> = {
  Backquote: "`", Minus: "-", Equal: "=", BracketLeft: "[", BracketRight: "]", Backslash: "\\",
  Semicolon: ";", Quote: "'", Comma: ",", Period: ".", Slash: "/", Space: "Space",
  ArrowLeft: "←", ArrowUp: "↑", ArrowRight: "→", ArrowDown: "↓",
  Home: "Home", End: "End", PageUp: "Page Up", PageDown: "Page Down", Backspace: "Backspace", Delete: "Delete",
};
type ShortcutEvent = Pick<KeyboardEvent, "code" | "metaKey" | "ctrlKey" | "altKey" | "shiftKey"> &
  Partial<Pick<KeyboardEvent, "isComposing" | "keyCode" | "repeat" | "getModifierState">>;

function keyLabel(code: string) {
  if (/^(?:Key[A-Z]|Digit\d)$/.test(code)) return code.at(-1);
  if (/^Numpad\d$/.test(code)) return `Num ${code.at(-1)}`;
  if (/^F(?:[1-9]|1\d|2[0-4])$/.test(code)) return code;
  return Object.hasOwn(keyLabels, code) ? keyLabels[code] : undefined;
}

export function shortcutFromEvent(event: ShortcutEvent): string | undefined {
  if (event.isComposing || event.keyCode === 229 || event.repeat || event.getModifierState?.("AltGraph")) return;
  if (!(event.metaKey || event.ctrlKey || event.altKey) || !keyLabel(event.code)) return;
  // `code` stays stable when Shift or Option changes the character produced by the key.
  const pressed = modifiers.filter(modifier => event[modifier.property]).map(modifier => modifier.name);
  return [...pressed, event.code].join("+");
}

export function shortcutLabel(shortcut: string): string {
  const keys = shortcut.split("+");
  const code = keys.pop() || "";
  return [...keys.map(key => modifiers.find(modifier => modifier.name === key)?.label ?? key), keyLabel(code) ?? code].join("+");
}

function validShortcut(shortcut: string) {
  const keys = shortcut.split("+");
  const code = keys.pop() || "";
  return shortcutFromEvent({
    code, metaKey: keys.includes("Meta"), ctrlKey: keys.includes("Control"),
    altKey: keys.includes("Alt"), shiftKey: keys.includes("Shift"),
  }) === shortcut;
}

export function readChatShortcuts(storage?: Pick<Storage, "getItem">): ChatShortcuts {
  try {
    const source = storage ?? localStorage;
    const current = source.getItem(chatShortcutStorageKey);
    const chords = current ?? source.getItem(legacyChordStorageKey);
    const saved = JSON.parse(chords ?? source.getItem(legacyDigitStorageKey) ?? "null");
    if (!saved || typeof saved !== "object" || Array.isArray(saved)) return defaultChatShortcuts;
    const bindings: ChatShortcuts = {};
    for (const [key, action] of Object.entries(saved)) {
      const shortcut = chords === null && /^[1-9]$/.test(key) ? commandDigit(key) : key;
      if (!validShortcut(shortcut)) continue;
      const entry = action as { kind?: unknown; value?: unknown } | null;
      if (entry?.kind === "reset") {
        bindings[shortcut] = { kind: "reset" };
      } else if ((entry?.kind === "model" || entry?.kind === "effort") &&
          typeof entry.value === "string" && entry.value.length > 0 && entry.value.length <= 200) {
        bindings[shortcut] = { kind: entry.kind, value: entry.value };
      }
    }
    if (current === null) bindings[resetShortcut] ??= defaultChatShortcuts[resetShortcut];
    return bindings;
  } catch {
    return defaultChatShortcuts;
  }
}
