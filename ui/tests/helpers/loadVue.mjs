import { readFile } from 'node:fs/promises';
import { createRequire, stripTypeScriptTypes } from 'node:module';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
const require = createRequire(new URL('../../package.json', import.meta.url));
const { parse, compileScript } = require('@vue/compiler-sfc');
const loaded = new Map();
export async function loadVue(url) {
  const file = url instanceof URL ? fileURLToPath(url) : url;
  if (loaded.has(file)) return loaded.get(file);
  const { descriptor } = parse(await readFile(file, 'utf8'), { filename: file });
  let source = compileScript(descriptor, { id: 'test-' + loaded.size, inlineTemplate: true }).content;
  for (const match of [...source.matchAll(/from\s*['"]([^'"]+)['"]/g)]) {
    const spec = match[1];
    let target = spec.startsWith('.') ? path.resolve(path.dirname(file), spec) : require.resolve(spec);
    if (spec.startsWith('.') && !path.extname(target)) target += '.ts';
    const resolved = spec.endsWith('.vue') ? await loadVue(target) : pathToFileURL(target).href;
    source = source.replace(match[0], `from ${JSON.stringify(resolved)}`);
  }
  const result = 'data:text/javascript;base64,' + Buffer.from(stripTypeScriptTypes(source)).toString('base64');
  loaded.set(file, result);
  return result;
}
