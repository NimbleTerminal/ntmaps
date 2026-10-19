// Validate the generated Pages artifact without requiring a local HTTP server.
import assert from 'node:assert/strict';
import { readFile, access } from 'node:fs/promises';
import { BoobaTerminal, installActionBar, installRendererHud, parseRendererFromURL } from '../web/booba/booba.js';
// Link the actual browser runtime exports: syntax checks alone miss missing APIs.
for (const api of [BoobaTerminal, installActionBar, installRendererHud, parseRendererFromURL]) {
  assert.equal(typeof api, 'function', 'required browser runtime API');
}
for (const file of [
  'index.html', 'site.css', 'site.js', 'wasm_exec.js', 'booba/booba.js',
  'ghostty-web/ghostty-web.js', 'ghostty-web/ghostty-vt.wasm',
  'LICENSE', 'THIRD_PARTY_NOTICES.md',
]) await access(`web/${file}`);
const html = await readFile('web/index.html', 'utf8');
const demos = [...html.matchAll(/data-demo="([^"]+)"/g)].map(m => m[1]);
assert.deepEqual(demos, ['explorer', 'mapview', 'mapview3d'], 'demo cards');
for (const demo of demos) {
  const bytes = await readFile(`web/demos/${demo}.wasm`);
  assert.deepEqual([...bytes.subarray(0, 4)], [0, 97, 115, 109], `${demo}: WASM header`);
  assert.ok(WebAssembly.validate(bytes), `${demo}: valid WASM`);
}
for (const [, path] of html.matchAll(/(?:src|href)="([^"?#]+)"/g)) {
  if (/^(https?:|\.\/)/.test(path)) continue;
  await access(`web/${path}`);
}
const js = await readFile('web/site.js', 'utf8');
for (const demo of demos) assert.ok(js.includes(`${demo}:`), `${demo}: site.js entry`);
console.log(`Site links, runtime assets, and ${demos.length} WASM binaries checked.`);
