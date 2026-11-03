// Runs apps with mount in Chromium, driven by Playwright, on a page that
// Vite serves: test/index.html. `pnpm build` must have built the modules.

import { afterAll, beforeAll, beforeEach, describe, expect, test } from 'vitest';
import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { chromium, type Browser, type Page } from 'playwright';
import { createServer, type ViteDevServer } from 'vite';

// Apps name the interface by its hash, which scrap-web.wasm has built in.
const iface = readFileSync(new URL('../cmd/scrap-web/web.scrap', import.meta.url));
const web = `; web : type -> type -> type = $source:${createHash('sha256').update(iface).digest('hex')}`;

let server: ViteDevServer;
let browser: Browser;
let page: Page;

beforeAll(async () => {
  server = await createServer({
    root: new URL('.', import.meta.url).pathname,
    logLevel: 'silent',
    server: { port: 0 },
  });
  await server.listen();
  browser = await chromium.launch();
}, 60_000);

afterAll(async () => {
  await browser?.close();
  await server?.close();
});

beforeEach(async () => {
  page = await browser.newPage();
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));
  await page.goto(server.resolvedUrls!.local[0] + 'test/index.html');
  await page.waitForFunction(() => 'mount' in window);
  return async () => {
    await page.close();
    expect(errors).toEqual([]);
  };
});

interface Options {
  flags?: unknown;
  scraps?: Record<string, string>;
  timeout?: number;
}

// start mounts the app in #app, with fetchScrap serving scraps, and
// collects its errors in window.errors. It returns the error that mount
// failed with, if any.
function start(source: string, options: Options = {}): Promise<string | null> {
  return page.evaluate(
    async ([source, { scraps = {}, ...options }]) => {
      const w = window as any;
      w.errors = [];
      try {
        w.app = await w.mount(document.getElementById('app'), {
          source,
          ...options,
          fetchScrap: async (hash: string) => scraps[hash] ?? null,
          onError: (message: string) => w.errors.push(message),
        });
        return null;
      } catch (e) {
        return (e as Error).message;
      }
    },
    [source, options] as const
  );
}

// html returns the content of #app once the app has rendered after the
// last event.
async function html(): Promise<string> {
  await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(resolve)));
  return page.innerHTML('#app');
}

const errors = () => page.evaluate(() => (window as any).errors as string[]);

const counter = `app
; app : web int msg =
    { init = _ -> { model = 0, cmds = [] }
    , update = | #inc -> n -> { model = n + 1, cmds = [] }
               | #spin -> n -> { model = loop n, cmds = [] }
    , view = n -> #el { tag = "button", attrs = [#on-click #inc, #on { event = "dblclick", msg = #spin }], children = [#text (int/to-text n)] }
    , subscriptions = _ -> []
    }
; msg : type = [#inc, #spin]
; loop = n -> loop n
${web}`;

describe('mount', () => {
  test('renders and updates', async () => {
    expect(await start(counter)).toBeNull();
    expect(await page.innerHTML('#app')).toBe('<button>0</button>');
    await page.click('button');
    await page.click('button');
    expect(await html()).toBe('<button>2</button>');
  });

  test('replaces what the element held', async () => {
    await page.evaluate(() => (document.getElementById('app')!.innerHTML = '<p>old</p>'));
    expect(await start(counter)).toBeNull();
    expect(await page.innerHTML('#app')).toBe('<button>0</button>');
  });

  test('reports a step that runs too long, and goes on', async () => {
    expect(await start(counter, { timeout: 100 })).toBeNull();
    await page.dblclick('button'); // two clicks, and a #spin
    expect(await html()).toBe('<button>2</button>');
    expect(await errors()).toEqual(['evaluation stopped: it ran past its deadline']);
  });

  test('stops', async () => {
    expect(await start(counter)).toBeNull();
    await page.evaluate(() => (window as any).app.stop());
    await page.click('button');
    expect(await html()).toBe('<button>0</button>');
  });

  test('fails to start an app that does not check', async () => {
    expect(await start(`1`)).toContain('does not implement the interface');
    expect(await start(`1 +`)).toContain('parse error');
  });

  test('fetches the scraps it refers to', async () => {
    const inc = 'x -> x + 1';
    const hash = createHash('sha256').update(inc).digest('hex');
    const source = counter.replace('n + 1', `$source:${hash} n`);
    expect(await start(source)).toBe(`There are no scraps\n$source:${hash}`);
    expect(await start(source, { scraps: { [hash]: inc } })).toBeNull();
    await page.click('button');
    expect(await html()).toBe('<button>1</button>');
  });

  test('gets flags', async () => {
    const flags = `app
; app : web text [#none] =
    { init = | #object [{ key = "name", value = #text name }, { key = "n", value = #number _ }] -> { model = name, cmds = [] }
             | _ -> { model = "?", cmds = [] }
    , update = | #none -> m -> { model = m, cmds = [] }
    , view = m -> #text m
    , subscriptions = _ -> []
    }
${web}`;
    expect(await start(flags, { flags: { name: 'scrap', n: 2.5 } })).toBeNull();
    expect(await page.innerHTML('#app')).toBe('scrap');
    expect(await start(flags)).toBeNull();
    expect(await page.innerHTML('#app')).toBe('?');
  });
});

