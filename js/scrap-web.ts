// Runs scrap-web.wasm, the web platform's host compiled to WebAssembly from
// cmd/scrap-web (see `pnpm build`), which runs apps for a page: mount, in
// mount.ts, is how pages use it.

import { instantiate, type Instance } from './wasi.ts';

// The URL of scrap-web.wasm, next to this file. Bundlers such as Vite copy
// it into what they build.
export const webWasmURL = new URL('./scrap-web.wasm', import.meta.url);

export interface StartRequest {
  // The app's scrap.
  source: string;
  // The sources of the scraps that it refers to by hash, by hash; the
  // web platform's interface is built in.
  sources: Record<string, string>;
  // The flags that init gets, as JSON; null if absent.
  flags?: unknown;
  // How long each step of the app may run, in milliseconds: starting it,
  // an update, or a view. No limit if absent.
  timeout?: number;
}

// An app's handle, or why it didn't start, or the hashes of the scraps
// to fetch before starting it again.
export type StartResult =
  | { handle: number }
  | { error: string }
  | { missing: string[] };

// A command that an app's step gave, for the page to carry out, by id.
export interface Command {
  id: number;
  http?: HttpRequest;
}

// An HTTP request, with a text body, or bytes as base64, or neither.
export interface HttpRequest {
  method: string;
  url: string;
  headers: [string, string][];
  text?: string;
  bytes?: string;
}

interface Exports extends Instance {
  start(): number;
  render(handle: number): number;
  event(handle: number): number;
  commands(handle: number): number;
  respond(handle: number): number;
  stop(handle: number): void;
}

const encoder = new TextEncoder();
const decoder = new TextDecoder();

// A ScrapWeb runs apps with an instance of scrap-web.wasm, which never
// waits on the page: it asks for the scraps that an app refers to, and
// gets events and renders when the page calls it. Starting the Go runtime
// takes far longer than a step, so pages share one ScrapWeb.
export class ScrapWeb {
  #exports: Exports;
  #trapped: unknown = null;

  constructor(module: WebAssembly.Module) {
    this.#exports = instantiate<Exports>(module);
  }

  start(request: StartRequest): StartResult {
    const n = this.#call((e) => {
      this.#write(encoder.encode(JSON.stringify(request)));
      return e.start();
    });
    return JSON.parse(decoder.decode(this.#read(n)));
  }

  // render returns the patch that takes the page from the app's last view
  // to its view now, in the format of cmd/scrap-web/patch.go.
  render(handle: number): Uint8Array {
    return this.#result(this.#call((e) => e.render(handle)));
  }

  // event passes the app an event on the node with the id, and the value
  // and checked state of the element.
  event(handle: number, node: number, name: string, value: string, checked: boolean) {
    const nameBytes = encoder.encode(name);
    const valueBytes = encoder.encode(value);
    const input = new Uint8Array(12 + nameBytes.length + valueBytes.length);
    const view = new DataView(input.buffer);
    view.setUint32(0, node, true);
    view.setUint32(4, checked ? 1 : 0, true);
    view.setUint32(8, nameBytes.length, true);
    input.set(nameBytes, 12);
    input.set(valueBytes, 12 + nameBytes.length);
    this.#result(
      this.#call((e) => {
        this.#write(input);
        return e.event(handle);
      })
    );
  }

  // commands takes the commands that the app's steps have given since it
  // was last asked, oldest first.
  commands(handle: number): Command[] {
    const out = this.#result(this.#call((e) => e.commands(handle)));
    return out.length === 0 ? [] : JSON.parse(decoder.decode(out));
  }

  // respond passes the app the response to its request with the id: its
  // status, headers and body, or a status of 0 and why the request failed.
  respond(handle: number, id: number, status: number, headers: [string, string][], body: Uint8Array) {
    const strings = headers.flat().map((s) => encoder.encode(s));
    const size = strings.reduce((n, s) => n + s.length, 12 + 4 * strings.length + body.length);
    const input = new Uint8Array(size);
    const view = new DataView(input.buffer);
    view.setUint32(0, id, true);
    view.setUint32(4, status, true);
    view.setUint32(8, headers.length, true);
    let at = 12;
    for (const s of strings) {
      view.setUint32(at, s.length, true);
      at += 4;
    }
    for (const s of strings) {
      input.set(s, at);
      at += s.length;
    }
    input.set(body, at);
    this.#result(
      this.#call((e) => {
        this.#write(input);
        return e.respond(handle);
      })
    );
  }

  stop(handle: number) {
    if (this.#trapped == null) this.#exports.stop(handle);
  }

  // #call calls the module. A trap leaves the Go runtime broken, and with
  // it every app that it runs, so calls after one fail too.
  #call(f: (e: Exports) => number): number {
    if (this.#trapped != null) {
      throw new Error('scrap-web.wasm stopped after an earlier failure', { cause: this.#trapped });
    }
    try {
      return f(this.#exports);
    } catch (e) {
      this.#trapped = e;
      throw e;
    }
  }

  #write(bytes: Uint8Array) {
    const ptr = this.#exports.alloc(bytes.length);
    new Uint8Array(this.#exports.memory.buffer, ptr, bytes.length).set(bytes);
  }

  // #read copies n bytes of output out of the module's memory, which the
  // next call may reuse or grow.
  #read(n: number): Uint8Array {
    const ptr = this.#exports.output();
    return new Uint8Array(this.#exports.memory.buffer.slice(ptr, ptr + n));
  }

  // #result returns the output of a call that returns its length, or
  // throws the error whose length it returns negated.
  #result(n: number): Uint8Array {
    if (n < 0) throw new Error(decoder.decode(this.#read(-n)));
    return this.#read(n);
  }
}
