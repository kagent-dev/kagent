import { ThemeProvider } from "@emotion/react";
import type { ReactElement } from "react";
import { themeFor } from "@/theme/theme";
import { fireEvent, render as rtlRender, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { BackLink } from "./BackLink";
import { FormActions } from "./FormActions";
import { FIX_FIELDS_MESSAGE } from "./useSubmitValidation";

const render = (ui: ReactElement) =>
  rtlRender(<ThemeProvider theme={themeFor("dark")}>{ui}</ThemeProvider>);

describe("FormActions", () => {
  it("puts the error above the buttons and submits", () => {
    const onSubmit = vi.fn();
    render(
      <MemoryRouter>
        <FormActions submitLabel="Create" onSubmit={onSubmit} cancel={{ onClick: () => {} }} error={<p>Failed</p>} testIdPrefix="model" />
      </MemoryRouter>,
    );
    const error = screen.getByText("Failed");
    const submit = screen.getByTestId("model-submit");
    expect(error.compareDocumentPosition(submit) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    fireEvent.click(submit);
    expect(onSubmit).toHaveBeenCalledTimes(1);
  });

  it("holds a blocked submit back and points the button at the reason", () => {
    const onSubmit = vi.fn();
    render(
      <MemoryRouter>
        <FormActions submitLabel="Create" onSubmit={onSubmit} cancel={{ onClick: () => {} }} blocked testIdPrefix="model" />
      </MemoryRouter>,
    );
    const submit = screen.getByTestId("model-submit");
    expect(submit).toHaveAttribute("aria-disabled", "true");
    expect(submit).toHaveAccessibleDescription(FIX_FIELDS_MESSAGE);
    expect(screen.getByTestId("model-blocked")).toHaveTextContent(FIX_FIELDS_MESSAGE);
    fireEvent.click(submit);
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("navigates to the cancel route", () => {
    render(
      <MemoryRouter initialEntries={["/models/new"]}>
        <Routes>
          <Route path="/models/new" element={<FormActions submitLabel="Create" onSubmit={() => {}} cancel={{ to: "/models" }} />} />
          <Route path="/models" element={<p>Models list</p>} />
        </Routes>
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByTestId("form-cancel"));
    expect(screen.getByText("Models list")).toBeInTheDocument();
  });
});

describe("BackLink", () => {
  it("names the list it returns to", () => {
    render(
      <MemoryRouter>
        <BackLink to="/models" resource="Models" />
      </MemoryRouter>,
    );
    expect(screen.getByRole("link", { name: "Models" })).toHaveAttribute("href", "/models");
  });
});
