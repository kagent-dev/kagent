import { ThemeProvider } from "@emotion/react";
import type { ReactElement } from "react";
import { themeFor } from "@/theme/theme";
import { render as rtlRender, screen } from "@testing-library/react";
import { Form, Input } from "antd";
import { describe, expect, it } from "vitest";
import { FieldLabel, FormField } from "./FormField";

const render = (ui: ReactElement) =>
  rtlRender(<ThemeProvider theme={themeFor("dark")}>{ui}</ThemeProvider>);

describe("FieldLabel", () => {
  it("marks a required field with an asterisk and no optional text", () => {
    render(<FieldLabel required>Name</FieldLabel>);
    expect(screen.getByTestId("field-required")).toHaveTextContent("*");
    expect(screen.queryByText("(optional)")).not.toBeInTheDocument();
  });

  it("marks an optional field in words, not with an asterisk", () => {
    render(<FieldLabel required={false}>Description</FieldLabel>);
    expect(screen.getByText("(optional)")).toBeInTheDocument();
    expect(screen.queryByTestId("field-required")).not.toBeInTheDocument();
  });
});

describe("FormField", () => {
  it("replaces antd's own required mark and shows the error under the field", () => {
    const { container } = render(
      <Form layout="vertical">
        <FormField label="Name" required error="A name is required.">
          <Input />
        </FormField>
      </Form>,
    );
    expect(screen.getByTestId("field-required")).toBeInTheDocument();
    expect(container.querySelector(".ant-form-item-required")).toBeNull();
    expect(screen.getByText("A name is required.")).toBeInTheDocument();
    expect(container.querySelector(".ant-form-item-has-error")).not.toBeNull();
  });

  // antd grows its help area from zero height on mount, which moves every control below it.
  it("shows a hint in the static extra area, not the animated help area", () => {
    const { container } = render(
      <Form layout="vertical">
        <FormField label="Origin" required help="Scheme and host.">
          <Input />
        </FormField>
      </Form>,
    );
    expect(container.querySelector(".ant-form-item-extra")).toHaveTextContent("Scheme and host.");
    expect(container.querySelector(".ant-form-item-explain")).toBeNull();
  });

  it("shows the error instead of the hint once there is one", () => {
    const { container } = render(
      <Form layout="vertical">
        <FormField label="Origin" required help="Scheme and host." error="Enter an origin.">
          <Input />
        </FormField>
      </Form>,
    );
    expect(container.querySelector(".ant-form-item-explain")).toHaveTextContent("Enter an origin.");
    expect(screen.queryByText("Scheme and host.")).not.toBeInTheDocument();
  });

  it("puts an above hint before the control and keeps it beside an error", () => {
    render(
      <Form layout="vertical">
        <FormField label="Rows" required hintPlacement="above" help="One row each." error="Add a row.">
          <Input data-testid="control" />
        </FormField>
      </Form>,
    );
    const hint = screen.getByText("One row each.");
    expect(hint.compareDocumentPosition(screen.getByTestId("control")) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(screen.getByText("Add a row.")).toBeInTheDocument();
  });
});
