import { describe, test, expect, beforeAll } from '@jest/globals';
import { render, screen } from '@testing-library/react';
import { Part } from '@a2a-js/sdk';
import FileAttachment, { formatFileSize } from '@/components/chat/FileAttachment';

beforeAll(() => {
  // jsdom does not implement object URLs; stub them for the component.
  global.URL.createObjectURL = jest.fn(() => 'blob:mock-url');
  global.URL.revokeObjectURL = jest.fn();
});

describe('formatFileSize', () => {
  test('formats bytes, KB, and MB', () => {
    expect(formatFileSize(512)).toBe('512 B');
    expect(formatFileSize(2048)).toBe('2.0 KB');
    expect(formatFileSize(5 * 1024 * 1024)).toBe('5.0 MB');
  });
});

describe('FileAttachment', () => {
  test('renders an image thumbnail for image mime types', () => {
    const part = Part.fromJSON({
      raw: 'AQID',
      filename: 'pic.png',
      mediaType: 'image/png',
    });
    render(<FileAttachment part={part} />);
    const img = screen.getByAltText('pic.png') as HTMLImageElement;
    expect(img).toBeTruthy();
    expect(img.tagName).toBe('IMG');
  });

  test('renders a downloadable chip for non-image types', () => {
    const part = Part.fromJSON({
      raw: 'aGVsbG8=',
      filename: 'notes.txt',
      mediaType: 'text/plain',
    });
    render(<FileAttachment part={part} />);
    expect(screen.getByText('notes.txt')).toBeTruthy();
    const download = screen.getByLabelText('Download notes.txt') as HTMLAnchorElement;
    expect(download).toBeTruthy();
    expect(download.getAttribute('download')).toBe('notes.txt');
    expect(download.getAttribute('href')).toBe('blob:mock-url');
  });
});
