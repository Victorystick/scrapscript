import { defineConfig, type Plugin } from "vite";
import { readFileSync } from "node:fs";
import { ScrapWasm } from "./scrap-wasm.ts";

// Gives the scrap of each example in a page the ranges to highlight it
// with, as data-highlights, so that repl.ts can highlight it without
// starting scrap-wasm.wasm, which it only does once the scrap is edited.
export function highlightExamples(): Plugin {
  let wasm: ScrapWasm;
  const highlights = (source: string) => {
    wasm ??= new ScrapWasm(
      new WebAssembly.Module(readFileSync("./scrap-wasm.wasm"))
    );
    const result = wasm.run({ source, sources: {}, command: "highlight" });
    if (!("highlights" in result)) throw new Error("highlighting failed");
    return JSON.stringify(result.highlights).replaceAll('"', "&quot;");
  };
  return {
    name: "highlight-examples",
    transformIndexHtml: (html) =>
      html.replace(
        /(<div class="example[^"]*"[^>]*>\s*<pre>)<code>([\s\S]*?)(?=<\/code><\/pre>)/g,
        (_, open, code) =>
          `${open}<code data-highlights="${highlights(unescape(code))}">${code}`
      ),
  };
}

function unescape(html: string): string {
  return html
    .replaceAll("&lt;", "<")
    .replaceAll("&gt;", ">")
    .replaceAll("&quot;", '"')
    .replaceAll("&amp;", "&");
}

export default defineConfig({
  // GitHub Pages serves the site at victorystick.github.io/scrapscript/.
  base: "/scrapscript/",
  plugins: [highlightExamples()],
  build: {
    rollupOptions: {
      input: ["index.html", "tutorial.html", "play.html"],
    },
  },
});
