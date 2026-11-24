import { ThemeProvider } from "@emotion/react";
import type { ReactElement } from "react";
import { themeFor } from "@/theme/theme";
import { fireEvent, render as rtlRender, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { Checkbox, Form, Input, InputNumber, Radio, Select } from "antd";
import { describe, expect, it, vi } from "vitest";
import { FieldLabel, FormField } from "./FormField";
import { ResourceSelect } from "./ResourceSelect";
import { useSubmitValidation } from "./useSubmitValidation";

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

  it("puts the hint between the label and the control, outside antd's help area", () => {
    const { container } = render(
      <Form layout="vertical">
        <FormField label="Origin" required help="Scheme and host.">
          <Input data-testid="control" />
        </FormField>
      </Form>,
    );
    const hint = screen.getByText("Scheme and host.");
    expect(container.querySelector("label")!.compareDocumentPosition(hint) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(hint.compareDocumentPosition(screen.getByTestId("control")) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(container.querySelector(".ant-form-item-explain")).toBeNull();
  });

  it("keeps the hint above and shows the error below the control", () => {
    const { container } = render(
      <Form layout="vertical">
        <FormField label="Origin" required help="Scheme and host." error="Enter an origin.">
          <Input data-testid="control" />
        </FormField>
      </Form>,
    );
    const explain = container.querySelector(".ant-form-item-explain")!;
    expect(explain).toHaveTextContent("Enter an origin.");
    expect(screen.getByText("Scheme and host.")).toBeInTheDocument();
    expect(screen.getByTestId("control").compareDocumentPosition(explain) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(screen.getByLabelText(/Origin/)).toHaveAccessibleDescription("Scheme and host. Enter an origin.");
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

  it("points the control at its hint", () => {
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

  it("shows a hint and the caller's extra together above the control", () => {
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

  it("reports a touch when the user leaves the field or types a value, not for an emptied box", () => {
    const onTouch = vi.fn();
    render(
      <FormField label="Name" required onTouch={onTouch}>
        <Input />
      </FormField>,
    );
    const box = screen.getByLabelText(/^Name/);
    fireEvent.input(box, { target: { value: " " } });
    expect(onTouch).not.toHaveBeenCalled();
    fireEvent.input(box, { target: { value: "a" } });
    expect(onTouch).toHaveBeenCalledTimes(1);
    fireEvent.blur(box);
    expect(onTouch).toHaveBeenCalledTimes(2);
  });

  it("passes every keystroke to onType instead of touching, and still touches on leaving", () => {
    const onTouch = vi.fn();
    const onType = vi.fn();
    render(
      <FormField label="Name" required onTouch={onTouch} onType={onType}>
        <Input />
      </FormField>,
    );
    const box = screen.getByLabelText(/^Name/);
    fireEvent.input(box, { target: { value: "a" } });
    fireEvent.input(box, { target: { value: "" } });
    expect(onType.mock.calls).toEqual([["a"], [""]]);
    expect(onTouch).not.toHaveBeenCalled();
    fireEvent.blur(box);
    expect(onTouch).toHaveBeenCalledOnce();
  });

  it("keeps the message's line whether or not there is a message", () => {
    const { container, rerender } = render(
      <FormField label="Name" required onTouch={() => {}}>
        <Input />
      </FormField>,
    );
    expect(container.querySelector(".ant-form-item-explain")).not.toBeNull();
    rerender(
      <ThemeProvider theme={themeFor("light")}>
        <FormField label="Name" required onTouch={() => {}} error="A name is required.">
          <Input />
        </FormField>
      </ThemeProvider>,
    );
    expect(container.querySelector(".ant-form-item-explain")?.textContent).toBe("A name is required.");
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

describe("FormField with a picker", () => {
  function Picker() {
    const [value, setValue] = useState<string>();
    const validation = useSubmitValidation(value ? [] : [{ field: "template", message: "Choose a template." }]);
    return (
      <>
        <FormField label="Template" required {...validation.field("template")}>
          <ResourceSelect kind="template" names={["alpha", "beta"]} loading={false} value={value} onChange={setValue} />
        </FormField>
        <button>Next</button>
      </>
    );
  }

  it("shows no error while typing in the search, one on blur with no value, and clears it on pick", async () => {
    render(<Picker />);
    await userEvent.click(screen.getByRole("combobox"));
    await userEvent.keyboard("alp");
    expect(screen.queryByText("Choose a template.")).toBeNull();

    await userEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(screen.getByText("Choose a template.")).toBeInTheDocument();
    expect(screen.getByRole("combobox")).toHaveAttribute("aria-invalid", "true");

    await userEvent.click(screen.getByRole("combobox"));
    await userEvent.click(screen.getByTitle("alpha"));
    expect(screen.getByRole("combobox")).not.toHaveAttribute("aria-invalid", "true");
  });
});
