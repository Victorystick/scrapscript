// mount runs a scrapscript app in the Elm architecture in an element of
// the page: the web platform, whose interface is cmd/scrap-web/web.scrap.

import { ScrapWeb, webWasmURL, type HttpRequest } from './scrap-web.ts';

export interface MountOptions {
  // The app's scrap.
  source: string;
  // The flags that init gets: any JSON value. null if absent.
  flags?: unknown;
  // Fetches the source of a scrap that the app refers to by hash, or
  // returns null if there's none. By default, from the yard.
  fetchScrap?: (hash: string) => Promise<string | null>;
  // How long each step of the app may run, in milliseconds: starting it,
  // an update, or a view. 1000 by default; 0 for no limit. A step that
  // runs longer fails, and the page, which shares a thread with it, is
  // stuck until then.
  timeout?: number;
  // Reports a step that failed, after the app started: an update or a
  // view. The app goes on, as it was before the step. console.error by
  // default.
  onError?: (message: string) => void;
  // The runtime to run the app with, which apps on a page share; by
  // default, one started from webWasmURL.
  runtime?: ScrapWeb | Promise<ScrapWeb>;
}

// A running app.
export interface App {
  // stop stops the app: it gets no more events, and the page stays as it
  // last rendered.
  stop(): void;
}

// The yard that apps fetch scraps from by default.
const yard = 'https://scraps.oseg.dev/';

// The most scraps that an app may refer to, directly or not.
const maxImports = 100;

let shared: Promise<ScrapWeb> | null = null;

// runtime returns the runtime that pages share by default.
function runtime(): Promise<ScrapWeb> {
  return (shared ??= WebAssembly.compileStreaming(fetch(webWasmURL)).then(
    (module) => new ScrapWeb(module)
  ));
}

// mount starts the app in options.source in element, whose content it
// replaces. It fails if the app doesn't start: if it doesn't check against
// the web platform's interface, refers to scraps that can't be fetched,
// or its init fails.
export async function mount(element: Element, options: MountOptions): Promise<App> {
  const web = await (options.runtime ?? runtime());
  const fetchScrap = options.fetchScrap ?? fetchFromYard;
  const sources: Record<string, string> = {};
  const request = {
    source: options.source,
    sources,
    flags: options.flags ?? null,
    timeout: options.timeout ?? 1000,
  };
  for (;;) {
    const result = web.start(request);
    if ('handle' in result) {
      return new Running(element, web, result.handle, options.onError ?? console.error);
    }
    if ('error' in result) throw new Error(result.error);
    if (Object.keys(sources).length + result.missing.length > maxImports) {
      throw new Error(`An app may refer to at most ${maxImports} scraps`);
    }
    const fetched = await Promise.all(result.missing.map(fetchScrap));
    const absent = result.missing.filter((_, i) => fetched[i] == null);
    if (absent.length > 0) {
      throw new Error(
        'There are no scraps\n' + absent.map((key) => `$source:${key}`).join('\n')
      );
    }
    result.missing.forEach((key, i) => (sources[key] = fetched[i]!));
  }
}

// fetchFromYard fetches a scrap from the yard, or returns null if the yard
// lacks it.
async function fetchFromYard(key: string): Promise<string | null> {
  const response = await fetch(yard + key, {
    headers: { Accept: 'application/scrap' },
  });
  if (response.status === 404) return null;
  if (!response.ok) {
    throw new Error(`Fetching $source:${key} failed: ${response.status}`);
  }
  return response.text();
}

// A Running app renders its views into the page, passes it events, and
// carries out its commands and subscriptions. Events, responses and
// timers update the app at once, and it renders once per animation frame
// after them, just after its animation frame subscriptions.
class Running implements App {
  #web: ScrapWeb;
  #handle: number;
  #page: Page;
  #onError: (message: string) => void;
  #frame = 0;
  #stopped = false;
  // Aborts the app's requests when it stops.
  #abort = new AbortController();
  // The intervals of the app's #every subscriptions, by milliseconds.
  #timers = new Map<number, ReturnType<typeof setInterval>>();
  // Whether it subscribes to animation frames, and the time of the last.
  #animating = false;
  #last = 0;

  constructor(element: Element, web: ScrapWeb, handle: number, onError: (message: string) => void) {
    this.#web = web;
    this.#handle = handle;
    this.#onError = onError;
    element.replaceChildren();
    this.#page = new Page(element, (id, name, node) => this.#event(id, name, node));
    this.#render();
    this.#carryOut();
  }