describe('the page', () => {
  test('applies attributes, properties and styles', async () => {
    expect(await start(`app
; app : web bool [#flip] =
    { init = _ -> { model = #false, cmds = [] }
    , update = | #flip -> (| #true -> { model = #false, cmds = [] } | #false -> { model = #true, cmds = [] })
    , view = | #true -> #el { tag = "input", attrs = [#attr { name = "class", value = "on" }, #style { name = "color", value = "red" }, #prop { name = "checked", value = #bool #true }, #on-click #flip], children = [] }
             | #false -> #el { tag = "input", attrs = [#attr { name = "type", value = "checkbox" }, #on-click #flip], children = [] }
    , subscriptions = _ -> []
    }
; bool : type = [#false, #true]
${web}`)).toBeNull();
    expect(await page.innerHTML('#app')).toBe('<input type="checkbox">');
    await page.click('input');
    expect(await html()).toBe('<input class="on" style="color: red;">');
    expect(await page.evaluate(() => document.querySelector('input')!.checked)).toBe(true);
    await page.click('input');
    // Removing the last style leaves the attribute empty, as the DOM does.
    expect(await html()).toBe('<input style="" type="checkbox">');
    expect(await page.evaluate(() => document.querySelector('input')!.checked)).toBe(false);
  });

  test('passes inputs, checkboxes and forms', async () => {
    expect(await start(`app
; app : web model msg =
    { init = _ -> { model = { text = "", done = #false, sent = 0 }, cmds = [] }
    , update = | #typed t   -> m -> { model = { text = t, done = m.done, sent = m.sent }, cmds = [] }
               | #checked b -> m -> { model = { text = m.text, done = b, sent = m.sent }, cmds = [] }
               | #sent      -> m -> { model = { text = "", done = m.done, sent = m.sent + 1 }, cmds = [] }
    , view = m -> #el { tag = "form", attrs = [#on-submit #sent], children =
        [ #el { tag = "input", attrs = [#attr { name = "id", value = "text" }, #prop { name = "value", value = #text m.text }, #on-input (t -> #typed t)], children = [] }
        , #el { tag = "input", attrs = [#attr { name = "id", value = "box" }, #attr { name = "type", value = "checkbox" }, #on-check (b -> #checked b)], children = [] }
        , #el { tag = "p", attrs = [], children = [#text (m.text ++ " " ++ yes m.done ++ " " ++ int/to-text m.sent)] }
        , #el { tag = "button", attrs = [], children = [#text "send"] }
        ] }
    , subscriptions = _ -> []
    }
; model : type = { text : text, done : bool, sent : int }
; msg : type = [#typed text, #checked bool, #sent]
; bool : type = [#false, #true]
; yes = | #true -> "yes" | #false -> "no"
${web}`)).toBeNull();
    await page.fill('#text', 'hé');
    await page.check('#box');
    await html();
    expect(await page.textContent('p')).toBe('hé yes 0');
    await page.click('button');
    await html();
    // The page didn't navigate away, and the input is empty again.
    expect(await page.textContent('p')).toBe(' yes 1');
    expect(await page.inputValue('#text')).toBe('');
  });

  test('passes events up from where they happen, unless they do not bubble', async () => {
    expect(await start(`app
; app : web (list text) text =
    { init = _ -> { model = [], cmds = [] }
    , update = e -> es -> { model = es +< e, cmds = [] }
    , view = es -> #el { tag = "div", attrs = [#on { event = "click", msg = "div" }, #on { event = "focus", msg = "div focus" }], children =
        [ #el { tag = "button", attrs = [#on-click "button", #on { event = "focus", msg = "button focus" }], children = [#text "b"] }
        , #el { tag = "p", attrs = [], children = [#text (text/join ", " es)] }
        ] }
    , subscriptions = _ -> []
    }
${web}`)).toBeNull();
    await page.click('button');
    await page.click('p');
    expect(await html()).toContain('<p>button focus, button, div, div</p>');
  });

  test('keeps the nodes of keyed children', async () => {
    expect(await start(`app
; app : web (list text) [#rotate] =
    { init = _ -> { model = ["a", "b", "c", "d"], cmds = [] }
    , update = | #rotate -> (| x >+ xs -> { model = xs +< x, cmds = [] }
                              | [] -> { model = [], cmds = [] })
    , view = xs -> #keyed { tag = "ul", attrs = [#on-click #rotate], children = list/map (x -> { key = x, node = #el { tag = "li", attrs = [], children = [#text x] } }) xs }
    , subscriptions = _ -> []
    }
${web}`)).toBeNull();
    await page.evaluate(() => document.querySelectorAll('li').forEach((li) => ((li as any).mark = li.textContent)));
    await page.click('li');
    expect(await html()).toBe('<ul><li>b</li><li>c</li><li>d</li><li>a</li></ul>');
    const marks = await page.evaluate(() => [...document.querySelectorAll('li')].map((li) => (li as any).mark));
    expect(marks).toEqual(['b', 'c', 'd', 'a']);
  });
});
