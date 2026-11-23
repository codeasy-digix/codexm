import React, { memo, useMemo, useState } from "react";
import Markdown, { defaultUrlTransform, type Components } from "react-markdown";
import type { Link, Parent, Root, RootContent, Text } from "mdast";
import remarkGfm from "remark-gfm";
import { call } from "../../client/data";
import { artifactURL, type ChatArtifact, type ChatItem, type ChatTurn, type RelayedInput } from "../contracts/web";
import { FileLink } from "./ChatFiles";

const markdownComponents: Components = {
  table({ node: _node, ...props }) {
    return <div className="markdown-table-scroll" role="region" aria-label="채팅 표" tabIndex={0}>
      <table {...props} />
    </div>;
  },
  th({ node: _node, ...props }) {
    return <th scope="col" {...props} />;
  },
};

export function inputText(content?: Array<{type: string; text?: string}>) {
  return (content || []).map(part => part.text || (part.type === "image" || part.type === "localImage" ? "[이미지 첨부]" :
    ["audio", "localAudio", "file", "localFile", "video", "localVideo"].includes(part.type) ? "[파일 첨부]" : "")).filter(Boolean).join("\n");
}

function detailTitle(item: ChatItem) {
  if (item.type === "commandExecution") return String(item.command || "명령 실행").split("\n")[0].slice(0,100);
  if (item.type === "mcpToolCall") return `${item.server || "도구"} · ${item.tool || "호출"}`;
  if (item.type === "fileChange") return "파일 변경";
  if (item.type === "reasoning") return "검토 과정";
  if (item.type === "agentMessage") return "진행 안내";
  return item.type;
}

type ItemEntry = { item: ChatItem; outputDeferred?: boolean };
function ToolItem({entry,thread,turn,account}:{entry:ItemEntry;thread:string;turn:string;account:string}) {
  const [full,setFull]=useState<ChatItem>(),[loading,setLoading]=useState(false),[error,setError]=useState("");
  const item=full||entry.item;
  async function expand(open:boolean) {
    if (!open || !entry.outputDeferred || full || loading) return;
    setLoading(true);setError("");
    try {const reply=await call<{data:ItemEntry[]}>("web.item",{thread_id:thread,turn_id:turn,item_id:item.id,account});setFull(reply.data[0]?.item);}
    catch(e){setError((e as Error).message);}finally{setLoading(false);}
  }
  const output = item.type === "agentMessage" ? item.text : item.aggregatedOutput;
  return <details className="tool-item" onToggle={e=>void expand(e.currentTarget.open)}>
    <summary><span>{detailTitle(item)}</span><small>{String(item.status||"")}</small></summary>
    {loading?<p className="muted" role="status">출력을 불러오는 중…</p>:error?<p role="alert">{error}</p>:typeof output==="string"?<pre>{output}</pre>:<pre>{JSON.stringify(item,null,2)}</pre>}
  </details>;
}

