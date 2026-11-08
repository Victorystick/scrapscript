// Starts each example of the app playground, app.html, as the page would,
// with scrap-web.wasm: each must name the interface by its hash now, check
// as an app, and init and render without errors.
import { describe, expect, test } from 'vitest';
import { createHash } from 'node:crypto';
import { readdirSync, readFileSync } from 'node:fs';
import { ScrapWeb, webWasmURL } from '@scrapscript/web';

const iface = readFileSync(new URL('../cmd/scrap-web/web.scrap', import.meta.url));
const hash = createHash('sha256').update(iface).digest('hex');
const web = new ScrapWeb(new WebAssembly.Module(readFileSync(webWasmURL)));

const dir = new URL('./apps/', import.meta.url);
const names = readdirSync(dir).filter((f) => f.endsWith('.scrap'));

describe('app examples', () => {
  test('are there', () => expect(names).toEqual(['clock.scrap', 'counter.scrap', 'fetch.scrap']));

  test.each(names)('%s starts', (name) => {
    const source = readFileSync(new URL(name, dir), 'utf8');
    expect(source).toContain(`; web : type -> type -> type = $source:${hash}\n`);
    const result = web.start({ source, sources: {}, flags: null, timeout: 1000 });
    expect(result).toHaveProperty('handle');
    const handle = (result as { handle: number }).handle;
    expect(web.render(handle).length).toBeGreaterThan(8);
    web.subscriptions(handle);
    web.commands(handle);
    web.stop(handle);
  });
});
