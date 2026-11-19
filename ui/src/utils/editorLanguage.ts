export type ScriptEditorLanguage =
  | 'plain' | 'env' | 'shell' | 'php' | 'javascript' | 'typescript' | 'jsx' | 'tsx'
  | 'json' | 'html' | 'vue' | 'css' | 'nginx' | 'python' | 'yaml' | 'toml';

const extensions: Readonly<Record<string, ScriptEditorLanguage>> = {
  php: 'php', phtml: 'php', php5: 'php', php7: 'php', php8: 'php',
  js: 'javascript', mjs: 'javascript', cjs: 'javascript', jsx: 'jsx',
  ts: 'typescript', mts: 'typescript', cts: 'typescript', tsx: 'tsx',
  json: 'json', map: 'json', html: 'html', htm: 'html', vue: 'vue', css: 'css',
  sh: 'shell', bash: 'shell', zsh: 'shell',
  py: 'python', pyi: 'python', yaml: 'yaml', yml: 'yaml', toml: 'toml',
};
const shellFiles = new Set(['.bashrc', '.bash_profile', '.bash_login', '.bash_logout', '.profile', '.zshrc', '.zprofile', '.zshenv']);

export function detectEditorLanguage(path: string): ScriptEditorLanguage {
  const normalized = path.replace(/\\/g, '/').toLowerCase();
  const name = normalized.split('/').pop() ?? '';
  if (name === '.env' || name.startsWith('.env.')) return 'env';
  if (shellFiles.has(name)) return 'shell';
  if (name === 'artisan') return 'php';
  if (name === 'composer.lock') return 'json';
  if (name === 'nginx.conf' || name.endsWith('.nginx') || name.endsWith('.nginx.conf') ||
      (name.endsWith('.conf') && /(?:^|\/)(?:nginx|sites-available|sites-enabled)\//.test(normalized))) return 'nginx';
  const extension = name.includes('.') ? name.slice(name.lastIndexOf('.') + 1) : '';
  return Object.hasOwn(extensions, extension) ? extensions[extension]! : 'plain';
}
