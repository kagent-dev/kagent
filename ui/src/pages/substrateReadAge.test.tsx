import { act, fireEvent, render, screen } from "@testing-library/react";
import { ThemeProvider } from "@emotion/react";
import { MemoryRouter } from "react-router-dom";
import { SWRConfig } from "swr";
import { afterEach, expect, it, vi } from "vitest";
import { createRouterTransport } from "@connectrpc/connect";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { SystemService } from "@/generated/kagent/api/v1/system_pb";
import { setApiTransport } from "@/api/transport";
import { themeFor } from "@/theme/theme";
import { SubstratePage } from "./SubstratePage";

afterEach(() => {
  setApiTransport(undefined);
  vi.useRealTimers();
});

// The lists answer once, then hang: polling keeps asking and no new read lands.
it("shows how long a polled list has gone without a new read", async () => {
  vi.useFakeTimers({ toFake: ["setInterval", "clearInterval", "Date"] });
  const hang = () => new Promise<never>(() => {});
  let actorReads = 0;
  setApiTransport(createRouterTransport(({ service }) => service(SystemService, {
    listNamespaces: () => ({}),
    getSubstrateSummary: () => ({}),
    listSubstrateActors: () =>
      actorReads++ === 0 ? { computedAt: timestampFromDate(new Date()) } : hang(),
    listSubstrateWorkers: () => ({ computedAt: timestampFromDate(new Date()) }),
  })));
  render(<ThemeProvider theme={themeFor("dark")}>
    <SWRConfig value={{ provider: () => new Map(), shouldRetryOnError: false }}>
      <MemoryRouter><SubstratePage /></MemoryRouter>
    </SWRConfig>
  </ThemeProvider>);

  const [actorsAge] = await screen.findAllByText(/^Read at /);
  expect(actorsAge).not.toHaveTextContent(/ago$/);

  fireEvent.click(screen.getByTestId("substrate-poll-toggle"));
  await act(async () => {
    vi.advanceTimersByTime(8_000);
  });

  expect(actorsAge).toHaveTextContent(/^Read at .+, \d+s ago$/);
});
