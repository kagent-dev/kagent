import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ValidationNotice } from "./ValidationNotice";
import { FIX_FIELDS_MESSAGE, FIX_FIELDS_TITLE } from "./useSubmitValidation";

describe("ValidationNotice", () => {
  it("says nothing was sent and offers no retry", () => {
    render(<ValidationNotice />);
    expect(screen.getByText(FIX_FIELDS_TITLE)).toBeInTheDocument();
    expect(screen.getByText(FIX_FIELDS_MESSAGE)).toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });
});
