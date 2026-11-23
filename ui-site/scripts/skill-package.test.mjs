import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { mkdtemp, mkdir, readFile, rm, symlink, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { resolve } from 'node:path';
import test from 'node:test';
import { packageSkill } from './package-skill.mjs';

const source = resolve(import.meta.dirname, '../../skills/fluxo');
const licenseFile = resolve(import.meta.dirname, '../../LICENSE');
const digest = bytes => createHash('sha256').update(bytes).digest('hex');
async function temporary(t) {
  const root = await mkdtemp(resolve(tmpdir(), 'fluxo-skill-'));
  t.after(() => rm(root, { recursive: true, force: true }));
  return root;
}

// Read the resulting standard ZIP independently of the writer's source list.
function readArchive(zip) {
  const files = new Map(); let at = 0;
  while (zip.readUInt32LE(at) === 0x04034b50) {
    assert.equal(zip.readUInt16LE(at + 8), 0, 'unsupported compression');
    const size = zip.readUInt32LE(at + 18), nameLength = zip.readUInt16LE(at + 26), extra = zip.readUInt16LE(at + 28);
    const name = zip.subarray(at + 30, at + 30 + nameLength).toString('utf8');
    assert.ok(name.startsWith('fluxo/') && !name.split('/').includes('..'));
    assert.equal(files.has(name), false);
    const start = at + 30 + nameLength + extra;
    files.set(name, zip.subarray(start, start + size)); at = start + size;
  }
  const directoryStart = at;
  for (const [name, bytes] of files) {
    assert.equal(zip.readUInt32LE(at), 0x02014b50);
    const nameLength = zip.readUInt16LE(at + 28), extra = zip.readUInt16LE(at + 30), comment = zip.readUInt16LE(at + 32);
    assert.equal(zip.subarray(at + 46, at + 46 + nameLength).toString(), name);
    assert.equal(zip.readUInt32LE(at + 24), bytes.length);
    at += 46 + nameLength + extra + comment;
  }
  assert.equal(zip.readUInt32LE(at), 0x06054b50);
  assert.equal(zip.readUInt16LE(at + 10), files.size);
  assert.equal(zip.readUInt32LE(at + 16), directoryStart);
  assert.equal(at + 22, zip.length);
  return files;
}

test('download contains a discoverable skill, references, UI metadata and license only', async t => {
  const outputDir = await temporary(t);
  const manifest = await packageSkill({ outputDir });
  const zip = await readFile(resolve(outputDir, manifest.archive));
  const entries = readArchive(zip);
  assert.deepEqual([...entries.keys()].sort(), ['fluxo/LICENSE', 'fluxo/SKILL.md', 'fluxo/agents/openai.yaml', 'fluxo/references/workflows.md']);
  for (const file of manifest.files) {
    const bytes = entries.get(`fluxo/${file.path}`);
    assert.equal(bytes.length, file.size); assert.equal(digest(bytes), file.sha256);
    assert.deepEqual(bytes, await readFile(resolve(outputDir, file.path)));
    assert.deepEqual(bytes, await readFile(file.path === 'LICENSE' ? licenseFile : resolve(source, file.path)));
  }
  assert.equal(manifest.sha256, digest(zip)); assert.equal(manifest.size, zip.length);
  assert.equal(await readFile(resolve(outputDir, 'SHA256SUMS'), 'utf8'), `${digest(zip)}  fluxo-skill.zip\n`);
  assert.deepEqual(JSON.parse(await readFile(resolve(outputDir, 'manifest.json'), 'utf8')), manifest);
  assert.match(entries.get('fluxo/SKILL.md').toString(), /^---\nname: fluxo\n/);
});

test('package is reproducible and installs without a repository checkout', async t => {
  const root = await temporary(t), one = resolve(root, 'one'), two = resolve(root, 'two');
  await packageSkill({ outputDir: one }); await packageSkill({ outputDir: two });
  const zip = await readFile(resolve(one, 'fluxo-skill.zip'));
  assert.deepEqual(zip, await readFile(resolve(two, 'fluxo-skill.zip')));
  const installRoot = resolve(root, '.agents/skills');
  for (const [name, bytes] of readArchive(zip)) {
    const path = resolve(installRoot, name); await mkdir(resolve(path, '..'), { recursive: true }); await writeFile(path, bytes);
  }
  const skill = await readFile(resolve(installRoot, 'fluxo/SKILL.md'), 'utf8');
  for (const link of skill.matchAll(/\]\(([^)]+)\)/g)) {
    if (!link[1].startsWith('https://')) await readFile(resolve(installRoot, 'fluxo', link[1]));
  }
});

async function sourceFixture(t) {
  const root = await temporary(t);
  await mkdir(resolve(root, 'agents')); await mkdir(resolve(root, 'references'));
  for (const file of ['SKILL.md', 'agents/openai.yaml', 'references/workflows.md']) await writeFile(resolve(root, file), await readFile(resolve(source, file)));
  await writeFile(resolve(root, '.env'), 'NEVER_PACKAGE_THIS_SECRET');
  return root;
}

test('unlisted files are excluded; symlinked and oversized inputs are refused', async t => {
  const skillRoot = await sourceFixture(t), outputDir = await temporary(t);
  const manifest = await packageSkill({ skillRoot, outputDir });
  assert.equal((await readFile(resolve(outputDir, manifest.archive))).includes(Buffer.from('NEVER_PACKAGE_THIS_SECRET')), false);
  await rm(resolve(skillRoot, 'SKILL.md')); await symlink(resolve(source, 'SKILL.md'), resolve(skillRoot, 'SKILL.md'));
  await assert.rejects(packageSkill({ skillRoot, outputDir }), /Unsafe skill package input/);
  await rm(resolve(skillRoot, 'SKILL.md')); await writeFile(resolve(skillRoot, 'SKILL.md'), 'x'.repeat(128 * 1024 + 1));
  await assert.rejects(packageSkill({ skillRoot, outputDir }), /Unsafe skill package input/);
});

test('parent-directory symlinks cannot import files outside the skill', async t => {
  const skillRoot = await sourceFixture(t), outputDir = await temporary(t);
  await rm(resolve(skillRoot, 'references'), { recursive: true }); await symlink(resolve(source, 'references'), resolve(skillRoot, 'references'));
  await assert.rejects(packageSkill({ skillRoot, outputDir }), /Unsafe skill package input/);
});

test('skill downloads remain static Pages assets', async () => {
  const routes = JSON.parse(await readFile(new URL('../public/_routes.json', import.meta.url), 'utf8'));
  const path = '/skills/fluxo/fluxo-skill.zip';
  const matches = rule => new RegExp('^' + rule.split('*').map(part => part.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('.*') + '$').test(path);
  assert.equal(routes.include.some(matches), false);
});