function TurnDetails({thread,turn,account,external,current}:{thread:string;turn:ChatTurn;account:string;external?:boolean;current:boolean}) {
  const [entries,setEntries]=useState<ItemEntry[]>([]),[cursor,setCursor]=useState<string|null>(),[started,setStarted]=useState(false),[loading,setLoading]=useState(false),[error,setError]=useState("");
  async function load(next?:string|null) {
    if (loading) return;setLoading(true);setError("");
    try {const page=await call<{data:ItemEntry[];nextCursor?:string|null}>("web.items",{thread_id:thread,turn_id:turn.id,account,...(next?{cursor:next}:{})});setEntries(old=>next?[...old,...page.data]:page.data);setCursor(page.nextCursor);setStarted(true);}
    catch(e){setError((e as Error).message);}finally{setLoading(false);}
  }
  const displayed = new Set(turn.items.filter(item => item.type === "agentMessage" && item.text).map(item => item.id));
  const details = entries.filter(({item}) => item.type !== "userMessage" &&
    (item.type !== "agentMessage" || (item.phase === "commentary" && !displayed.has(item.id))));
  return <details className="turn-details" onToggle={e=>{if(e.currentTarget.open&&!started&&!loading)void load();}}>
    <summary>작업 과정 <span>{current||turn.status==="inProgress"?"진행 중":turn.status==="interrupted"?(external?"원래 창에서 상태 확인":"중단됨"):"보기"}</span></summary>
    {error&&<p role="alert">{error}</p>}
    {details.map((entry,i)=><ToolItem key={`${entry.item.id}-${i}`} entry={entry} thread={thread} turn={turn.id} account={account}/>)}
    {started&&!details.length&&!loading&&<p className="muted">별도 작업 과정이 없습니다.</p>}
    {loading&&<p role="status">작업 과정을 불러오는 중…</p>}
    {cursor&&<button className="text-button" onClick={()=>void load(cursor)} disabled={loading}>과정 더 보기</button>}
  </details>;
}

function UserMessage({ text }: { text: string }) {
  return <div className="user-message"><span className="sr-only">나</span><p>{text}</p></div>;
}

