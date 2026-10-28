// The playground: drafts of a little website, which scrap-web.wasm runs as
// an app of the web platform, in the Elm architecture, in a Web Worker
// (worker.js). main.scrap is the app, which the page shows in a sandboxed
// frame, as the host patches its view, and sends the events of back to the
// host, which keeps the app and its model. The page keeps the messages that
// updated the model since the state it was loaded with, and loads a changed
// app with them, as Elm's debugger replays them, so editing any draft keeps
// the state, and the steps. Once the updates settle, the page asks the host
// for the model, as a scrap, and shows it in state.scrap, and the address
// bar keeps the drafts, the state among them. See ../README.md for the
// host's requests.

// The draft that is the app, and the draft that shows its model.
const app = 'main.scrap';
const state = 'state.scrap';

// What drafts may be named: a draft refers to another as $"./name.scrap".
const draftName = /^[\w-]+\.scrap$/;

// How long after an edit the site reloads, and after an update the page
// asks for the model.
const delay = 400;
const settle = 300;

// The most messages that the page keeps, as each costs an update when the
// app reloads, and how many it keeps when it has more, starting from the
// model before them.
const maxLog = 500;
const keepLog = 250;

// How long a request may run before the page stops the worker.
const timeout = 5000;

// A Runner runs requests in a worker, which it replaces when a request runs
// too long, calling stopped. The new worker has none of the old one's
// state. A failed request answers { error }.
class Runner {
  #worker = null;
  #ready = Promise.resolve();
  #next = 0;
  #pending = new Map();
  stopped = () => {};

  async run(request) {
    const worker = (this.#worker ??= this.#begin());
    try {
      await this.#ready;
    } catch (e) {
      this.#worker = null;
      return { error: e.message };
    }
    const id = this.#next++;
    return new Promise((resolve) => {
      const timer = setTimeout(() => this.#stop(`Stopped after ${timeout / 1000} seconds.`), timeout);
      this.#pending.set(id, (result) => {
        clearTimeout(timer);
        resolve(result);
      });
      worker.postMessage({ id, request });
    });
  }

  #begin() {
    const worker = new Worker('./worker.js');
    this.#ready = new Promise((resolve, reject) => {
      worker.onmessage = ({ data }) => {
        if ('id' in data) {
          this.#pending.get(data.id)?.(data.result);
          this.#pending.delete(data.id);
        } else if (data.ready) {
          resolve();
        } else {
          reject(new Error('Starting scrap-web failed: ' + data.error));
        }
      };
      worker.onerror = (e) => reject(new Error('Starting scrap-web failed: ' + e.message));
    });
    return worker;
  }

  #stop(error) {
    this.#worker?.terminate();
    this.#worker = null;
    for (const resolve of this.#pending.values()) resolve({ error });
    this.#pending.clear();
    this.stopped();
  }
}

const host = new Runner();
const drafts = [];
const list = document.querySelector('.drafts');
const site = document.querySelector('.site');
const frame = site.querySelector('iframe');
const out = site.querySelector('.out');
const log = site.querySelector('.history');

function button(label, onclick) {
  const b = document.createElement('button');
  b.type = 'button';
  b.textContent = label;
  b.onclick = onclick;
  return b;
}

// fail shows an error, or clears the last if there is none.
function fail(error) {
  out.textContent = error ?? '';
  out.classList.remove('stale');
}

