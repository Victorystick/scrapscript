// Runs scrap-wasm.wasm, the scrap tool compiled to WebAssembly from
// cmd/scrap-wasm (see `pnpm build`), in the browser and tests alike.
// The yard's Worker in Victorystick/cloudflare-scrapyard runs a copy.

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

interface Exports {
  memory: WebAssembly.Memory;
  _initialize(): void;
  alloc(n: number): number;
  run(): number;
  output(): number;
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
    const c = (this.#instance ??= start(this.#module));
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

function start(module: WebAssembly.Module): Exports {
  let exports: Exports;
  const memory = () => new DataView(exports.memory.buffer);
  const instance = new WebAssembly.Instance(module, {
    wasi_snapshot_preview1: wasi(memory),
  });
  exports = instance.exports as unknown as Exports;
  exports._initialize();
  return exports;
}

const ESUCCESS = 0;
const EBADF = 8;
const ENOSYS = 52;

// wasi implements what the Go runtime imports from WASI preview 1, for a
// module that has no arguments, environment, files or sleeping, and only
// writes to stdout and stderr, as when it panics.
function wasi(memory: () => DataView) {
  const zero = (...ptrs: number[]) => {
    for (const p of ptrs) memory().setUint32(p, 0, true);
    return ESUCCESS;
  };
  const lines: Record<number, string> = { 1: '', 2: '' };

  return {
    args_sizes_get: zero,
    args_get: () => ESUCCESS,
    environ_sizes_get: zero,
    environ_get: () => ESUCCESS,
    clock_time_get(_id: number, _precision: bigint, ptr: number) {
      memory().setBigUint64(ptr, BigInt(Date.now()) * 1_000_000n, true);
      return ESUCCESS;
    },
    random_get(ptr: number, len: number) {
      const buf = new Uint8Array(memory().buffer, ptr, len);
      for (let i = 0; i < len; i += 65536) {
        crypto.getRandomValues(buf.subarray(i, i + 65536));
      }
      return ESUCCESS;
    },
    fd_write(fd: number, iovs: number, iovsLen: number, nwritten: number) {
      if (!(fd in lines)) return EBADF;
      const view = memory();
      let n = 0;
      for (let i = 0; i < iovsLen; i++) {
        const ptr = view.getUint32(iovs + 8 * i, true);
        const len = view.getUint32(iovs + 8 * i + 4, true);
        lines[fd] += decoder.decode(new Uint8Array(view.buffer, ptr, len));
        n += len;
      }
      const parts = lines[fd].split('\n');
      lines[fd] = parts.pop()!;
      for (const line of parts) console.error(line);
      view.setUint32(nwritten, n, true);
      return ESUCCESS;
    },
    fd_fdstat_get(fd: number, ptr: number) {
      if (fd > 2) return EBADF;
      new Uint8Array(memory().buffer, ptr, 24).fill(0);
      memory().setUint8(ptr, 2); // A character device.
      return ESUCCESS;
    },
    fd_fdstat_set_flags: () => ESUCCESS,
    fd_read: () => EBADF,
    fd_close: () => EBADF,
    fd_prestat_get: () => EBADF,
    fd_prestat_dir_name: () => EBADF,
    poll_oneoff: () => ENOSYS,
    sched_yield: () => ESUCCESS,
    proc_exit(code: number) {
      throw new Error('the checker exited with status ' + code);
    },
  };
}
