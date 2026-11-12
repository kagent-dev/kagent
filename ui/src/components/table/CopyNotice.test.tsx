import { useState } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { CopyNotice } from "./CopyNotice";
import { useMountedBlank } from "./RowActionsMenu";

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

describe("CopyNotice", () => {
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
