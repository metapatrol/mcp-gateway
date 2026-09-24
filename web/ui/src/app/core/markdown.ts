import MarkdownIt from 'markdown-it';

/*
 * Instructions are written by whoever runs a backend, so they are rendered as
 * text that may not do anything:
 *
 *   - html: false — HTML in the Markdown is escaped, never parsed.
 *   - markdown-it refuses javascript:, vbscript:, file: and data: links.
 *   - Angular sanitizes what goes into [innerHTML] again.
 *   - The console's Content-Security-Policy runs no inline script and loads
 *     no image from elsewhere, so a tracking pixel stays a broken image.
 *
 * Links open in a new tab, without an opener or a referrer.
 */
const md = new MarkdownIt({ html: false, linkify: true, typographer: false });

const defaultLink = md.renderer.rules['link_open'] ?? ((tokens, idx, options, _env, self) => self.renderToken(tokens, idx, options));
md.renderer.rules['link_open'] = (tokens, idx, options, env, self) => {
  tokens[idx].attrSet('target', '_blank');
  tokens[idx].attrSet('rel', 'noopener noreferrer');
  return defaultLink(tokens, idx, options, env, self);
};

export function renderMarkdown(text: string): string {
  return md.render(text);
}
