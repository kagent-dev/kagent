import { ThemeProvider } from "@emotion/react";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { themeFor } from "@/theme/theme";
import { ValidationNotice } from "./ValidationNotice";
import { FIX_FIELDS_MESSAGE } from "./useSubmitValidation";

describe("ValidationNotice", () => {
  it("asks for the required fields and offers no retry", () => {
    render(
      <ThemeProvider theme={themeFor("light")}>
        <ValidationNotice />
      </ThemeProvider>,
    );
    expect(screen.getByText(FIX_FIELDS_MESSAGE)).toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });
});
