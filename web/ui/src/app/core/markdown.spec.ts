import { renderMarkdown } from './markdown';

describe('renderMarkdown', () => {
  it('renders Markdown', () => {
    const html = renderMarkdown('## weather\n\nTools from this server: `get_alerts`\n\n- one\n- two');
    expect(html).toContain('<h2>weather</h2>');
    expect(html).toContain('<code>get_alerts</code>');
    expect(html).toContain('<li>one</li>');
  });

  it('escapes HTML instead of parsing it', () => {
    const html = renderMarkdown('<script>alert(1)</script>\n\n<img src=x onerror=alert(1)>');
    expect(html).not.toContain('<script');
    expect(html).not.toContain('<img');
    expect(html).toContain('&lt;script&gt;');
  });

  it('makes no link of a script URL', () => {
    const html = renderMarkdown('[click](javascript:alert(1)) and [data](data:text/html,x)');
    expect(html).not.toContain('href="javascript:');
    expect(html).not.toContain('href="data:');
  });

  it('opens links in a new tab, without opener or referrer', () => {
    expect(renderMarkdown('[docs](https://example.com)')).toContain(
      '<a href="https://example.com" target="_blank" rel="noopener noreferrer">docs</a>',
    );
  });
});
