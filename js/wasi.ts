// The WASI shim that our Go modules, scrap-wasm.wasm and scrap-web.wasm,
// run with. The yard's Worker in Victorystick/cloudflare-scrapyard runs a
// copy.

const decoder = new TextDecoder();

// The exports of a Go module built with -buildmode=c-shared for wasip1, as
// our modules have them: memory, the Go runtime's start, and a buffer
// for requests and one for responses.
export interface Instance {
  memory: WebAssembly.Memory;
  _initialize(): void;
  alloc(n: number): number;
  output(): number;
}

// instantiate starts an instance of a Go module, with just enough of WASI
// for the Go runtime.
export function instantiate<T extends Instance>(module: WebAssembly.Module): T {
  let exports: T;
  const memory = () => new DataView(exports.memory.buffer);
  const instance = new WebAssembly.Instance(module, {
    wasi_snapshot_preview1: wasi(memory),
  });
  exports = instance.exports as unknown as T;
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
      throw new Error('the module exited with status ' + code);
    },
  };
}
