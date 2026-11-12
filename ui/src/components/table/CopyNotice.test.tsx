import { useState } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { CopyNotice } from "./CopyNotice";
import { useMountedBlank } from "./copyFrom";

const from = { namespace: "kagent", name: "src" };

/** A form keyed on the requested source, whose source load can be flipped from failed to loaded. */
function Page() {
  const [loaded, setLoaded] = useState(false);
  const mountedBlank = useMountedBlank("kagent/src", true, loaded);
  return (
    <>
      <button onClick={() => setLoaded(true)}>load</button>
      <CopyNotice kind="agent" from={from} error={loaded ? undefined : "boom"} loaded={loaded} mountedBlank={mountedBlank} testId="copy" />
    </>
  );
}

/** A form whose source is dropped (`?from=` removed) and whose load then finishes. */
function Dropped() {
  const [source, setSource] = useState<string | undefined>("kagent/src");
  const [loaded, setLoaded] = useState(false);
  const mountedBlank = useMountedBlank(source, true, loaded);
  return (
    <>
      <button onClick={() => setSource(undefined)}>drop</button>
      <button onClick={() => setLoaded(true)}>load</button>
      <CopyNotice kind="agent" from={from} loaded={loaded} mountedBlank={mountedBlank} testId="copy" />
    </>
  );
}

describe("CopyNotice", () => {
  it("does not warn about a copy source on a plain new form", async () => {
    render(<Dropped />);
    await userEvent.click(screen.getByText("drop"));
    await userEvent.click(screen.getByText("load"));
    expect(screen.queryByTestId("copy-late")).toBeNull();
  });

  it("says when the source is not there", () => {
    render(<CopyNotice kind="harness" from={from} notFound loaded={false} mountedBlank={false} testId="copy" />);
    expect(screen.getByTestId("copy-not-found")).toHaveTextContent("Harness not found");
    expect(screen.getByTestId("copy-not-found")).toHaveTextContent("No harness kagent/src to copy. Starting from a blank form.");
  });

  it("keeps explaining a source that loads after the form began blank", async () => {
    render(<Page />);
    expect(screen.getByTestId("copy")).toHaveTextContent("boom Starting from a blank form.");

    await userEvent.click(screen.getByText("load"));

    expect(screen.queryByTestId("copy")).toBeNull();
    expect(screen.getByTestId("copy-late")).toHaveTextContent(
      "Copy source kagent/src has loaded, but it was not applied so your edits are kept.",
    );
  });

  it("stays silent when the source was there from the start", () => {
    const { container } = render(<CopyNotice kind="agent" from={from} loaded mountedBlank={false} testId="copy" />);
    expect(container).toBeEmptyDOMElement();
  });
});
