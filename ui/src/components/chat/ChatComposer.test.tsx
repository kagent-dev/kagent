import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ThemeProvider } from "@emotion/react";
import { describe, expect, it, vi } from "vitest";
import { LinkFile } from "@/api/chat/attachments";
import { themeFor } from "@/theme/theme";
import { ChatComposer } from "./ChatComposer";

const TEXT = "read https://files.example/a.pdf and https://example.com/docs";

/** What the composer hands to `send` when the reader types TEXT and presses Enter. */
async function sent(canAttach: boolean | undefined) {
  const send = vi.fn<(text: string, files: File[]) => Promise<void>>(async () => {});
  render(
    <ThemeProvider theme={themeFor("dark")}>
      <ChatComposer send={send} canAttach={canAttach} />
    </ThemeProvider>,
  );
  const input = screen.getByTestId("chat-input");
  fireEvent.change(input, { target: { value: TEXT } });
  fireEvent.keyDown(input, { key: "Enter", code: "Enter", keyCode: 13 });
  await waitFor(() => expect(send).toHaveBeenCalledOnce());
  return send.mock.calls[0];
}

describe("ChatComposer and links in the text", () => {
  it("sends a link to a known file type as a LinkFile, and leaves a page link as text", async () => {
    const [text, files] = await sent(true);
    expect(text).toBe(TEXT);
    expect(files).toHaveLength(1);
    expect(files[0]).toBeInstanceOf(LinkFile);
    expect((files[0] as LinkFile).url).toBe("https://files.example/a.pdf");
  });

  it.each([false, undefined])("sends no link when the agent takes no files (canAttach %s)", async (canAttach) => {
    const [, files] = await sent(canAttach);
    expect(files).toEqual([]);
  });
});