// draft adds a pane that edits the draft with the code, or makes one of
// pane, whose textarea has its code.
function draft(name, source, pane) {
  if (!pane) {
    pane = document.createElement('div');
    pane.className = 'draft';
    pane.append(document.createElement('textarea'));
    newDraft.before(pane);
  }
  const code = pane.querySelector('textarea');
  if (source != null) code.value = source;
  code.spellcheck = false;
  code.setAttribute('aria-label', 'Scrap');
  const d = { name, pane, code };
  const fit = () => {
    code.style.height = 'auto';
    code.style.height = code.scrollHeight + 2 + 'px';
  };
  code.addEventListener('input', () => {
    fit();
    edited(d);
  });
  code.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      reload();
    } else if (e.key === 'Tab' && !e.shiftKey) {
      e.preventDefault();
      code.setRangeText('  ', code.selectionStart, code.selectionEnd, 'end');
    }
  });
  requestAnimationFrame(fit);

  const head = document.createElement('div');
  head.className = 'draft-head';
  const label = document.createElement('input');
  label.value = name;
  label.spellcheck = false;
  label.setAttribute('aria-label', 'Name');
  label.readOnly = name === app || name === state;
  label.onchange = () => {
    const taken = drafts.some((o) => o !== d && o.name === label.value);
    if (!draftName.test(label.value) || taken) {
      label.value = d.name;
      return;
    }
    d.name = pane.dataset.draft = label.value;
    reload();
  };
  head.append(label);
  if (name === app || name === state) {
    const role = document.createElement('span');
    role.className = 'role';
    role.textContent = name === app ? 'the app' : 'its model, once updates settle';
    head.append(role);
  } else {
    head.append(
      button('Delete', () => {
        drafts.splice(drafts.indexOf(d), 1);
        pane.remove();
        reload();
      })
    );
  }
  pane.dataset.draft = name;
  pane.prepend(head);
  drafts.push(d);
}

function path(name) {
  return `$"./${name}"`;
}

function sources() {
  return Object.fromEntries(drafts.map((d) => [d.name, d.code.value]));
}

function find(name) {
  return drafts.find((d) => d.name === name);
}

const newDraft = button('New draft', () => {
  let name = 'draft.scrap';
  for (let i = 2; find(name); i++) name = `draft-${i}.scrap`;
  draft(name, `-- Other drafts refer to this one as ${path(name)}.\n()`);
  drafts.at(-1).code.focus();
  save();
});
newDraft.className = 'new-draft';
list.append(newDraft);

// The model that the messages start from, as a scrap, or "" for init, the
// messages, as scraps, the step of the host that the first follows, and
// how many of them the model shown follows, if not all, as after going
// back. The app loads with the model after them, replayed.
let base = '';
let messages = [];
let shift = 0;
let at = null;

// The generation of the last view, whether the host has the app, whether
// state.scrap holds its model, and whether state.scrap was edited since the
// app was last loaded, which the next load starts from, as it does first.
let generation = 0;
let loaded = false;
let synced = true;
let stateEdited = true;

// The page asks the host one thing at a time, and notes whether it is
// loading the app.
let queue = Promise.resolve();
let loading = false;
function enqueue(task) {
  queue = queue.then(task);
}

// The site reloads a while after the last edit, and keeps its state.
let timer;
function edited(d) {
  out.classList.add('stale');
  if (d.name === state) stateEdited = true;
  clearTimeout(timer);
  timer = setTimeout(reload, delay);
}

function reload() {
  clearTimeout(timer);
  enqueue(() => load());
}

// load loads the app with the messages, from the base, or from state.scrap
// if it was edited, which drops them, or init if it is blank. If it fails,
// the host keeps the app it has. It clears the last error, unless asked
// not to.
async function load(clear = true) {
  if (stateEdited) {
    base = find(state)?.code.value.trim() ?? '';
    forget();
    stateEdited = false;
  }
  save();
  loading = true;
  const res = await host
    .run({
      op: 'load',
      source: path(app),
      drafts: sources(),
      state: base || undefined,
      messages,
      step: at ?? undefined,
      generation: generation + 1,
    })
    .finally(() => (loading = false));
  if (!res.patch) {
    fail(res.error ?? 'The host failed to load the app.');
    return;
  }
  loaded = true;
  // The messages that no longer fit the app go.
  messages.length = res.replayed ?? 0;
  shift = 0;
  if (at != null && at > messages.length) at = null;
  shown(res);
  showSteps();
  if (res.error || clear) fail(res.error);
  settled();
}

// forget forgets the messages, as the model starts anew.
function forget() {
  messages = [];
  shift = 0;
  at = null;
}

// The events that the site sent, which the host takes together once it is
// free, and draws one view for, as Elm draws once per frame.
let events = [];
function send(event) {
  events.push(event);
  enqueue(flush);
}

async function flush() {
  if (events.length == 0) return;
  const batch = events;
  events = [];
  const res = await host.run({ op: 'send', events: batch });
  post({ reply: batch.length });
  if (res.unloaded) return lost();
  for (const { step, message } of res.steps ?? []) {
    // An update after going back drops the messages after it.
    messages.length = step - shift - 1;
    messages.push(message);
    at = null;
  }
  if (res.patch) shown(res);
  if (res.error || res.patch) fail(res.error);
  if (res.steps?.length) {
    showSteps();
    settled();
  }
}