function messageFiles(thread: string, cwd: string, files: ChatArtifact[], refs: Map<string, string>) {
  const byID = new Map(files.map(file => [file.id, file]));
  const paths = new Map(files.map(file => [file.source + ":" + file.path, file]));
  const links = new Map(files.map(file => [artifactURL(thread, file.id), file]));
  const find = (value: unknown) => {
    if (typeof value !== "string") return;
    const exact = refs.get(value) || refs.get(value.trim());
    if (exact) return byID.get(exact);
    let path = value.replace(/^file:\/\/(?:localhost)?(?=\/)|^sandbox:(?=\/(?!\/))/i, "").replace(/(?:#L\d+(?:-L?\d+)?|:\d+(?::\d+)?)$/, "");
    if (/^(?:[a-z][\w+.-]*:|\/\/)/i.test(path)) return;
    try { path = decodeURIComponent(path); } catch { /* This can be a literal filesystem path. */ }
    const decoded = refs.get(path);
    if (decoded) return byID.get(decoded);
    const marker = "/visualizations/";
    const parts = path.slice(path.lastIndexOf(marker) + marker.length).split("/");
    if (path.includes(marker) && parts[3] === thread) return paths.get("visualization:" + parts.join("/"));
    const generated = `/generated_images/${thread}/`;
    if (path.includes(generated)) return paths.get("generated:" + thread + "/" + path.split(generated).at(-1));
    const root = cwd.replace(/\/+$/, "") + "/";
    if (path.startsWith("/")) {
      if (!path.startsWith(root)) return;
      path = path.slice(root.length);
    }
    return paths.get("workspace:" + path.replace(/^(?:\.\/)+/, ""));
  };
  const urlTransform = (url: string) => {
    if (links.has(url)) return url;
    const file = find(url);
    if (file) return artifactURL(thread, file.id);
    if (/^(?:\/(?!\/)|file:|sandbox:|\.\.?\/|~\/)/i.test(url) ||
        !url.startsWith("//") && /^[^:#?]+\.[a-z0-9]{1,16}(?:#L\d+)?$/i.test(url)) return "";
    return defaultUrlTransform(url);
  };
  const components: Components = {
    ...markdownComponents,
    a({ node: _node, href, children, ...props }) {
      const file = href ? links.get(href) : undefined;
      return file ? <FileLink thread={thread} file={file} label={children} preview /> :
        href ? <a href={href} {...props}>{children}</a> : <span>{children}</span>;
    },
    img({ node: _node, src, alt, ...props }) {
      const file = typeof src === "string" ? links.get(src) : undefined;
      if (file) return <FileLink thread={thread} file={file} alt={alt} preview />;
      if (src) return <img src={src} alt={alt} loading="lazy" {...props} />;
      return <span>{alt}</span>;
    },
  };
  return { find, byID, components, urlTransform, thread };
}

const fileTextPath = /(^|[\s<(])((?:(?:file:\/\/(?:localhost)?|sandbox:)?\/|[~.]\/)[^\s<>'"`\])]+\.[A-Za-z0-9]{1,16}(?::\d+(?::\d+)?|#L\d+(?:-L?\d+)?)?)(?=$|[\s.,;>)\]])/g;

function linkifyFilePaths(root: Root, media: ReturnType<typeof messageFiles>) {
  function link(node: Link["children"][number], value: string): Link | undefined {
    const file = media.find(value);
    if (file) return { type: "link", url: artifactURL(media.thread, file.id), children: [node] };
  }
  function textLinks(node: Text): RootContent[] {
    const parts: RootContent[] = [];
    let offset = 0;
    for (const match of node.value.matchAll(fileTextPath)) {
      const [, prefix, value] = match;
      const item = link({ type: "text", value }, value);
      if (!item) continue;
      const start = match.index + prefix.length;
      if (start > offset) parts.push({ type: "text", value: node.value.slice(offset, start) });
      parts.push(item);
      offset = start + value.length;
    }
    if (parts.length === 0) return [node];
    if (offset < node.value.length) parts.push({ type: "text", value: node.value.slice(offset) });
    return parts;
  }
  function visit(node: Parent) {
    node.children = node.children.flatMap<RootContent>(child => {
      if (child.type === "text") return textLinks(child);
      if (child.type === "inlineCode") return [link(child, child.value) ?? child];
      if ("children" in child && child.type !== "link" && child.type !== "linkReference") visit(child);
      return [child];
    });
  }
  visit(root);
}

const MessageMarkdown = memo(function MessageMarkdown({ body, media }: { body: string; media: ReturnType<typeof messageFiles> }) {
  const fileLinks = useMemo(() => () => (tree: Root) => linkifyFilePaths(tree, media), [media]);
  const text = body.replace(/visualize([^\r\n]*?)/g, (_marker, payload) => {
    try {
      const file = media.find(JSON.parse(payload)?.path);
      if (file) return "[" + file.name.replace(/[\\\[\]]/g, "\\$&") + "](" + artifactURL(media.thread, file.id) + ")";
    } catch { /* Leave unavailable file references readable. */ }
    return "생성 파일 · 채팅 메뉴에서 확인";
  });
  return <Markdown remarkPlugins={[remarkGfm, fileLinks]} components={media.components} urlTransform={media.urlTransform}>{text}</Markdown>;
});

export function ChatMessages({thread,turns,account,external,liveTurnId,relayedInputs,hasOlder,cwd,files,refs}:{
  thread:string;turns:ChatTurn[];account:string;external?:boolean;liveTurnId?:string;
  relayedInputs?:RelayedInput[] | null;hasOlder:boolean;cwd:string;files:ChatArtifact[];refs:Map<string, string>;
}) {
  const media = useMemo(() => messageFiles(thread, cwd, files, refs), [thread, cwd, files, refs]);
  const relayedByTurn = new Map<string, RelayedInput[]>();
  const after: RelayedInput[] = [];
  const newestFirst = [...turns].reverse();
  for (const input of relayedInputs ?? []) {
    const sent = Math.floor(input.sentAt / 1000);
    const accepted = Math.floor(input.acceptedAt / 1000);
    const observed = turns.find(turn => turn.id === input.observedTurnId);
    if (!observed && hasOlder && turns[0]?.startedAt != null && sent < turns[0].startedAt) continue;
    // The GUI acknowledgement identifies the chat. If the turn changed
    // during delivery, use its native time range to place the message.
    const turn = observed && (observed.completedAt == null || observed.completedAt >= accepted) ? observed :
      newestFirst.find(turn => turn.startedAt != null && turn.startedAt <= accepted &&
        (turn.completedAt == null || turn.completedAt >= sent)) ?? observed ??
        turns.find(turn => turn.startedAt != null && turn.startedAt > accepted);
    if (turn) {
      const messages = relayedByTurn.get(turn.id) ?? [];
      messages.push(input);
      relayedByTurn.set(turn.id, messages);
    } else {
      after.push(input);
    }
  }
  return <div className="turns">{turns.map(turn => {
    const messages = turn.items.filter(item => item.type === "fileOutput" || item.type === "contextCompaction" ||
      item.type === "exitedReviewMode" || (item.type === "agentMessage" || item.type === "plan") && item.text);
    const ordered = [...messages.filter(item => item.phase === "commentary"), ...messages.filter(item => item.phase !== "commentary")];
    const linked = new Set(Object.values(turn.fileRefs ?? {}));
    return <article className="turn" key={turn.id} data-turn-id={turn.id}>
      {turn.items.filter(item => item.type === "userMessage").map(item => {
        const attachments = new Map<number, React.ReactNode>();
        for (const [index, part] of (item.content ?? []).entries()) {
          const file = part.artifactId ? media.byID.get(part.artifactId) : media.find(part.path);
          if (file) attachments.set(index, <FileLink key={index} thread={thread} file={file} preview />);
          else if (part.type === "image" && part.url?.startsWith("https://")) {
            attachments.set(index, <a key={index} className="chat-file-link" href={part.url} target="_blank" rel="noreferrer">
              <img src={part.url} alt="이미지 첨부" loading="lazy" referrerPolicy="no-referrer" />
            </a>);
          }
        }
        if (!attachments.size) return <UserMessage key={item.id} text={inputText(item.content)} />;
        const text = inputText(item.content?.filter((_part, i) => !attachments.has(i)));
        return <div key={item.id} className="user-message attached-message">
          <span className="sr-only">나</span>
          {text && <p>{text}</p>}
          {[...attachments.values()]}
        </div>;
      })}
      {relayedByTurn.get(turn.id)?.map(input=><UserMessage key={input.id} text={input.text}/>)}
      <div className="assistant-message">
        {ordered.map(item => {
          if (item.type === "contextCompaction") {
            return <p className="muted" key={item.id}>{turn.status === "completed" ? "컨텍스트를 압축했습니다." :
              turn.status === "inProgress" ? "컨텍스트를 압축하고 있습니다…" : "컨텍스트 압축 작업"}</p>;
          }
          if (item.type === "fileOutput") {
            const ids = item.artifactIds ?? [];
            const output = ids.filter(id => !linked.has(id)).flatMap(id => media.byID.get(id) ?? []);
            if (ids.length && !output.length && ids.every(id => linked.has(id))) return null;
            return <div className="file-output" key={item.id}>{output.length
              ? output.map(file => <FileLink key={file.id} thread={thread} file={file} preview />)
              : <small className="muted">첨부 파일 · 원본을 확인할 수 없습니다.</small>}</div>;
          }
          const label = item.type === "plan" ? "계획" : item.type === "exitedReviewMode" ? "리뷰 결과" : "";
          const body = typeof item.review === "string" ? item.review : item.text || "";
          if (!body) return null;
          const classes = `${item.streaming ? "live-message" : "markdown"}${item.phase === "commentary" ? " commentary-message" : ""}`;
          return <div className={classes} key={item.id}>
            {label && <strong className="chat-command-result">{label}</strong>}
            {item.streaming ? body : <MessageMarkdown body={body} media={media} />}
          </div>;
        })}
        {turn.error&&<p role="alert" className="inline-error">{turn.error.message}</p>}
        <TurnDetails thread={thread} turn={turn} account={account} external={external} current={turn.id === liveTurnId}/>
      </div>
    </article>;
  })}{after.map(input=><UserMessage key={input.id} text={input.text}/>)}</div>;
}
