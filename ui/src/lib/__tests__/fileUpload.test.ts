import { describe, test, expect } from '@jest/globals';
import {
  isAllowedFile,
  validateFile,
  fileToFilePart,
  MAX_FILE_BYTES,
} from '@/lib/fileUpload';

/** Build a File-like object with a controllable size for validation tests. */
function makeFile(name: string, type: string, size = 10): File {
  const file = new File([new Uint8Array(Math.min(size, 1024))], name, { type });
  // Override size for large-file tests without allocating real memory.
  Object.defineProperty(file, 'size', { value: size });
  return file;
}

describe('isAllowedFile', () => {
  test('accepts images, pdf, text, csv, json by mime', () => {
    expect(isAllowedFile(makeFile('a.png', 'image/png'))).toBe(true);
    expect(isAllowedFile(makeFile('a.pdf', 'application/pdf'))).toBe(true);
    expect(isAllowedFile(makeFile('a.txt', 'text/plain'))).toBe(true);
    expect(isAllowedFile(makeFile('a.csv', 'text/csv'))).toBe(true);
    expect(isAllowedFile(makeFile('a.json', 'application/json'))).toBe(true);
  });

  test('accepts by extension when mime is empty', () => {
    expect(isAllowedFile(makeFile('a.md', ''))).toBe(true);
    expect(isAllowedFile(makeFile('a.csv', ''))).toBe(true);
    expect(isAllowedFile(makeFile('a.yaml', ''))).toBe(true);
    expect(isAllowedFile(makeFile('a.yml', ''))).toBe(true);
    expect(isAllowedFile(makeFile('a.xml', ''))).toBe(true);
  });

  test('accepts yaml and xml by mime', () => {
    expect(isAllowedFile(makeFile('a.xml', 'application/xml'))).toBe(true);
    expect(isAllowedFile(makeFile('a.xml', 'text/xml'))).toBe(true);
    expect(isAllowedFile(makeFile('a.yaml', 'application/x-yaml'))).toBe(true);
  });

  test('rejects disallowed types', () => {
    expect(isAllowedFile(makeFile('a.exe', 'application/octet-stream'))).toBe(false);
    expect(isAllowedFile(makeFile('a.zip', 'application/zip'))).toBe(false);
  });
});

describe('validateFile', () => {
  test('returns null for a valid file', () => {
    expect(validateFile(makeFile('a.txt', 'text/plain', 100))).toBeNull();
  });

  test('rejects disallowed type with a message', () => {
    expect(validateFile(makeFile('a.zip', 'application/zip'))).toMatch(/not an allowed/);
  });

  test('rejects oversized files', () => {
    expect(validateFile(makeFile('big.txt', 'text/plain', MAX_FILE_BYTES + 1))).toMatch(/10 MB/);
  });
});

describe('fileToFilePart', () => {
  test('produces a base64 inline file part', async () => {
    const file = new File(['hello'], 'note.txt', { type: 'text/plain' });
    const part = await fileToFilePart(file);
    expect(part.kind).toBe('file');
    expect(part.file.name).toBe('note.txt');
    expect(part.file.mimeType).toBe('text/plain');
    // "hello" → base64
    expect((part.file as { bytes: string }).bytes).toBe('aGVsbG8=');
  });
});