// lost loads the app again, with the messages, as the host lost it.
async function lost() {
  loaded = false;
  await load();
}
// A host that runs too long is stopped, and loses its app, which the page
// loads again with the messages, keeping the error. The update that ran
// too long isn't among them, but if loading did, they may be what does, so
// the page tries once more without them, and then waits for an edit.
host.stopped = () => {
  loaded = false;
  if (!loading) {
    enqueue(() => load(false));
  } else if (messages.length > 0) {
    forget();
    showSteps();
    enqueue(() => load(false));
  }
};

// shown shows the patch of the view that the host answered with.
function shown(res) {
  generation = res.generation;
  post({ patch: res.patch, generation });
}

// Once updates settle, the page asks for the model, to show it, and keeps
// fewer messages if it has too many.
let syncTimer;
function settled() {
  synced = false;
  clearTimeout(syncTimer);
  syncTimer = setTimeout(() => enqueue(sync), settle);
}

// sync makes state.scrap hold the host's model, unless it was edited since,
// and moves the base past the first messages, if there are too many.
async function sync() {
  clearTimeout(syncTimer);
  if (!loaded) return;
  const drop = Math.min(messages.length - keepLog, at ?? Infinity);
  if (messages.length > maxLog && drop > 0) {
    const res = await host.run({ op: 'state', step: shift + drop });
    if (res.unloaded) return lost();
    if (res.state != null) {
      base = res.state;
      messages = messages.slice(drop);
      shift += drop;
      if (at != null) at -= drop;
      showSteps();
    }
  }
  if (synced || stateEdited) return;
  const res = await host.run({ op: 'state' });
  if (res.unloaded) return lost();
  if (res.state == null) return;
  setState(res.state);
  synced = true;
  save();
}

// setState makes state.scrap hold the model, as a scrap.
function setState(source) {
  let d = find(state);
  if (!d) {
    draft(state, source);
    d = drafts.at(-1);
  }
  d.code.value = source;
}

// showSteps shows the messages, the latest first, each of which clicking
// goes back to the model after, and marks the one shown.
function showSteps() {
  const shownAt = at ?? messages.length;
  const entries = messages.slice(-100).map((message, i, last) => {
    const n = messages.length - last.length + i + 1;
    const entry = document.createElement('li');
    entry.append(button(message, () => enqueue(() => goTo(n))));
    if (n === shownAt) entry.className = 'current';
    return entry;
  });
  log.replaceChildren(...entries.reverse());
}

// goTo shows the model after n messages.
async function goTo(n) {
  const res = await host.run({ op: 'goto', step: shift + n });
  if (res.unloaded) return lost();
  if (!res.patch) return fail(res.error);
  at = n === messages.length ? null : n;
  shown(res);
  showSteps();
  fail();
  settled();
}

// save keeps the drafts in the address bar, to share.
function save() {
  history.replaceState(null, '', '#' + encodeURIComponent(JSON.stringify(sources())));
}

// shared returns the drafts in the address bar, if any.
function shared() {
  try {
    const found = JSON.parse(decodeURIComponent(location.hash.slice(1)));
    const ok =
      typeof found[app] === 'string' &&
      Object.entries(found).every(([k, v]) => draftName.test(k) && typeof v === 'string');
    return ok ? found : null;
  } catch {
    return null;
  }
}

// The site runs in a frame of its own origin, which can't touch the page.
// The page posts it each patch of the view, with its generation, and the
// number of events it sent that the host has taken, and it posts each
// event back. Until the frame has loaded, or once it loads anew, as after
// leaving the site, the page drops patches, and asks the host for the
// whole view.
let ready = false;
function post(data) {
  if (ready || data.patch == null) frame.contentWindow?.postMessage(data, '*');
}
frame.addEventListener('load', () => {
  ready = false;
  enqueue(async () => {
    // A host without the app shows the next whole.
    if (!loaded) {
      ready = true;
      return;
    }
    const res = await host.run({ op: 'view' });
    if (res.unloaded) return lost();
    ready = true;
    if (res.patch) shown(res);
  });
});
frame.srcdoc = `<!doctype html><meta charset="utf-8"><script>(${render})()</script>`;
addEventListener('message', ({ source, data }) => {
  if (source !== frame.contentWindow) return;
  const { generation, node, event, input } = data ?? {};
  if (typeof generation !== 'number' || typeof node !== 'number' || typeof event !== 'string') return;
  send(typeof input === 'string' ? { generation, node, event, input } : { generation, node, event });
});

