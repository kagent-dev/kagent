import { ThemeProvider } from "@emotion/react";
import type { ReactElement } from "react";
import { themeFor } from "@/theme/theme";
import { render as rtlRender, screen } from "@testing-library/react";
import { Checkbox, Form, Input, InputNumber, Radio, Select } from "antd";
import { describe, expect, it } from "vitest";
import { FieldLabel, FormField } from "./FormField";
import { ResourceSelect } from "./ResourceSelect";

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

  it.each([
    ["an input", <Input key="c" />],
    ["a text area", <Input.TextArea key="c" />],
    ["a number input", <InputNumber key="c" />],
    ["a select", <Select key="c" options={[]} />],
    ["a resource select", <ResourceSelect key="c" kind="harness" names={[]} loading={false} onChange={() => {}} />],
  ])("ties the label to %s without htmlFor", (_, control) => {
    render(
      <Form layout="vertical">
        <FormField label="Name" required>{control}</FormField>
      </Form>,
    );
    const field = screen.getByLabelText(/Name/);
    expect(field.matches("input, textarea")).toBe(true);
  });

  it("marks the control invalid and points it at the error", () => {
    render(
      <Form layout="vertical">
        <FormField label="Name" required error="A name is required.">
          <Input />
        </FormField>
      </Form>,
    );
    const field = screen.getByLabelText(/Name/);
    expect(field).toHaveAttribute("aria-invalid", "true");
    expect(field).toHaveAccessibleDescription("A name is required.");
  });

  it("points the control at a hint below it", () => {
    render(
      <Form layout="vertical">
        <FormField label="Origin" required help="Scheme and host.">
          <Input />
        </FormField>
      </Form>,
    );
    const field = screen.getByLabelText(/Origin/);
    expect(field).not.toHaveAttribute("aria-invalid");
    expect(field).toHaveAccessibleDescription("Scheme and host.");
  });

  it("keeps a control's own id and the caller's htmlFor", () => {
    render(
      <Form layout="vertical">
        <FormField label="Name" required>
          <Input id="own-id" />
        </FormField>
        <FormField label="Other" required htmlFor="given">
          <Input id="given" />
        </FormField>
      </Form>,
    );
    expect(screen.getByLabelText(/Name/)).toHaveAttribute("id", "own-id");
    expect(screen.getByLabelText(/Other/)).toHaveAttribute("id", "given");
  });

  it("leaves a wrapped control to the caller's htmlFor", () => {
    const { container } = render(
      <Form layout="vertical">
        <FormField label="Name" required htmlFor="inner" error="Too long.">
          <div data-testid="wrapper"><Input id="inner" /></div>
        </FormField>
      </Form>,
    );
    expect(screen.getByTestId("wrapper")).not.toHaveAttribute("id");
    expect(container.querySelectorAll("#inner")).toHaveLength(1);
    expect(screen.getByLabelText(/Name/)).toHaveAttribute("id", "inner");
  });

  it("gives repeated fields distinct ids", () => {
    const { container } = render(
      <Form layout="vertical">
        <FormField label="Key" required error="Enter a key."><Input /></FormField>
        <FormField label="Key" required error="Enter a key."><Input /></FormField>
      </Form>,
    );
    const ids = [...container.querySelectorAll("[id]")].map((node) => node.id);
    expect(new Set(ids).size).toBe(ids.length);
    expect(screen.getAllByLabelText(/Key/)).toHaveLength(2);
  });

  it("names a radio or checkbox group by its label instead of htmlFor", () => {
    const { container } = render(
      <Form layout="vertical">
        <FormField label="Authentication" required>
          <Radio.Group data-testid="auth" options={["apiKey", "none"]} />
        </FormField>
        <FormField label="On days" required>
          <Checkbox.Group data-testid="days" options={["Mon", "Tue"]} />
        </FormField>
      </Form>,
    );
    expect(screen.getByTestId("auth")).toHaveAccessibleName(/^Authentication/);
    expect(screen.getByTestId("days")).toHaveAccessibleName(/^On days/);
    expect(container.querySelector("label[for]")).toBeNull();
  });

  it("shows both a hint and the caller's extra", () => {
    render(
      <Form layout="vertical">
        <FormField label="Time" required help="Local time." extra={<span>Runs daily.</span>}>
          <Input />
        </FormField>
      </Form>,
    );
    expect(screen.getByText("Local time.")).toBeInTheDocument();
    expect(screen.getByText("Runs daily.")).toBeInTheDocument();
  });

  it("lays out vertically with no colon outside an antd Form", () => {
    const { container } = render(
      <FormField label="Name" required>
        <Input />
      </FormField>,
    );
    expect(container.querySelector(".ant-form-item-vertical, .ant-form-vertical")).not.toBeNull();
    expect(container.querySelector(".ant-form-item-no-colon")).not.toBeNull();
  });
});
