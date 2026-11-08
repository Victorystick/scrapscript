/// <reference lib="dom" />
// Makes the examples on a page editable, and runs them in the browser with
// scrap-wasm.wasm, which prints what `scrap eval` and `scrap type` do, or
// `scrap test` for an example with data-run="test". Without this script,
// the examples show what they print as they are.
import { printTests, type WasmResult } from '@scrapscript/web/scrap-wasm';
import { editable, runner } from './editor.ts';

// enhance makes an example editable, with a button that runs it.
function enhance(example: HTMLElement) {
  const out = example.querySelector<HTMLElement>('pre.out')!;
  const command = example.dataset.run === 'test' ? 'test' : 'eval';
  editable(
    example,
    async (source, current) => {
      out.classList.add('stale');
      const result = await runner.run(source, command);
      // Show only the latest run.
      if (current()) show(out, result);
    },
    () => out.classList.add('stale')
  );
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