// render runs in the frame, where it applies each patch of the view that
// the page posts it, and posts the page each event that an element listens
// for. It must refer to nothing outside itself.
function render() {
  // The nodes of the view, by id, but the body, 0, which the script runs
  // before, and their ids.
  const nodes = new Map();
  const ids = new WeakMap();
  // The events that the host has yet to take. Until it has, an input that
  // has the focus keeps the value that was typed into it.
  let pending = 0;
  // The generation of the view shown, which events are of.
  let generation = 0;
  addEventListener('message', ({ source, data }) => {
    if (source !== parent) return;
    pending -= data.reply ?? 0;
    if (data.patch != null) {
      generation = data.generation;
      apply(data.patch);
    }
  });
  // A form would leave the page.
  addEventListener('submit', (e) => e.preventDefault());

  // apply applies a patch, as ../dom.go describes.
  function apply(ops) {
    let i = 0;
    const num = () => ops[i++];
    const str = () => ops[i++];
    const get = (id) => (id === 0 ? document.body : nodes.get(id));
    const place = (parent, after, node) =>
      parent.insertBefore(node, after ? get(after).nextSibling : parent.firstChild);
    const build = () => {
      const id = num();
      let node;
      if (ops[i] === null) {
        i++;
        node = document.createTextNode(str());
      } else {
        const tag = str();
        try {
          node = document.createElement(tag);
        } catch {
          node = document.createElement('div');
        }
        for (let n = num(); n > 0; n--) set(node, str(), str());
        for (let n = num(); n > 0; n--) node.addEventListener(str(), handle);
        for (let n = num(); n > 0; n--) node.append(build());
      }
      nodes.set(id, node);
      ids.set(node, id);
      return node;
    };
    while (i < ops.length) {
      switch (num()) {
        case 0: // clear
          document.body.replaceChildren();
          nodes.clear();
          break;
        case 1: {
          // insert
          const parent = get(num());
          const after = num();
          place(parent, after, build());
          break;
        }
        case 2: {
          // move
          const parent = get(num());
          const after = num();
          place(parent, after, get(num()));
          break;
        }
        case 3: {
          // remove
          const node = get(num());
          node.remove();
          forget(node);
          break;
        }
        case 4: // text
          get(num()).data = str();
          break;
        case 5: // set
          set(get(num()), str(), str());
          break;
        case 6: {
          // unset
          const el = get(num());
          const name = str();
          el.removeAttribute(name);
          if (name === 'checked' && 'checked' in el) el.checked = false;
          break;
        }
        case 7: // on
          get(num()).addEventListener(str(), handle);
          break;
        case 8: // off
          get(num()).removeEventListener(str(), handle);
          break;
      }
    }
  }

  // set sets an attribute, and the property of an input that its attribute
  // only starts it with.
  function set(el, name, value) {
    try {
      el.setAttribute(name, value);
    } catch {}
    if (name === 'value' && 'value' in el && el.value !== value) {
      if (!(pending > 0 && el === document.activeElement)) el.value = value;
    }
    if (name === 'checked' && 'checked' in el) el.checked = true;
  }

  function forget(node) {
    nodes.delete(ids.get(node));
    node.childNodes.forEach(forget);
  }

  function handle(e) {
    const node = ids.get(e.currentTarget);
    if (node == null) return;
    pending++;
    parent.postMessage({ generation, node, event: e.type, input: String(e.target.value ?? '') }, '*');
  }
}

// Start with the drafts in the address bar, or the page's.
const panes = [...list.querySelectorAll('.draft')];
const found = shared();
if (found) {
  for (const pane of panes) pane.remove();
  for (const [name, source] of Object.entries(found)) draft(name, source);
} else {
  for (const pane of panes) draft(pane.dataset.draft, null, pane);
}

site.querySelector('.actions').append(
  button('Reset state', () => {
    setState('');
    stateEdited = true;
    reload();
  }),
  button('Run', reload)
);
reload();
