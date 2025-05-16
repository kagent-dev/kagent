export default function processSseChunk(chunk: string) {
  const lines = chunk.split('\n\n');
  const events = [];

  for (const line of lines) {
  }
}