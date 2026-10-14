import { ThemeProvider } from "@emotion/react";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { themeFor } from "@/theme/theme";
import { AttachmentChip } from "./AttachmentChip";

const URL_LINK = "https://www.w3.org/WAI/ER/tests/xhtml/testfiles/resources/pdf/dummy.pdf";

const renderChip = (props: Parameters<typeof AttachmentChip>[0]) =>
  render(
    <ThemeProvider theme={themeFor("light")}>
      <AttachmentChip {...props} />
    </ThemeProvider>,
  );

describe("AttachmentChip", () => {
  it("shows a sent link as a link that opens in a new tab, with its host", () => {
    renderChip({ file: { name: "dummy.pdf", mediaType: "application/pdf", url: URL_LINK } });

    const chip = screen.getByRole("link", { name: "Open dummy.pdf" });
    expect(chip.getAttribute("href")).toBe(URL_LINK);
    expect(chip.getAttribute("target")).toBe("_blank");
    expect(chip.hasAttribute("download")).toBe(false);
    expect(screen.getByText("www.w3.org")).toBeTruthy();
  });
});
