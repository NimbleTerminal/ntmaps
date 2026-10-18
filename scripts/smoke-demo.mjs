// Run each real WASM demo in its own process. No tile service is needed:
// unavailable tiles exercise the asynchronous error path without network I/O.
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import vm from 'node:vm';
import { setTimeout as delay } from 'node:timers/promises';
const demo = process.argv[2];
assert.ok(['mapview', 'mapview3d'].includes(demo));
// Go disables Fetch when process.argv0 identifies Node. Give the WASM
// runtime a browser-like process descriptor so the stub owns every request.
const nodeProcess = process;
globalThis.process = Object.create(nodeProcess);
Object.defineProperty(globalThis.process, 'argv0', { value: 'browser-smoke' });
globalThis.fetch = async () => { throw new Error('Offline smoke test'); };
vm.runInThisContext(await readFile('web/wasm_exec.js', 'utf8'));
const go = new Go();
let exitCode;
go.exit = code => { exitCode = code; };
const { instance } = await WebAssembly.instantiate(await readFile(`web/demos/${demo}.wasm`), go.importObject);
const timeout = setTimeout(() => { console.error(`${demo} timed out`); process.exit(1); }, 20000);
const running = go.run(instance);
async function expectOutput(text) {
  let output = '';
  for (let n = 0; n < 200 && !output.includes(text); n++) {
    await delay(25);
    output += bubbletea_read();
  }
  assert.ok(output.includes(text), `expected output: ${text}; got ${output.slice(-1000)}`);
  return output;
}
try {
  for (let n = 0; typeof globalThis.bubbletea_read !== 'function' && n < 100; n++) await delay(20);
  assert.equal(typeof globalThis.bubbletea_read, 'function');
  bubbletea_resize(100, 40);
  await expectOutput(demo === 'mapview' ? 'Loading' : 'map zoom');
  bubbletea_write('\x1b]7501;?\x1b\\');
  const advertised = await expectOutput(':msg=');
  const encoded = advertised.match(/:msg=([A-Za-z0-9+/=]+)/)?.[1];
  assert.ok(encoded, 'action status payload');
  const description = Buffer.from(encoded, 'base64').toString('utf8');
  assert.ok(description.includes(demo === 'mapview' ? 'North' : 'Rotate left'));
  bubbletea_resize(60, 20);
  bubbletea_write(demo === 'mapview' ? 'j' : 'n');
  await delay(100);
  bubbletea_write('\x03');
  await running;
  assert.equal(exitCode, 0);
  console.log(`${demo}: startup, actions, resize, input, and quit passed`);
} finally {
  clearTimeout(timeout);
}
process.exit(0);
