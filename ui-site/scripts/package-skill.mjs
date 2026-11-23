import { lstat, mkdir, readFile, realpath, writeFile } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import { dirname, resolve, relative, isAbsolute } from 'node:path';
import { fileURLToPath } from 'node:url';

const repository = resolve(dirname(fileURLToPath(import.meta.url)), '../..');
const allowed = ['SKILL.md', 'agents/openai.yaml', 'references/workflows.md'];

function crc32(bytes) {
  let crc = 0xffffffff;
  for (const byte of bytes) {
    crc ^= byte;
    for (let bit = 0; bit < 8; bit++) crc = (crc >>> 1) ^ ((crc & 1) ? 0xedb88320 : 0);
  }
  return (crc ^ 0xffffffff) >>> 0;
}

// Small, deterministic, uncompressed ZIP. The skill is text-only, so there is
// no external archiver dependency or executable installer in the download.
export function skillZip(files) {
  const local = [], directory = [];
  let offset = 0, directorySize = 0;
  for (const file of files) {
    const name = Buffer.from(file.name, 'utf8'), bytes = Buffer.from(file.bytes);
    const crc = crc32(bytes), header = Buffer.alloc(30);
    header.writeUInt32LE(0x04034b50, 0); header.writeUInt16LE(20, 4);
    header.writeUInt16LE(0x800, 6); header.writeUInt16LE(33, 12);
    header.writeUInt32LE(crc, 14); header.writeUInt32LE(bytes.length, 18);
    header.writeUInt32LE(bytes.length, 22); header.writeUInt16LE(name.length, 26);
    local.push(header, name, bytes);
    const entry = Buffer.alloc(46);
    entry.writeUInt32LE(0x02014b50, 0); entry.writeUInt16LE(0x314, 4);
    entry.writeUInt16LE(20, 6); entry.writeUInt16LE(0x800, 8);
    entry.writeUInt16LE(33, 14); entry.writeUInt32LE(crc, 16);
    entry.writeUInt32LE(bytes.length, 20); entry.writeUInt32LE(bytes.length, 24);
    entry.writeUInt16LE(name.length, 28); entry.writeUInt32LE((0o100644 * 65536) >>> 0, 38);
    entry.writeUInt32LE(offset, 42);
    directory.push(entry, name);
    offset += header.length + name.length + bytes.length;
    directorySize += entry.length + name.length;
  }
  const end = Buffer.alloc(22);
  end.writeUInt32LE(0x06054b50, 0); end.writeUInt16LE(files.length, 8);
  end.writeUInt16LE(files.length, 10); end.writeUInt32LE(directorySize, 12);
  end.writeUInt32LE(offset, 16);
  return Buffer.concat([...local, ...directory, end]);
}

export async function packageSkill({
  skillRoot = resolve(repository, 'skills/fluxo'),
  licenseFile = resolve(repository, 'LICENSE'),
  outputDir = resolve(repository, 'ui-site/public/skills/fluxo'),
} = {}) {
  const root = await realpath(skillRoot), files = [];
  for (const name of allowed) {
    const path = resolve(skillRoot, name), info = await lstat(path);
    const actual = await realpath(path), fromRoot = relative(root, actual);
    if (!info.isFile() || info.isSymbolicLink() || fromRoot.startsWith('..') || isAbsolute(fromRoot) || info.size > 128 * 1024) {
      throw new Error(`Unsafe skill package input: ${name}`);
    }
    files.push({ name, bytes: await readFile(path) });
  }
  const entrypoint = files.find(file => file.name === 'SKILL.md').bytes.toString('utf8');
  if (!entrypoint.startsWith('---\n') || !/^name: fluxo$/m.test(entrypoint)) throw new Error('Invalid Fluxo skill entry point');
  const version = entrypoint.match(/^  version: "([0-9]+\.[0-9]+\.[0-9]+)"$/m)?.[1];
  if (!version) throw new Error('Skill version is missing');
  const licenseInfo = await lstat(licenseFile);
  if (!licenseInfo.isFile() || licenseInfo.isSymbolicLink() || licenseInfo.size > 128 * 1024) throw new Error('Unsafe skill license input');
  const license = await readFile(licenseFile);
  files.push({ name: 'LICENSE', bytes: license });
  const archive = skillZip(files.map(file => ({ ...file, name: `fluxo/${file.name}` })));
  const digest = createHash('sha256').update(archive).digest('hex');
  const manifest = { name: 'fluxo', version, format: 'Agent Skills', minimum_fluxo_version: '0.4.56',
    archive: 'fluxo-skill.zip', sha256: digest, size: archive.length,
    files: files.map(file => ({ path: file.name, sha256: createHash('sha256').update(file.bytes).digest('hex'), size: file.bytes.length })) };
  await mkdir(outputDir, { recursive: true });
  for (const file of files) {
    const target = resolve(outputDir, file.name);
    await mkdir(dirname(target), { recursive: true });
    await writeFile(target, file.bytes);
  }
  await writeFile(resolve(outputDir, 'fluxo-skill.zip'), archive);
  await writeFile(resolve(outputDir, 'manifest.json'), JSON.stringify(manifest, null, 2) + '\n');
  await writeFile(resolve(outputDir, 'SHA256SUMS'), `${digest}  fluxo-skill.zip\n`);
  return manifest;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const manifest = await packageSkill();
  console.log(`Packaged Fluxo skill ${manifest.version}: ${manifest.size} bytes (${manifest.sha256})`);
}
