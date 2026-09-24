import {
  afterNextRender,
  ChangeDetectionStrategy,
  Component,
  DestroyRef,
  effect,
  ElementRef,
  inject,
  input,
  viewChild,
} from '@angular/core';
import { defaultKeymap } from '@codemirror/commands';
import { json } from '@codemirror/lang-json';
import { bracketMatching, foldGutter, foldKeymap, HighlightStyle, syntaxHighlighting } from '@codemirror/language';
import { highlightSelectionMatches, search, searchKeymap } from '@codemirror/search';
import { EditorState } from '@codemirror/state';
import { EditorView, highlightActiveLine, keymap, lineNumbers } from '@codemirror/view';
import { tags } from '@lezer/highlight';

/*
 * Colours are the console's roles, as CSS variables, so the viewer follows
 * the theme without being rebuilt when it changes.
 */
const theme = EditorView.theme({
  '&': {
    fontSize: '12.5px',
    backgroundColor: 'transparent',
    color: 'var(--ink)',
  },
  '&.cm-focused': { outline: 'none' },
  '.cm-scroller': { fontFamily: "'IBM Plex Mono', ui-monospace, SFMono-Regular, Menlo, Consolas, monospace", lineHeight: '1.6' },
  '.cm-content': { padding: '10px 0', caretColor: 'var(--ink)' },
  '.cm-gutters': {
    backgroundColor: 'var(--sunken)',
    color: 'var(--faint)',
    border: 'none',
    borderRight: '1px solid var(--line)',
  },
  '.cm-activeLine': { backgroundColor: 'color-mix(in srgb, var(--accent-soft) 45%, transparent)' },
  '.cm-activeLineGutter': { backgroundColor: 'transparent', color: 'var(--muted)' },
  '.cm-selectionBackground, &.cm-focused .cm-selectionBackground, .cm-content ::selection': {
    backgroundColor: 'var(--accent-soft) !important',
  },
  '.cm-matchingBracket': { backgroundColor: 'var(--accent-soft)', outline: '1px solid var(--line-strong)' },
  '.cm-foldPlaceholder': { backgroundColor: 'var(--sunken)', border: '1px solid var(--line)', color: 'var(--muted)' },
  '.cm-searchMatch': { backgroundColor: 'var(--warn-soft)', outline: '1px solid var(--warn)' },
  '.cm-panels': { backgroundColor: 'var(--sunken)', color: 'var(--ink)', borderColor: 'var(--line)' },
});

const highlight = HighlightStyle.define([
  { tag: tags.propertyName, color: 'var(--accent-ink)' },
  { tag: tags.string, color: 'var(--calc)' },
  { tag: tags.number, color: 'var(--warn)' },
  { tag: [tags.bool, tags.null], color: 'var(--bad)' },
  { tag: [tags.brace, tags.squareBracket, tags.separator, tags.punctuation], color: 'var(--muted)' },
]);

/**
 * JSON in a read-only editor: highlighted, foldable, searchable, and
 * selectable for copying.
 */
@Component({
  selector: 'app-json-viewer',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `<div #host class="max-h-[75vh] overflow-auto"></div>`,
})
export class JsonViewer {
  readonly value = input.required<string>();
  private readonly host = viewChild.required<ElementRef<HTMLElement>>('host');
  private view?: EditorView;

  constructor() {
    afterNextRender(() => {
      this.view = new EditorView({
        parent: this.host().nativeElement,
        state: EditorState.create({
          doc: this.value(),
          extensions: [
            EditorState.readOnly.of(true),
            lineNumbers(),
            foldGutter(),
            highlightActiveLine(),
            bracketMatching(),
            search({ top: true }),
            highlightSelectionMatches(),
            keymap.of([...defaultKeymap, ...foldKeymap, ...searchKeymap]),
            json(),
            syntaxHighlighting(highlight),
            EditorView.lineWrapping,
            theme,
          ],
        }),
      });
    });
    // A new answer replaces the document, keeping the viewer where it is.
    effect(() => {
      const value = this.value();
      const view = this.view;
      if (view && view.state.doc.toString() !== value) {
        view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: value } });
      }
    });
    inject(DestroyRef).onDestroy(() => this.view?.destroy());
  }
}