  stop() {
    if (this.#stopped) return;
    this.#stopped = true;
    cancelAnimationFrame(this.#frame);
    this.#timers.forEach(clearInterval);
    this.#abort.abort();
    this.#page.detach();
    this.#web.stop(this.#handle);
  }

  #event(id: number, name: string, node: Node) {
    if (this.#stopped) return;
    const el = node as Partial<HTMLInputElement>;
    this.#step(() =>
      this.#web.event(this.#handle, id, name, typeof el.value === 'string' ? el.value : '', el.checked === true)
    );
  }

  // #step runs a step of the app, unless it has stopped, then carries out
  // its commands, and renders in the next animation frame.
  #step(step: () => void) {
    if (this.#stopped) return;
    try {
      step();
    } catch (e) {
      this.#onError(message(e));
    }
    this.#carryOut();
    this.#schedule();
  }

  #schedule() {
    this.#frame ||= requestAnimationFrame((time) => this.#onFrame(time));
  }

  #onFrame(time: number) {
    this.#frame = 0;
    if (this.#animating) {
      const delta = Math.max(0, time - this.#last);
      this.#last = time;
      try {
        this.#web.frame(this.#handle, delta);
      } catch (e) {
        this.#onError(message(e));
      }
      this.#carryOut();
    }
    this.#render();
  }

  #carryOut() {
    let commands;
    try {
      commands = this.#web.commands(this.#handle);
    } catch (e) {
      this.#onError(message(e));
      return;
    }
    for (const c of commands) {
      if (c.http) this.#fetch(c.id, c.http);
      else setTimeout(() => this.#step(() => this.#web.wake(this.#handle, c.id)));
    }
  }

  // #fetch carries out a request, and passes the app its response, or why
  // it failed, unless the app stops first.
  async #fetch(id: number, r: HttpRequest) {
    let status = 0;
    let headers: [string, string][] = [];
    let body: Uint8Array;
    try {
      const response = await fetch(r.url, {
        method: r.method,
        headers: r.headers,
        body: r.bytes != null ? fromBase64(r.bytes) : r.text,
        signal: this.#abort.signal,
      });
      body = new Uint8Array(await response.arrayBuffer());
      status = response.status;
      headers = [...response.headers];
    } catch (e) {
      body = encoder.encode(message(e));
    }
    this.#step(() => this.#web.respond(this.#handle, id, status, headers, body));
  }

  #render() {
    if (this.#stopped) return;
    try {
      this.#page.apply(this.#web.render(this.#handle));
    } catch (e) {
      this.#onError(message(e));
    }
    this.#subscribe();
  }

  // #subscribe starts and stops timers for the app's subscriptions, for
  // its model now.
  #subscribe() {
    let timers;
    try {
      timers = this.#web.subscriptions(this.#handle);
    } catch (e) {
      this.#onError(message(e));
    }
    if (timers != null) {
      for (const [ms, timer] of this.#timers) {
        if (!timers.every.includes(ms)) {
          clearInterval(timer);
          this.#timers.delete(ms);
        }
      }
      for (const ms of timers.every) {
        if (!this.#timers.has(ms)) {
          // setInterval takes at most 2^31 - 1 ms, and runs longer ones at once.
          const every = Math.min(ms, 2 ** 31 - 1);
          this.#timers.set(ms, setInterval(() => this.#step(() => this.#web.tick(this.#handle, ms)), every));
        }
      }
      if (timers.frame && !this.#animating) this.#last = performance.now();
      this.#animating = timers.frame;
    }
    if (this.#animating) this.#schedule();
  }
}

