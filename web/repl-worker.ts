/// <reference lib="webworker" />
// Runs scraps for repl.ts, off the page's thread, so that a scrap that
// loops forever doesn't freeze the page. The page stops it instead.
import { ScrapWasm, type Command, type WasmResult } from './scrap-wasm.ts';

export interface RunRequest {
  id: number;
  source: string;
  command: Command;
}

// The worker says once whether it started, and then answers each request.
export type RunResponse =
  | { ready: true }
  | { ready: false; error: string }
  | { id: number; result: WasmResult };

const scrap = WebAssembly.compileStreaming(
  fetch(new URL('./scrap-wasm.wasm', import.meta.url))
).then((module) => new ScrapWasm(module));

scrap.then(
  () => post({ ready: true }),
  (e) => post({ ready: false, error: String(e) })
);

function post(response: RunResponse) {
  self.postMessage(response);
}

// The sources of the scraps fetched from the yard so far, by hash. A hash
// names one source forever, so they never go stale.
const sources: Record<string, string> = {};

// The most scraps that a scrap may refer to, as the yard allows.
const maxImports = 256;

self.onmessage = async ({ data }: MessageEvent<RunRequest>) => {
  let result: WasmResult;
  try {
    result = await run(await scrap, data.source, data.command);
  } catch (e) {
    result = { error: String(e) };
  }
  post({ id: data.id, result });
};

async function run(
  scrap: ScrapWasm,
  source: string,
  command: Command
): Promise<WasmResult> {
  let count = 0;
  for (;;) {
    const result = scrap.run({ source, sources, command });
    if (!('missing' in result)) return result;

    count += result.missing.length;
    if (count > maxImports) {
      return { error: `A scrap may refer to at most ${maxImports} scraps` };
    }
    const fetched = await Promise.all(result.missing.map(fetchScrap));
    const absent = result.missing.filter((_, i) => fetched[i] == null);
    if (absent.length > 0) {
      return {
        error:
          'The yard lacks the scraps it refers to:\n' +
          absent.map((key) => `$source:${key}`).join('\n'),
      };
    }
    result.missing.forEach((key, i) => (sources[key] = fetched[i]!));
  }
}

// The yard that scrap fetches scraps from by default.
const yard = 'https://scraps.oseg.dev/';

// fetchScrap fetches a scrap from the yard, or returns null if the yard
// lacks it.
async function fetchScrap(key: string): Promise<string | null> {
  const response = await fetch(yard + key, {
    headers: { Accept: 'application/scrap' },
  });
  if (response.status === 404) return null;
  if (!response.ok) {
    throw new Error(`Fetching $source:${key} failed: ${response.status}`);
  }
  return response.text();
}
