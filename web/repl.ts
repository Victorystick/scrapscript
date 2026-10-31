/// <reference lib="dom" />
// Makes the examples on a page editable, and runs them in the browser with
// scrap-wasm.wasm, which prints what `scrap eval` and `scrap type` do, or
// `scrap test` for an example with data-run="test". Without this script,
// the examples show what they print as they are.
import type { RunRequest, RunResponse } from './repl-worker.ts';
import { printTests, type Command, type WasmResult } from '@scrapscript/web';

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

const runner = new Runner();

// enhance makes an example editable, with a button that runs it.
function enhance(example: HTMLElement) {
  const code = example.querySelector<HTMLElement>('pre:first-child > code')!;
  const out = example.querySelector<HTMLElement>('pre.out')!;
  const command = example.dataset.run === 'test' ? 'test' : 'eval';
  const original = code.textContent!;
  // The page is built with the original code's highlights.
  const built = JSON.parse(code.dataset.highlights ?? '{}');
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
    run();
  });
  reset.hidden = true;
  const runButton = button('Run', () => run());
  actions.append(hint, reset, runButton);
  code.parentElement!.after(actions);

  const share = example.hasAttribute('data-share');
  if (share && location.hash.length > 1) {
    code.textContent = decodeURIComponent(location.hash.slice(1));
    edited();
    rehighlight();
    run();
  }

  let timer: ReturnType<typeof setTimeout> | undefined;
  code.addEventListener('input', () => {
    edited();
    clearTimeout(timer);
    timer = setTimeout(rehighlight, 150);
  });
  code.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      run();
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
    out.classList.add('stale');
  }

  async function run() {
    const source = code.textContent!;
    if (share) {
      history.replaceState(null, '', '#' + encodeURIComponent(source));
    }
    runButton.disabled = true;
    out.classList.add('stale');
    const result = await runner.run(source, command);
    runButton.disabled = false;
    // Show only the latest run.
    if (code.textContent === source) {
      show(out, result);
    }
  }
}

// highlight replaces the highlights of code with spans: start and end
// offsets into its text, by the tree-sitter capture name that style.css
// colors with ::highlight(). It needs the CSS Custom Highlight API.
function highlight(code: HTMLElement, spans: Record<string, number[]>) {
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

function button(label: string, onclick: () => void): HTMLButtonElement {
  const b = document.createElement('button');
  b.type = 'button';
  b.textContent = label;
  b.onclick = onclick;
  return b;
}

// show shows result in out, as the static examples do.
function show(out: HTMLElement, result: WasmResult) {
  const samps: HTMLElement[] = [];
  const samp = (kind: string, text: string) => {
    const s = document.createElement('samp');
    s.className = kind;
    s.textContent = text;
    samps.push(s);
  };
  if ('results' in result && result.results != null) {
    // As `scrap test` prints them, without the type.
    samp('output', printTests(result.results));
  } else {
    if ('value' in result && result.value != null) samp('value', result.value);
    if ('error' in result && result.error != null) samp('error', result.error);
    if ('type' in result) samp('type', result.type);
  }
  out.replaceChildren(...samps);
  out.classList.remove('stale');
}

for (const example of document.querySelectorAll<HTMLElement>('.example')) {
  enhance(example);
}
