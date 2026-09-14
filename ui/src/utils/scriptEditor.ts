import { Compartment, EditorState, type Extension } from '@codemirror/state';
import { EditorView, drawSelection, dropCursor, highlightSpecialChars, keymap, lineNumbers, placeholder } from '@codemirror/view';
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands';
import { HighlightStyle, StreamLanguage, syntaxHighlighting } from '@codemirror/language';
import { shell } from '@codemirror/legacy-modes/mode/shell';
import { tags } from '@lezer/highlight';

export interface ScriptEditorOptions {
  modelValue: string;
  language: 'env' | 'shell' | 'plain';
  label: string;
  id?: string;
  ariaDescribedby?: string;
  placeholder: string;
  readonly: boolean;
  masked: boolean;
  busy: boolean;
  minimumLines: number;
}

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
const shellLanguage = StreamLanguage.define(shell);
const highlight = syntaxHighlighting(HighlightStyle.define([
  { tag: tags.comment, color: 'var(--editor-muted)', fontStyle: 'italic' },
  { tag: [tags.propertyName, tags.keyword, tags.definitionKeyword, tags.function(tags.variableName)], color: 'var(--editor-key)' },
  { tag: [tags.string, tags.number, tags.bool], color: 'var(--editor-value)' },
]));
const theme = EditorView.theme({
  '&': { height: '100%', color: 'var(--editor-text)', backgroundColor: 'var(--editor-bg)', fontSize: '14px' },
  '&.cm-focused': { outline: 'none' },
  '.cm-scroller': { overflow: 'auto', fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace', lineHeight: '20px' },
  '.cm-content': { padding: '8px 0 24px', caretColor: 'var(--editor-text)' },
  '.cm-line': { padding: '0 12px 0 8px' },
  '.cm-gutters': { backgroundColor: 'var(--editor-gutter)', color: 'var(--editor-muted)', borderRight: '1px solid var(--editor-border)' },
  '.cm-lineNumbers .cm-gutterElement': { minWidth: '46px', padding: '0 8px', fontSize: '12px' },
  '.cm-cursor, .cm-dropCursor': { borderLeftColor: 'var(--editor-text)' },
  '.cm-selectionBackground, &.cm-focused .cm-selectionBackground': { backgroundColor: 'var(--editor-selection)' },
  '.cm-content ::selection': { backgroundColor: 'var(--editor-selection)' },
  '.cm-placeholder': { color: 'var(--editor-muted)' },
});

export function createScriptEditor(parent: HTMLElement, initial: ScriptEditorOptions, onChange: (value: string) => void, onKeydown: (event: KeyboardEvent) => void) {
  let options = initial;
  const configuration = new Compartment();
  const settings = (): Extension[] => [
    EditorState.readOnly.of(options.readonly || options.masked),
    EditorView.editable.of(!options.masked),
    EditorView.contentAttributes.of({
      'aria-label': options.label,
      'aria-readonly': String(options.readonly || options.masked),
      'aria-busy': String(options.busy),
      ...(options.id ? { id: options.id } : {}),
      ...(options.ariaDescribedby ? { 'aria-describedby': options.ariaDescribedby } : {}),
      'data-gramm': 'false', autocapitalize: 'off', spellcheck: 'false',
    }),
    placeholder(options.placeholder),
    options.language === 'env' ? envLanguage : options.language === 'shell' ? shellLanguage : [],
    EditorView.theme({ '.cm-content': { minHeight: `${Math.max(1, options.minimumLines) * 20}px` } }),
  ];
  const extensions: Extension[] = [
    lineNumbers(), highlightSpecialChars(), drawSelection(), dropCursor(), history(),
    keymap.of([...defaultKeymap, ...historyKeymap, indentWithTab]),
    theme, highlight, configuration.of(settings()),
    EditorView.domEventHandlers({ keydown(event) {
      if (!event.isComposing && !options.masked) onKeydown(event);
      return event.defaultPrevented;
    } }),
    EditorView.updateListener.of(update => {
      if (update.docChanged) onChange(update.state.sliceDoc());
    }),
  ];
  const createState = (value: string) => EditorState.create({
    doc: value,
    extensions: [...extensions, EditorState.lineSeparator.of(value.includes('\r\n') ? '\r\n' : '\n')],
  });
  const view = new EditorView({ parent, state: createState(options.modelValue) });
  return {
    configure(next: ScriptEditorOptions) {
      options = next;
      view.dispatch({ effects: configuration.reconfigure(settings()) });
    },
    setValue(value: string) {
      if (value === view.state.sliceDoc()) return;
      // A server reload or a different file starts a new history. Vue echoes of
      // local edits take the equality path above, preserving selection and undo.
      view.setState(createState(value));
      view.dispatch({ effects: configuration.reconfigure(settings()) });
    },
    focus() { view.focus(); },
    destroy() { view.destroy(); },
  };
}
