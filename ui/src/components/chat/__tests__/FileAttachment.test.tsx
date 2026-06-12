import { describe, test, expect, beforeAll } from '@jest/globals';
import { render, screen } from '@testing-library/react';
import type { FilePart } from '@a2a-js/sdk';
import FileAttachment, { formatFileSize } from '@/components/chat/FileAttachment';

beforeAll(() => {
  // jsdom does not implement object URLs; stub them for the component.
  global.URL.createObjectURL = jest.fn(() => 'blob:mock-url');
  global.URL.revokeObjectURL = jest.fn();
});

// "hello" base64-encoded.
const HELLO_B64 = 'aGVsbG8=';
// 1x1 transparent PNG.
const PNG_B64 =
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==';

describe('formatFileSize', () => {
  test('formats bytes, KB, and MB', () => {
    expect(formatFileSize(512)).toBe('512 B');
    expect(formatFileSize(2048)).toBe('2.0 KB');
    expect(formatFileSize(5 * 1024 * 1024)).toBe('5.0 MB');
  });
});

describe('FileAttachment', () => {
  test('renders an image thumbnail for image mime types', () => {
    const part: FilePart = {
      kind: 'file',
      file: { name: 'pic.png', mimeType: 'image/png', bytes: PNG_B64 },
    };
    render(<FileAttachment part={part} />);
    const img = screen.getByAltText('pic.png') as HTMLImageElement;
    expect(img).toBeInTheDocument();
    expect(img.tagName).toBe('IMG');
  });

  test('renders a downloadable chip for non-image types', () => {
    const part: FilePart = {
      kind: 'file',
      file: { name: 'notes.txt', mimeType: 'text/plain', bytes: HELLO_B64 },
    };
    render(<FileAttachment part={part} />);
    expect(screen.getByText('notes.txt')).toBeInTheDocument();
    const download = screen.getByLabelText('Download notes.txt') as HTMLAnchorElement;
    expect(download).toBeInTheDocument();
    expect(download.getAttribute('download')).toBe('notes.txt');
    expect(download.getAttribute('href')).toBe('blob:mock-url');
  });
});
