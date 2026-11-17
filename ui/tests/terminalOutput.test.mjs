import test from 'node:test';
import assert from 'node:assert/strict';
import { terminalOutput } from '../src/utils/terminalOutput.ts';

test('a CPU-stalled terminal cannot accumulate unbounded writes', () => {
  const callbacks = []; let received = 0; let disconnected = 0;
  const output = terminalOutput((data, done) => { received += data.length; callbacks.push(done); }, () => disconnected++, 8192);
  for (let n = 0; n < 10000; n++) output.write(new Uint8Array(4096));
  assert.equal(received, 8192);
  assert.equal(disconnected, 1);
  callbacks.forEach(done => done());
  output.write(new Uint8Array(1));
  assert.equal(received, 8192);
});
test('completed rendering releases capacity and disposal rejects further frames', () => {
  let done; let received = 0;
  const output = terminalOutput((data, callback) => { received += data.length; done = callback; }, () => assert.fail('overflow'), 4096);
  output.write(new Uint8Array(4096)); done(); done();
  output.write(new Uint8Array(4096)); assert.equal(received, 8192);
  output.close(); done(); output.write(new Uint8Array(1)); assert.equal(received, 8192);
});

test('render acknowledgements follow completion once and stop after disposal', () => {
  const callbacks = []; const rendered = [];
  const output = terminalOutput((data, done) => callbacks.push(done), () => assert.fail('overflow'), 4096, bytes => rendered.push(bytes));
  output.write(new Uint8Array(100)); assert.deepEqual(rendered, []);
  callbacks[0](); callbacks[0](); assert.deepEqual(rendered, [100]);
  output.write(new Uint8Array(100)); output.close(); callbacks[1]();
  assert.deepEqual(rendered, [100]);
});
