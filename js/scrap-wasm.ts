// Runs scrap-wasm.wasm, the scrap tool compiled to WebAssembly from
// cmd/scrap-wasm (see `pnpm build`), in the browser and tests alike.
// The yard's Worker in Victorystick/cloudflare-scrapyard runs a copy.

import { instantiate, type Instance } from './wasi.ts';

// The URL of scrap-wasm.wasm, next to this file. Bundlers such as Vite
// copy it into what they build.
export const wasmURL = new URL('./scrap-wasm.wasm', import.meta.url);

export interface WasmRequest {
  source: string;
  // The sources of the scraps that source refers to by hash, by hash.
  sources: Record<string, string>;
  // What to do with source if it checks, as the commands of the scrap CLI
  // do: only check it (the default), evaluate it, or run its tests. Or
  // "highlight" it, which needn't even parse: its ranges by tree-sitter
  // capture name, as start and end offsets into source.
  command?: Command;
  // How long evaluating may take, in milliseconds since the request, before
  // it stops with an error. No limit if absent.
  timeout?: number;
}

export type Command = 'type' | 'eval' | 'test' | 'highlight';

export type WasmResult =
  | { type: string; value?: string; results?: TestResult[]; error?: string }
  | { error: string }
  | { highlights: Record<string, number[]> }
  | { missing: string[] };

// The outcome of one case of a test scrap.
export interface TestResult {
  path: string;
  failed?: boolean;
  message?: string;
}

// printTests prints test results as `scrap test` does.
export function printTests(results: TestResult[]): string {
  let out = '';
  let failed = 0;
  for (const { path, failed: f, message = '' } of results) {
    if (!f) continue;
    failed++;
    out += ('FAIL ' + path).trim() + '\n';
    out += '    ' + message.replaceAll('\n', '\n    ') + '\n';
  }
  return out + `${failed} failed, ${results.length - failed} passed`;
}

interface Exports extends Instance {
  run(): number;
}

const encoder = new TextEncoder();
const decoder = new TextDecoder();

// A ScrapWasm checks and runs scraps with an instance of scrap-wasm.wasm.
// Starting the Go runtime takes far longer than a run, so hosts make one
// ScrapWasm and reuse it.
export class ScrapWasm {
  #module: WebAssembly.Module;
  #instance: Exports | null = null;

  constructor(module: WebAssembly.Module) {
    this.#module = module;
  }

  run(request: WasmRequest): WasmResult {
    const c = (this.#instance ??= instantiate<Exports>(this.#module));
    try {
      const input = encoder.encode(JSON.stringify(request));
      const ptr = c.alloc(input.length);
      new Uint8Array(c.memory.buffer, ptr, input.length).set(input);
      const n = c.run();
      return JSON.parse(
        decoder.decode(new Uint8Array(c.memory.buffer, c.output(), n))
      );
    } catch (e) {
      // A trap leaves the Go runtime broken; start afresh next time.
      this.#instance = null;
      throw e;
    }
  }
}
