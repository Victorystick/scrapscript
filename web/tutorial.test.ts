import { beforeAll, describe, expect, test } from 'vitest';
import { spawnSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import {
  ScrapWasm,
  printTests,
  type Command,
  type WasmResult,
} from './scrap-wasm.ts';
import { highlightExamples } from './vite.config.ts';

// The scrap CLI, built from this repository, which runs each example in
// tutorial.html as a reader would.
const scrap = './build/scrap';

interface Example {
  source: string;
  run?: string;
  outputs: Record<string, string>;
}

// examples returns the examples in html: a .example div holds the scrap
// in a <pre><code>, and what running it prints in <samp>s of a pre.out.
function examples(html: string): Example[] {
  const found: Example[] = [];
  const example =
    /<div class="example"(?: data-run="(\w+)")?>\s*<pre><code>([\s\S]*?)<\/code><\/pre>\s*<pre class="out">([\s\S]*?)<\/pre>\s*<\/div>/g;
  for (const [, run, source, out] of html.matchAll(example)) {
    const outputs: Record<string, string> = {};
    for (const [, kind, text] of out.matchAll(
      /<samp class="(\w+)">([\s\S]*?)<\/samp>/g
    )) {
      outputs[kind] = unescape(text);
    }
    found.push({ source: unescape(source), run, outputs });
  }
  return found;
}

function unescape(html: string): string {
  return html
    .replaceAll('&lt;', '<')
    .replaceAll('&gt;', '>')
    .replaceAll('&quot;', '"')
    .replaceAll('&amp;', '&');
}

// run runs a scrap command on source, and returns what it prints.
function run(command: string, source: string): string {
  const result = spawnSync(scrap, [command, '--cache', './build/scraps'], {
    input: source,
    encoding: 'utf8',
  });
  return (result.stdout + result.stderr).trimEnd();
}

// runInBrowser runs source as the tutorial's Run buttons do, fetching the
// scraps it refers to from the yard.
async function runInBrowser(
  scrap: ScrapWasm,
  source: string,
  command: Command
): Promise<WasmResult> {
  const sources: Record<string, string> = {};
  for (;;) {
    const result = scrap.run({ source, sources, command });
    if (!('missing' in result)) return result;
    for (const key of result.missing) {
      const response = await fetch('https://scraps.oseg.dev/' + key);
      sources[key] = await response.text();
    }
  }
}

describe('tutorial', () => {
  beforeAll(() => {
    const build = spawnSync(
      'go build -o build/scrap ../cmd/scrap',
      { shell: true, stdio: 'pipe' }
    );
    if (build.status !== 0) {
      throw new Error('go build failed:\n' + build.stderr);
    }
  });

  const all = examples(readFileSync('./tutorial.html', 'utf8'));

  test('has examples', () => {
    expect(all.length).toBeGreaterThan(40);
  });

  // npm test builds scrap-wasm.wasm before running the tests.
  const wasm = new ScrapWasm(
    new WebAssembly.Module(readFileSync('./scrap-wasm.wasm'))
  );

  test('highlights examples, without changing their text', () => {
    const source = readFileSync('./tutorial.html', 'utf8');
    const transform = highlightExamples().transformIndexHtml as (
      html: string
    ) => string;
    const html = transform(source);
    // Only the attribute is added.
    expect(html.replace(/ data-highlights="[^"]*"/g, '')).toBe(source);
    const found = [
      ...html.matchAll(
        /<pre><code data-highlights="([^"]*)">([\s\S]*?)<\/code><\/pre>/g
      ),
    ];
    expect(found.length).toBe(all.length);
    for (const [, attribute, code] of found) {
      const text = unescape(code);
      const spans: Record<string, number[]> = JSON.parse(unescape(attribute));
      for (const offsets of Object.values(spans)) {
        let last = 0;
        for (const offset of offsets) {
          expect(offset).toBeGreaterThanOrEqual(last);
          last = offset;
        }
        expect(last).toBeLessThanOrEqual(text.length);
      }
    }
    expect(found[0][1]).toContain('number');
  });

  for (const { source, run: command, outputs } of all) {
    test('in the browser: ' + source, async () => {
      if (command === 'test') {
        const result = await runInBrowser(wasm, source, 'test');
        expect('results' in result && printTests(result.results!)).toBe(
          outputs.output
        );
        return;
      }
      const result = await runInBrowser(wasm, source, 'eval');
      if ('error' in outputs) {
        expect(result).toEqual({ error: outputs.error });
      } else {
        expect(result).toEqual({ type: outputs.type, value: outputs.value });
      }
    });

    test(source, () => {
      if (command) {
        expect(run(command, source)).toBe(outputs.output);
      } else if ('error' in outputs) {
        expect(run('type', source)).toBe(outputs.error);
      } else {
        expect(run('eval', source)).toBe(outputs.value);
        expect(run('type', source)).toBe(outputs.type);
      }
    });
  }
});
