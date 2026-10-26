import { useState } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { Header } from "@/api";
import { CredentialHeadersEditor } from "./CredentialHeadersEditor";

function Editor({ initial }: { initial: Header[] }) {
  const [headers, setHeaders] = useState(initial);
  return <>
    <CredentialHeadersEditor value={headers} onChange={setHeaders} />
    <output data-testid="headers">{JSON.stringify(headers)}</output>
  </>;
}

function value(): Header[] {
  return JSON.parse(screen.getByTestId("headers").textContent ?? "[]") as Header[];
}

describe("CredentialHeadersEditor", () => {
  it("switches to an actor token and edits audiences and optional lifetime", async () => {
    const user = userEvent.setup();
    render(<Editor initial={[{
      name: "Authorization", prefix: "Bearer ", valueFrom: { secretKeyRef: { name: "old-secret", key: "token" } },
    }]} />);
    await user.click(screen.getByRole("combobox", { name: "Header 1 credential source" }));
    await user.click(screen.getByText("Actor token"));
    expect(value()).toEqual([{
      name: "Authorization", prefix: "Bearer ", valueFrom: { actorToken: { audiences: [] } },
    }]);
    expect(screen.queryByLabelText("Header 1 Secret name")).not.toBeInTheDocument();
    await user.type(screen.getByRole("combobox", { name: "Header 1 audiences" }), "https://service.example{Enter}");
    await user.type(screen.getByRole("combobox", { name: "Header 1 audiences" }), "other-service{Enter}");
    const lifetime = screen.getByRole("spinbutton", { name: "Header 1 lifetime in seconds" });
    expect(lifetime).toHaveAttribute("placeholder", "900");
    await user.type(lifetime, "1800");
    expect(value()[0].valueFrom.actorToken).toEqual({
      audiences: ["https://service.example", "other-service"], expirationSeconds: 1800,
    });
    await user.clear(lifetime);
    expect(value()[0].valueFrom.actorToken).toEqual({ audiences: ["https://service.example", "other-service"] });
  });

  it("removes actor settings when switching to invocation passthrough", async () => {
    const user = userEvent.setup();
    render(<Editor initial={[{
      name: "Authorization", prefix: "Bearer ", valueFrom: { actorToken: { audiences: ["service"], expirationSeconds: 600 } },
    }]} />);
    await user.click(screen.getByRole("combobox", { name: "Header 1 credential source" }));
    await user.click(screen.getByText("Invocation token"));
    expect(value()).toEqual([{ name: "Authorization", prefix: "Bearer ", valueFrom: { tokenPassthrough: {} } }]);
    expect(screen.queryByRole("combobox", { name: "Header 1 audiences" })).not.toBeInTheDocument();
  });
});
