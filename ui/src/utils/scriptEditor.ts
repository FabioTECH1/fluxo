import { Compartment, EditorState, type Extension } from '@codemirror/state';
import { EditorView, drawSelection, dropCursor, highlightSpecialChars, keymap, lineNumbers, placeholder } from '@codemirror/view';
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands';
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language';
import { tags } from '@lezer/highlight';
import type { ScriptEditorLanguage } from './editorLanguage';
import { loadScriptEditorLanguage } from './scriptEditorLanguages';

export interface ScriptEditorOptions {
  modelValue: string;
  language: ScriptEditorLanguage;
  label: string;
  id?: string;
  ariaDescribedby?: string;
  placeholder: string;
  readonly: boolean;
  masked: boolean;
  busy: boolean;
  minimumLines: number;
}

const highlight = syntaxHighlighting(HighlightStyle.define([
  { tag: tags.comment, color: 'var(--editor-muted)', fontStyle: 'italic' },
  { tag: [tags.propertyName, tags.keyword, tags.definitionKeyword, tags.function(tags.variableName), tags.tagName, tags.attributeName, tags.typeName, tags.className, tags.namespace], color: 'var(--editor-key)' },
  { tag: [tags.string, tags.number, tags.bool, tags.null, tags.regexp, tags.atom], color: 'var(--editor-value)' },
  { tag: [tags.operator, tags.meta], color: 'var(--editor-operator)' },
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
  let languageExtension: Extension = [];
  let languageRequest = 0;
  let languageFailed = false;
  let destroyed = false;
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
    languageExtension,
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
  const configureLanguage = () => {
    const request = ++languageRequest;
    languageFailed = false;
    void loadScriptEditorLanguage(options.language).then(extension => {
      if (destroyed || request !== languageRequest) return;
      languageExtension = extension;
      view.dispatch({ effects: configuration.reconfigure(settings()) });
    }).catch(() => {
      // Optional grammar loading must not prevent reading or saving a file.
      if (!destroyed && request === languageRequest) languageFailed = true;
    });
  };
  configureLanguage();
  return {
    configure(next: ScriptEditorOptions) {
      const reloadLanguage = options.language !== next.language || languageFailed;
      options = next;
      if (reloadLanguage) {
        languageExtension = [];
        configureLanguage();
      }
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
    destroy() { destroyed = true; view.destroy(); },
  };
}
