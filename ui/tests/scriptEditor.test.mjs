import test from 'node:test';
import assert from 'node:assert/strict';
import { Compartment, EditorState } from '@codemirror/state';
import { EditorView } from '@codemirror/view';
import { history, undo, redo } from '@codemirror/commands';
import { ensureSyntaxTree } from '@codemirror/language';
import { classHighlighter, highlightTree } from '@lezer/highlight';
import { detectEditorLanguage } from '../src/utils/editorLanguage.ts';
import { loadScriptEditorLanguage } from '../src/utils/scriptEditorLanguages.ts';

test('file names select shared parsers, with plain text for unknown names', () => {
  const cases = {
    'config/app.php': 'php', 'resources/views/home.blade.php': 'php', 'index.PHTML': 'php',
    artisan: 'php', 'app.js': 'javascript', 'vite.config.mjs': 'javascript', 'script.cjs': 'javascript',
    'App.jsx': 'jsx', 'app.ts': 'typescript', 'types.d.ts': 'typescript', 'entry.mts': 'typescript',
    'server.cts': 'typescript', 'App.tsx': 'tsx', 'App.vue': 'vue', 'index.HTML': 'html', 'page.htm': 'html',
    'composer.json': 'json', 'composer.lock': 'json', 'app.js.map': 'json', 'styles.css': 'css',
    '.env': 'env', '.env.production': 'env', 'app/.env.example': 'env',
    'scripts/deploy.sh': 'shell', '.bashrc': 'shell', '.zshrc': 'shell', '.profile': 'shell',
    'app.py': 'python', 'module.pyi': 'python', 'compose.yaml': 'yaml', 'ci.yml': 'yaml', 'pyproject.toml': 'toml',
    'nginx.conf': 'nginx', 'example.nginx': 'nginx', 'site.nginx.conf': 'nginx',
    '/etc/nginx/conf.d/example.conf': 'nginx', '/etc/nginx/sites-enabled/example.conf': 'nginx',
    'C:\\app\\index.php': 'php', 'php-fpm.conf': 'plain', '/php/conf.d/nginx-other.conf': 'plain',
    'README': 'plain', 'notes.txt': 'plain', 'app.php.backup': 'plain', 'file.constructor': 'plain',
    'file.__proto__': 'plain', 'no-extension': 'plain', '': 'plain',
  };
  for (const [path, expected] of Object.entries(cases)) assert.equal(detectEditorLanguage(path), expected, path);
});

test('all modes emit highlight spans without enabling automatic input handlers', async () => {
  const examples = {
    php: '<?php echo "hello"; ?><section class="card">Hi</section>',
    javascript: 'const value = "hello";', typescript: 'const value: string = "hello";',
    jsx: 'const value = <div title="hello">Hi</div>;', tsx: 'const value: string = <div>Hi</div>;',
    json: '{"name":"hello","count":42}', html: '<div class="hello">Hi</div>',
    vue: '<template><p>{{ title }}</p></template><script setup lang="ts">const title: string = "Hi"</script><style>p { color: red; }</style>',
    css: 'p { color: red; margin: 4px; }', nginx: 'server { listen 80; root /home/fluxo; }',
    env: '# Comment\nAPP_ENV=production', shell: '#!/bin/bash\necho "hello"',
    python: 'print("hello")', yaml: 'name: hello', toml: 'name = "hello"',
  };
  for (const [language, doc] of Object.entries(examples)) {
    const extension = await loadScriptEditorLanguage(language);
    const state = EditorState.create({ doc, extensions: [extension] });
    const tree = ensureSyntaxTree(state, state.doc.length, 1000);
    assert.ok(tree, language);
    const spans = [];
    highlightTree(tree, classHighlighter, (from, to, classes) => spans.push({ text: doc.slice(from, to), classes }));
    assert.ok(spans.length > 0, language);
    assert.equal(state.sliceDoc(), doc, language);
    assert.deepEqual(state.facet(EditorView.inputHandler), [], language);
    if (language === 'php') {
      assert.ok(spans.some(s => s.text === 'echo'), 'PHP keywords');
      assert.ok(spans.some(s => s.text === 'section'), 'HTML embedded in PHP/Blade files');
    }
    if (language === 'vue') {
      for (const text of ['template', 'const', 'color']) assert.ok(spans.some(s => s.text === text), text);
    }
  }
  assert.deepEqual(await loadScriptEditorLanguage('plain'), []);
});

test('language reconfiguration preserves CRLF content, undo history and read-only state', async () => {
  const language = new Compartment();
  const access = new Compartment();
  const original = '<?php\r\necho "unchanged";\r\n';
  let state = EditorState.create({ doc: original, extensions: [
    history(), EditorState.lineSeparator.of('\r\n'), language.of([]), access.of(EditorState.readOnly.of(true)),
  ] });
  const dispatch = transaction => { state = transaction.state; };
  const suffix = '// edit';
  state = state.update({ changes: { from: state.doc.length, insert: suffix } }).state;
  for (const mode of ['php', 'html', 'vue', 'plain', 'env', 'shell', 'nginx']) {
    state = state.update({ effects: language.reconfigure(await loadScriptEditorLanguage(mode)) }).state;
    assert.equal(state.sliceDoc(), original + suffix);
    assert.equal(state.readOnly, true);
  }
  assert.equal(undo({ state, dispatch }), false, 'read-only editors still refuse undo');
  state = state.update({ effects: access.reconfigure(EditorState.readOnly.of(false)) }).state;
  assert.equal(undo({ state, dispatch }), true);
  assert.equal(state.sliceDoc(), original);
  assert.equal(redo({ state, dispatch }), true);
  assert.equal(state.sliceDoc(), original + suffix);
});

test('shared grammar requests reuse the same asynchronous load', async () => {
  const first = loadScriptEditorLanguage('php');
  assert.equal(loadScriptEditorLanguage('php'), first);
  assert.equal(await first, await loadScriptEditorLanguage('php'));
});
