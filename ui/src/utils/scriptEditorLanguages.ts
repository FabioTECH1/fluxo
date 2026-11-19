import { StreamLanguage } from '@codemirror/language';
import type { Extension } from '@codemirror/state';
import type { ScriptEditorLanguage } from './editorLanguage';

const envLanguage = StreamLanguage.define({
  token(stream) {
    if (stream.sol() && stream.match(/\s*#.*/)) return 'comment';
    if (stream.sol() && stream.match(/\s*(?:export\s+)?[A-Za-z_][A-Za-z0-9_]*(?=\s*=)/)) return 'propertyName';
    if (stream.eat('=')) return 'operator';
    stream.skipToEnd();
    return 'string';
  },
  languageData: { commentTokens: { line: '#' } },
});
const languages = new Map<ScriptEditorLanguage, Promise<Extension>>();

async function languageParser(language: ScriptEditorLanguage): Promise<Extension> {
  // Load parsers only. LanguageSupport may also enable automatic tag insertion
  // or completion; syntax colouring must not change editing behaviour.
  switch (language) {
    case 'env': return envLanguage;
    case 'shell': return StreamLanguage.define((await import('@codemirror/legacy-modes/mode/shell')).shell);
    case 'nginx': return StreamLanguage.define((await import('@codemirror/legacy-modes/mode/nginx')).nginx);
    case 'python': return StreamLanguage.define((await import('@codemirror/legacy-modes/mode/python')).python);
    case 'yaml': return StreamLanguage.define((await import('@codemirror/legacy-modes/mode/yaml')).yaml);
    case 'toml': return StreamLanguage.define((await import('@codemirror/legacy-modes/mode/toml')).toml);
    case 'php': return (await import('@codemirror/lang-php')).php().language;
    case 'javascript': case 'typescript': case 'jsx': case 'tsx':
      return (await import('@codemirror/lang-javascript')).javascript({
        typescript: language === 'typescript' || language === 'tsx',
        jsx: language === 'jsx' || language === 'tsx',
      }).language;
    case 'json': return (await import('@codemirror/lang-json')).json().language;
    case 'html': return (await import('@codemirror/lang-html')).html().language;
    case 'vue': return (await import('@codemirror/lang-vue')).vue().language;
    case 'css': return (await import('@codemirror/lang-css')).css().language;
    default: return [];
  }
}

export function loadScriptEditorLanguage(language: ScriptEditorLanguage): Promise<Extension> {
  let parser = languages.get(language);
  if (!parser) {
    parser = languageParser(language).catch(error => {
      languages.delete(language);
      throw error;
    });
    languages.set(language, parser);
  }
  return parser;
}
