// Runs the page's requests in scrap-web.wasm, in a Web Worker, off the
// page's thread, so that an app that loops forever doesn't freeze the page:
// the page stops the worker instead. The module serves requests as the
// package host/wasmhost describes, and keeps the app that it loaded between
// them. The worker fetches the scraps that a request lacks, and asks again
// with them.

const encoder = new TextEncoder();
const decoder = new TextDecoder();

// The module, which the worker says once whether it compiled, and its
// instance. Starting the Go runtime takes far longer than a request, so the
// worker keeps one, until a trap breaks it.
const module = fetch('./scrap-web.wasm').then(async (res) => {
  if (!res.ok) throw new Error(await res.text());
  return WebAssembly.compileStreaming(res);
});
let instance = null;
module.then(
  () => postMessage({ ready: true }),
  (e) => postMessage({ ready: false, error: String(e.message ?? e) })
);

// The page posts { id, request }, and the worker answers { id, result }.
onmessage = async ({ data }) => {
  let result;
  try {
    result = await run(await module, data.request);
  } catch (e) {
    result = { error: String(e) };
  }
  postMessage({ id: data.id, result });
};

// The sources of the scraps fetched so far, by hash. A hash names one
// source forever, so they never go stale.
const sources = {};

// The most scraps that a scrap may refer to, as a yard allows.
const maxImports = 256;

// run runs the request, and again with the scraps that it lacks, which it
// only then passes, as most requests lack none.
async function run(module, request) {
  let count = 0;
  for (;;) {
    const result = call(module, count == 0 ? request : { ...request, sources });
    if (!result.missing) return result;
    count += result.missing.length;
    if (count > maxImports) {
      return { error: `A scrap may refer to at most ${maxImports} scraps` };
    }
    const wanted = result.missing.filter((key) => !(key in sources));
    const fetched = await Promise.all(wanted.map(fetchScrap));
    const absent = wanted.filter((_, i) => fetched[i] == null);
    if (absent.length > 0) {
      return {
        error: 'The yard lacks the scraps it refers to:\n' + absent.map((key) => `$source:${key}`).join('\n'),
      };
    }
    wanted.forEach((key, i) => (sources[key] = fetched[i]));
  }
}

// fetchScrap fetches a scrap, which the server fetches from a yard, or
// returns null if the yard lacks it.
async function fetchScrap(key) {
  const res = await fetch('./' + key);
  if (res.status === 404) return null;
  if (!res.ok) throw new Error(`Fetching $source:${key} failed: ${res.status}`);
  return res.text();
}

// call writes the request into the module's memory, runs it, and reads the
// response.
function call(module, request) {
  const exports = (instance ??= start(module));
  try {
    const input = encoder.encode(JSON.stringify(request));
    const ptr = exports.alloc(input.length);
    new Uint8Array(exports.memory.buffer, ptr, input.length).set(input);
    const n = exports.run();
    return JSON.parse(decoder.decode(new Uint8Array(exports.memory.buffer, exports.output(), n)));
  } catch (e) {
    // A trap leaves the Go runtime broken; start afresh next time, which
    // answers that it has no app.
    instance = null;
    throw e;
  }
}

function start(module) {
  let exports;
  const memory = () => new DataView(exports.memory.buffer);
  const started = new WebAssembly.Instance(module, { wasi_snapshot_preview1: wasi(memory) });
  exports = started.exports;
  exports._initialize();
  return exports;
}

const ESUCCESS = 0;
const EBADF = 8;
const ENOSYS = 52;

// wasi implements what the Go runtime imports from WASI preview 1, for a
// module that has no arguments, environment, files or sleeping, and only
// writes to stdout and stderr, as when it panics.
function wasi(memory) {
  const zero = (...ptrs) => {
    for (const p of ptrs) memory().setUint32(p, 0, true);
    return ESUCCESS;
  };
  const lines = { 1: '', 2: '' };
  return {
    args_sizes_get: zero,
    args_get: () => ESUCCESS,
    environ_sizes_get: zero,
    environ_get: () => ESUCCESS,
    clock_time_get(_id, _precision, ptr) {
      memory().setBigUint64(ptr, BigInt(Date.now()) * 1_000_000n, true);
      return ESUCCESS;
    },
    random_get(ptr, len) {
      const buf = new Uint8Array(memory().buffer, ptr, len);
      for (let i = 0; i < len; i += 65536) crypto.getRandomValues(buf.subarray(i, i + 65536));
      return ESUCCESS;
    },
    fd_write(fd, iovs, iovsLen, nwritten) {
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
      lines[fd] = parts.pop();
      for (const line of parts) console.error(line);
      view.setUint32(nwritten, n, true);
      return ESUCCESS;
    },
    fd_fdstat_get(fd, ptr) {
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
    proc_exit(code) {
      throw new Error('scrap-web exited with status ' + code);
    },
  };
}