function message(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

function fromBase64(s: string): Uint8Array {
  return Uint8Array.from(atob(s), (c) => c.charCodeAt(0));
}

const encoder = new TextEncoder();

// The ops of patches, as numbered in cmd/scrap-web/patch.go.
const Op = {
  CreateElement: 1,
  CreateText: 2,
  SetText: 3,
  SetAttr: 4,
  RemoveAttr: 5,
  SetProp: 6,
  RemoveProp: 7,
  SetStyle: 8,
  RemoveStyle: 9,
  Listen: 10,
  Unlisten: 11,
  Append: 12,
  Replace: 13,
  Remove: 14,
  Insert: 15,
} as const;

const decoder = new TextDecoder();

// A Page applies patches to the DOM in an element, whose id is 0: the
// nodes it made, by id, and the events that each listens to. It listens
// to each event once, on the element, in the capture phase, so as to see
// events that don't bubble too. It passes an event to each node from the
// target up that listens to it, or only to the target if the event
// doesn't bubble, as the DOM would.
class Page {
  #root: Element;
  #nodes: (Node | undefined)[];
  #events: (Set<string> | undefined)[] = [];
  #ids = new WeakMap<Node, number>();
  #listening = new Set<string>();
  #dispatch: (id: number, name: string, node: Node) => void;

  constructor(root: Element, dispatch: (id: number, name: string, node: Node) => void) {
    this.#root = root;
    this.#nodes = [root];
    this.#dispatch = dispatch;
  }

  // detach stops listening to events.
  detach() {
    for (const name of this.#listening) {
      this.#root.removeEventListener(name, this.#listener, { capture: true });
    }
    this.#listening.clear();
  }

  #listener = (e: Event) => {
    if (e.type === 'submit') e.preventDefault();
    const hits: [number, Node][] = [];
    for (let node = e.target as Node | null; node != null && node !== this.#root; node = node.parentNode) {
      const id = this.#ids.get(node);
      if (id !== undefined && this.#nodes[id] === node && this.#events[id]?.has(e.type)) {
        hits.push([id, node]);
      }
      if (!e.bubbles) break;
    }
    for (const [id, node] of hits) this.#dispatch(id, e.type, node);
  };

  apply(patch: Uint8Array) {
    const view = new DataView(patch.buffer, patch.byteOffset, patch.byteLength);
    const nWords = view.getUint32(0, true);
    const nStrings = view.getUint32(4, true);
    const words = new Uint32Array(patch.buffer, patch.byteOffset + 8, nWords);
    const strings: string[] = new Array(nStrings);
    let at = 8 + 4 * (nWords + nStrings);
    for (let i = 0; i < nStrings; i++) {
      const n = view.getUint32(8 + 4 * (nWords + i), true);
      strings[i] = decoder.decode(patch.subarray(at, at + n));
      at += n;
    }

    const nodes = this.#nodes;
    const el = (id: number) => nodes[id] as HTMLElement;
    for (let i = 0; i < words.length; ) {
      const op = words[i++];
      const id = words[i++];
      switch (op) {
        case Op.CreateElement:
        case Op.CreateText: {
          const s = strings[words[i++]];
          const node = op === Op.CreateText ? document.createTextNode(s) : document.createElement(s);
          nodes[id] = node;
          this.#events[id] = undefined;
          this.#ids.set(node, id);
          break;
        }
        case Op.SetText:
          nodes[id]!.nodeValue = strings[words[i++]];
          break;
        case Op.SetAttr:
          el(id).setAttribute(strings[words[i++]], strings[words[i++]]);
          break;
        case Op.RemoveAttr:
          el(id).removeAttribute(strings[words[i++]]);
          break;
        case Op.SetProp: {
          const name = strings[words[i++]];
          (el(id) as any)[name] = JSON.parse(strings[words[i++]]);
          break;
        }
        case Op.RemoveProp: {
          const name = strings[words[i++]];
          const e = el(id) as any;
          e[name] = typeof e[name] === 'string' ? '' : null;
          break;
        }
        case Op.SetStyle:
          el(id).style.setProperty(strings[words[i++]], strings[words[i++]]);
          break;
        case Op.RemoveStyle:
          el(id).style.removeProperty(strings[words[i++]]);
          break;
        case Op.Listen: {
          const name = strings[words[i++]];
          (this.#events[id] ??= new Set()).add(name);
          if (!this.#listening.has(name)) {
            this.#listening.add(name);
            this.#root.addEventListener(name, this.#listener, { capture: true });
          }
          break;
        }
        case Op.Unlisten:
          this.#events[id]?.delete(strings[words[i++]]);
          break;
        case Op.Append:
          el(id).appendChild(nodes[words[i++]]!);
          break;
        case Op.Replace:
          (nodes[id] as ChildNode).replaceWith(nodes[words[i++]]!);
          break;
        case Op.Remove:
          (nodes[id] as ChildNode).remove();
          break;
        case Op.Insert: {
          const child = nodes[words[i++]]!;
          const before = words[i++];
          el(id).insertBefore(child, before === 0 ? null : nodes[before]!);
          break;
        }
        default:
          throw new Error(`unknown op ${op} in a patch`);
      }
    }
  }
}
