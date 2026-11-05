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
  describe('HTTP', () => {
    // An API that serves some JSON, echoes what it gets, and lacks the rest.
    beforeEach(async () => {
      await page.route('**/api/**', async (route) => {
        const r = route.request();
        const path = new URL(r.url()).pathname;
        if (path === '/api/data') {
          await route.fulfill({ json: { name: 'hé', n: 2 } });
        } else if (path === '/api/echo' && r.method() === 'PUT') {
          await route.fulfill({ status: 201, body: r.postDataBuffer()! });
        } else if (path === '/api/echo') {
          const h = r.headers();
          await route.fulfill({ body: `${r.method()} ${h['content-type']} ${h['x-scrap']} ${r.postData()}` });
        } else {
          await route.fulfill({ status: 404, body: 'none' });
        }
      });
    });

    const app = `app
; app : web (list text) msg =
    { init = _ -> { model = [], cmds =
        [ get "/api/data" (#json (r -> #got (json-result r)))
        , #http { method = "POST", url = "/api/echo", headers = [{ name = "x-scrap", value = "yes" }], body = #json (#object [{ key = "a", value = #list [#number 1.5, #null, #bool #true] }]), expect = #text (r -> #got (text-result r)) }
        , #http { method = "PUT", url = "/api/echo", headers = [], body = #bytes ~~AAH/, expect = #bytes (r -> #got (bytes-result r)) }
        ] }
    , update = | #got t -> log -> { model = log +< t, cmds = [] }
               | #more  -> log -> { model = log, cmds = [get "/api/missing" (#text (r -> #got (text-result r))), get "/api/data" (#text (r -> #got (text-result r)))] }
    , view = log -> #el { tag = "div", attrs = [#on-click #more], children = list/map (t -> #el { tag = "p", attrs = [], children = [#text t] }) log }
    , subscriptions = _ -> []
    }
; get = url -> expect -> #http { method = "GET", url = url, headers = [], body = #empty, expect = expect }
; json-result =
    | #ok { status = s, headers = _, body = #object ({ key = "name", value = #text t } >+ _) } -> int/to-text s ++ " " ++ t
    | #ok _ -> "unexpected"
    | #error e -> e
; text-result =
    | #ok r -> int/to-text r.status ++ " " ++ r.body
    | #error e -> e
; bytes-result =
    | #ok r -> int/to-text r.status ++ " " ++ int/to-text (bytes/length r.body)
    | #error e -> e
; msg : type = [#got text, #more]
${web}`;

    // first replaces the URL of the first request, which expects JSON.
    const first = (url: string) => app.replace('"/api/data"', JSON.stringify(url));
    const log = () => page.$$eval('p', (ps) => ps.map((p) => p.textContent));

    test('carries out requests, and passes back their responses', async () => {
      expect(await start(app)).toBeNull();
      await page.waitForFunction(() => document.querySelectorAll('p').length === 3);
      expect((await log()).sort()).toEqual([
        '200 POST application/json yes {"a":[1.5,null,true]}',
        '200 hé',
        '201 3',
      ]);
      // An update's requests; as in Elm, a status that isn't 2xx is an
      // error.
      await page.click('div');
      await page.waitForFunction(() => document.querySelectorAll('p').length === 5);
      expect((await log()).sort()).toEqual([
        '200 POST application/json yes {"a":[1.5,null,true]}',
        '200 hé',
        '200 {"name":"hé","n":2}',
        '201 3',
        'bad status: 404',
      ]);
      expect(await errors()).toEqual([]);
    });

    test('reports a request that fails', async () => {
      expect(await start(first('http://127.0.0.1:1/'))).toBeNull();
      await page.waitForFunction(() => document.querySelectorAll('p').length === 3);
      expect(await log()).toContain('Failed to fetch');
    });

    test('reports a body that is not the JSON expected', async () => {
      expect(await start(first('/api/echo'))).toBeNull();
      await page.waitForFunction(() => document.querySelectorAll('p').length === 3);
      expect((await log()).filter((t) => t!.startsWith('bad body: '))).toHaveLength(1);
    });

    test('aborts its requests when it stops', async () => {
      await page.route('**/api/data', () => {});
      const failed = page.waitForEvent('requestfailed', (r) => r.url().endsWith('/api/data'));
      expect(await start(app)).toBeNull();
      await page.waitForFunction(() => document.querySelectorAll('p').length === 2);
      await page.evaluate(() => (window as any).app.stop());
      expect((await failed).failure()?.errorText).toBe('net::ERR_ABORTED');
      expect(await page.locator('p').count()).toBe(2);
      expect(await errors()).toEqual([]);
    });
  });
  describe('time', () => {
    const t0 = Date.UTC(2026, 0, 1);
    // The clock stands at t0, and moves only as the tests run it.
    beforeEach(async () => {
      await page.clock.install({ time: t0 - 1000 });
      await page.clock.pauseAt(t0);
    });

    test('runs timers while subscribed', async () => {
      await page.evaluate(() => {
        const w = window as any;
        w.cleared = 0;
        const clear = window.clearInterval;
        window.clearInterval = (id) => (w.cleared++, clear(id));
      });
      expect(await start(`app
; app : web { n : int, at : int } [#tick int] =
    { init = _ -> { model = { n = 0, at = 0 }, cmds = [] }
    , update = | #tick t -> m -> { model = { n = m.n + 1, at = t }, cmds = [] }
    , view = m -> #el { tag = "p", attrs = [], children = [#text (int/to-text m.n ++ " " ++ int/to-text m.at)] }
    , subscriptions = | { n = 3, at = _ } -> []
                      | _ -> [#every { ms = 1000, msg = t -> #tick t }, #every { ms = 1000, msg = t -> #tick t }]
    }
${web}`)).toBeNull();
      expect(await page.textContent('p')).toBe('0 0');
      // Two subscriptions to the same interval share a timer.
      // It renders in the animation frame after the tick.
      await page.clock.runFor(1050);
      expect(await page.textContent('p')).toBe(`2 ${t0 + 1000}`);
      await page.clock.runFor(1000);
      expect(await page.textContent('p')).toBe(`4 ${t0 + 2000}`);
      // It keeps the timer across renders, and clears it when it stops.
      expect(await page.evaluate(() => (window as any).cleared)).toBe(0);
      await page.evaluate(() => (window as any).app.stop());
      expect(await page.evaluate(() => (window as any).cleared)).toBe(1);
      await page.clock.runFor(1000);
      expect(await page.textContent('p')).toBe(`4 ${t0 + 2000}`);
    });

    test('stops timers that it no longer subscribes to', async () => {
      await page.evaluate(() => {
        const w = window as any;
        w.cleared = 0;
        const clear = window.clearInterval;
        window.clearInterval = (id) => (w.cleared++, clear(id));
      });
      // Ticks every 300 ms twice, and every 1000 ms three times, the
      // last at t0 + 3000.
      expect(await start(`app
; app : web { a : int, b : int, at : int } [#a int, #b] =
    { init = _ -> { model = { a = 0, b = 0, at = 0 }, cmds = [] }
    , update = | #a t -> m -> { model = { ..m, a = m.a + 1, at = t }, cmds = [] }
               | #b   -> m -> { model = { ..m, b = m.b + 1 }, cmds = [] }
    , view = m -> #el { tag = "p", attrs = [], children = [#text (text/join " " (list/map int/to-text [m.a, m.b, m.at]))] }
    , subscriptions = | { a = 3, b = _, at = _ } -> []
                      | { a = _, b = 2, at = _ } -> [#every { ms = 1000, msg = t -> #a t }]
                      | _ -> [#every { ms = 1000, msg = t -> #a t }, #every { ms = 300, msg = _ -> #b }]
    }
${web}`)).toBeNull();
      await page.clock.runFor(5000);
      expect(await page.textContent('p')).toBe(`3 2 ${t0 + 3000}`);
      expect(await page.evaluate(() => (window as any).cleared)).toBe(2);
    });

    test('passes animation frames the time since the last', async () => {
      expect(await start(`app
; app : web (list json) [#frame float] =
    { init = _ -> { model = [], cmds = [] }
    , update = | #frame d -> ds -> { model = ds +< #number d, cmds = [] }
    , view = ds -> #el { tag = "p", attrs = [#prop { name = "deltas", value = #list ds }], children = [#text (int/to-text (list/length ds))] }
    , subscriptions = ds -> (| 3 -> [] | _ -> [#animation-frame (d -> #frame d)]) (list/length ds)
    }
; json : type = [#null, #bool [#false, #true], #number float, #text text, #list (list json), #object (list { key : text, value : json })]
${web}`)).toBeNull();
      await page.clock.runFor(1000);
      expect(await page.textContent('p')).toBe('3');
      const deltas: number[] = await page.evaluate(() => (document.querySelector('p') as any).deltas);
      // A frame every 16 ms, as the clock runs them; the first is the
      // time since the app subscribed, within a frame.
      expect(deltas.slice(1)).toEqual([16, 16]);
      expect(deltas[0]).toBeGreaterThan(0);
      expect(deltas[0]).toBeLessThanOrEqual(16);
      expect(await errors()).toEqual([]);
    });

    test('gets the time and random numbers', async () => {
      expect(await start(`app
; app : web (list text) [#at int, #roll int, #float float] =
    { init = _ -> { model = [], cmds = [#now (t -> #at t), #random-float (f -> #float f)] ++ list/repeat 50 (#random-int { min = 6, max = 1, msg = n -> #roll n }) }
    , update = | #at t    -> log -> { model = log +< ("at " ++ int/to-text t), cmds = [] }
               | #roll n  -> log -> { model = log +< int/to-text n, cmds = [] }
               | #float f -> log -> { model = log +< "float", cmds = [] }
    , view = log -> #el { tag = "p", attrs = [], children = [#text (text/join " " log)] }
    , subscriptions = _ -> []
    }
${web}`)).toBeNull();
      await page.clock.runFor(100);
      const [at, now, float, ...rolls] = (await page.textContent('p'))!.split(' ');
      expect(at).toBe('at');
      // Woken at once, but after init.
      expect(Number(now)).toBeGreaterThanOrEqual(t0);
      expect(Number(now)).toBeLessThan(t0 + 10);
      expect(float).toBe('float');
      expect(rolls).toHaveLength(50);
      expect(new Set(rolls)).toEqual(new Set(['1', '2', '3', '4', '5', '6']));
    });
  });
});
