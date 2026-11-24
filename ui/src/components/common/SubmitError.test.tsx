import { render, screen } from "@testing-library/react";
import { Code, ConnectError } from "@connectrpc/connect";
import { describe, expect, it } from "vitest";
import { fromConnectError } from "@/api/ApiError";
import { SubmitError } from "./SubmitError";

const rejected = (code: Code) => fromConnectError(new ConnectError("server text", code), "rpc");

describe("SubmitError", () => {
  it("words AlreadyExists as a name clash", () => {
    render(<SubmitError error={rejected(Code.AlreadyExists)} what="schedule" onRetry={() => {}} />);
    expect(screen.getByText("The schedule was not created")).toBeInTheDocument();
    expect(screen.getByText(/already exists in this namespace/)).toBeInTheDocument();
  });

  it("words Aborted as a stale edit, not a name clash", () => {
    render(<SubmitError error={rejected(Code.Aborted)} what="schedule" outcome="saved" onRetry={() => {}} />);
    expect(screen.getByText("The schedule was not saved")).toBeInTheDocument();
    expect(screen.getByText(/This schedule changed since you opened it/)).toBeInTheDocument();
    expect(screen.queryByText(/already exists/)).not.toBeInTheDocument();
  });
});
