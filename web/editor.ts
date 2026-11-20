/// <reference lib="dom" />
// Makes a .example on a page an editor: its code becomes editable, and is
// highlighted with scrap-wasm.wasm in a Web Worker as it is edited, with
// actions to run it and to reset it. repl.ts runs the examples it makes,
// and app.ts mounts them as apps.
import type { RunRequest, RunResponse } from './repl-worker.ts';
import type { Command, WasmResult } from '@scrapscript/web/scrap-wasm';

// How long a scrap may run before the page stops the worker, in case it is
// still fetching scraps or stuck. The worker stops evaluating it a little
// earlier, so that its error usually comes first and the worker lives on.
const timeout = 5000;
const evalTimeout = timeout - 100;

// A Runner runs scraps in a Web Worker, which it replaces when a scrap
// runs too long.
class Runner {
  #worker: Worker | null = null;
  #ready: Promise<void> = Promise.resolve();
  #next = 0;
  #pending = new Map<number, (result: WasmResult) => void>();

  async run(source: string, command: Command): Promise<WasmResult> {
    const worker = (this.#worker ??= this.#start());
    try {
      await this.#ready;
    } catch (e) {
      this.#worker = null;
      return { error: String(e) };
    }
    const id = this.#next++;
    return new Promise((resolve) => {
      const timer = setTimeout(() => {
        this.#stop(`Stopped after ${timeout / 1000} seconds.`);
      }, timeout);
      this.#pending.set(id, (result) => {
        clearTimeout(timer);
        resolve(result);
      });
      worker.postMessage({
        id,
        source,
        command,
        timeout: evalTimeout,
      } satisfies RunRequest);
    });
  }

  #start(): Worker {
    const worker = new Worker(new URL('./repl-worker.ts', import.meta.url), {
      type: 'module',
    });
    this.#ready = new Promise((resolve, reject) => {
      worker.onmessage = ({ data }: MessageEvent<RunResponse>) => {
        if ('id' in data) {
          this.#pending.get(data.id)?.(data.result);
          this.#pending.delete(data.id);
        } else if (data.ready) {
          resolve();
        } else {
          reject(new Error('Starting scrap failed: ' + data.error));
        }
      };
      worker.onerror = (e) => {
        reject(new Error('Starting scrap failed: ' + e.message));
      };
    });
    return worker;
  }

  // stop stops the worker, failing every scrap it was running.
  #stop(error: string) {
    this.#worker?.terminate();
    this.#worker = null;
    for (const resolve of this.#pending.values()) resolve({ error });
    this.#pending.clear();
  }
}

export const runner = new Runner();

// An Editor is an example made editable.
export interface Editor {
  // The code element, whose text is the scrap.
  code: HTMLElement;
  // The bar of actions under the code, which pages may add to.
  actions: HTMLElement;
  // set replaces the scrap with source, as if typed, and runs it.
  set(source: string): void;
  // load replaces the scrap with source, which Reset then goes back to,
  // and runs it.
  load(source: string): void;
}

// editable makes an example editable, with a button that runs it with
// run, which gets the scrap and whether it is still the scrap edited. The
// example's code may hold highlights from the build, as data-highlights.
// With data-share, the address bar keeps the scrap last run, and the page
// starts with the scrap in it, if any.
export function editable(
  example: HTMLElement,
  run: (source: string, current: () => boolean) => Promise<void>,
  onEdit: () => void = () => {}
): Editor {
  const code = example.querySelector<HTMLElement>('pre:first-child > code')!;
  let original = code.textContent!;
  // The page is built with the original code's highlights.
  let built = JSON.parse(code.dataset.highlights ?? '{}');
  highlight(code, built);

  try {
    code.contentEditable = 'plaintext-only';
  } catch {
    code.contentEditable = 'true';
  }
  code.spellcheck = false;
  code.setAttribute('aria-label', 'Scrap');

  const actions = document.createElement('div');
  actions.className = 'actions';
  const hint = document.createElement('span');
  hint.textContent = navigator.platform.startsWith('Mac') ? '⌘↵' : 'Ctrl+↵';
  const reset = button('Reset', () => {
    code.textContent = original;
    highlight(code, built);
    edited();
    start();
  });
  reset.hidden = true;
  const runButton = button('Run', () => start());
  actions.append(hint, reset, runButton);
  code.parentElement!.after(actions);

  const share = example.hasAttribute('data-share');

  let timer: ReturnType<typeof setTimeout> | undefined;
  code.addEventListener('input', () => {
    edited();
    clearTimeout(timer);
    timer = setTimeout(rehighlight, 150);
  });
  code.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      start();
    }
  });

  // rehighlight highlights the code as it is, if it hasn't changed by the
  // time the worker answers. Until then, the last highlights move with the
  // edits.
  async function rehighlight() {
    const source = code.textContent!;
    const result = await runner.run(source, 'highlight');
    if ('highlights' in result && code.textContent === source) {
      highlight(code, result.highlights);
    }
  }

  function edited() {
    reset.hidden = code.textContent === original;
    onEdit();
  }

  async function start() {
    const source = code.textContent!;
    if (share) {
      history.replaceState(null, '', '#' + encodeURIComponent(source));
    }
    runButton.disabled = true;
    try {
      await run(source, () => code.textContent === source);
    } finally {
      runButton.disabled = false;
    }
  }

  const editor: Editor = {
    code,
    actions,
    set(source: string) {
      code.textContent = source;
      edited();
      rehighlight();
      start();
    },
    load(source: string) {
      original = source;
      built = {};
      editor.set(source);
    },
  };
  if (share && location.hash.length > 1) {
    editor.set(decodeURIComponent(location.hash.slice(1)));
  }
  return editor;
}

// highlight replaces the highlights of code with spans: start and end
// offsets into its text, by the tree-sitter capture name that style.css
// colors with ::highlight(). It needs the CSS Custom Highlight API.
export function highlight(code: HTMLElement, spans: Record<string, number[]>) {
  if (!('highlights' in CSS)) return;
  // Ranges whose text was replaced have moved to code itself.
  for (const ranges of CSS.highlights.values()) {
    for (const range of ranges) {
      if (code.contains(range.startContainer)) ranges.delete(range);
    }
  }
  // Editing may split the text into several nodes.
  const texts: Text[] = [];
  const walker = document.createTreeWalker(code, NodeFilter.SHOW_TEXT);
  while (walker.nextNode()) texts.push(walker.currentNode as Text);
  const at = (offset: number): [Text, number] => {
    for (const text of texts) {
      if (offset <= text.length) return [text, offset];
      offset -= text.length;
    }
    throw new RangeError('offset past the code');
  };
  for (const [name, offsets] of Object.entries(spans)) {
    let ranges = CSS.highlights.get(name);
    if (!ranges) CSS.highlights.set(name, (ranges = new Highlight()));
    for (let i = 0; i < offsets.length; i += 2) {
      const range = new Range();
      range.setStart(...at(offsets[i]));
      range.setEnd(...at(offsets[i + 1]));
      ranges.add(range);
    }
  }
}

export function button(label: string, onclick: () => void): HTMLButtonElement {
  const b = document.createElement('button');
  b.type = 'button';
  b.textContent = label;
  b.onclick = onclick;
  return b;
}

