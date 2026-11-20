/// <reference lib="dom" />
// The app playground: an editor whose scrap runs as an app on the web
// platform, next to it, with mount. Run starts the app afresh; until the
// new one starts, the last one goes on, so an error shows next to it.
import { mount, type App } from '@scrapscript/web';
import { editable } from './editor.ts';
import counter from './apps/counter.scrap?raw';
import clock from './apps/clock.scrap?raw';
import fetch from './apps/fetch.scrap?raw';

const examples: Record<string, string> = { Counter: counter, Clock: clock, Fetch: fetch };

const example = document.querySelector<HTMLElement>('.example')!;
const host = document.getElementById('app')!;
const errors = document.getElementById('errors')!;

let app: App | null = null;
// Each run's number, so that a run that starts after a later one is dropped.
let runs = 0;

// The first example is what Reset goes back to, unless one is picked.
example.querySelector('code')!.textContent = counter;
const editor = editable(example, run, () => errors.classList.add('stale'));

const pick = document.createElement('select');
pick.setAttribute('aria-label', 'Example');
for (const name of Object.keys(examples)) pick.append(new Option(name));
pick.onchange = () => editor.load(examples[pick.value]);
editor.actions.prepend(pick);

if (location.hash.length <= 1) editor.load(counter);

async function run(source: string) {
  const run = ++runs;
  const element = document.createElement('div');
  const reported: string[] = [];
  let next: App;
  try {
    next = await mount(element, { source, flags: null, onError: report });
  } catch (e) {
    if (run === runs) show([e instanceof Error ? e.message : String(e)]);
    return;
  }
  if (run !== runs) {
    next.stop();
    return;
  }
  app?.stop();
  app = next;
  host.replaceChildren(element);
  show(reported);

  // report lists a step of the app that failed, while it is the one shown.
  function report(message: string) {
    reported.push(message);
    if (app === next || app == null) show(reported);
  }
}

// show lists the errors, newest last.
function show(messages: string[]) {
  errors.replaceChildren(
    ...messages.map((m) => {
      const s = document.createElement('samp');
      s.className = 'error';
      s.textContent = m;
      return s;
    })
  );
  errors.classList.remove('stale');
}
