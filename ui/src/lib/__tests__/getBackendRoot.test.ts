import { getBackendRoot } from "@/lib/utils";

describe("getBackendRoot", () => {
  const original = process.env.BACKEND_INTERNAL_URL;

  afterEach(() => {
    if (original === undefined) {
      delete process.env.BACKEND_INTERNAL_URL;
    } else {
      process.env.BACKEND_INTERNAL_URL = original;
    }
  });

  it("strips a trailing /api segment", () => {
    process.env.BACKEND_INTERNAL_URL = "http://controller.kagent.svc/api";
    expect(getBackendRoot()).toBe("http://controller.kagent.svc");
  });

  it("strips a trailing /api/ segment", () => {
    process.env.BACKEND_INTERNAL_URL = "http://controller.kagent.svc/api/";
    expect(getBackendRoot()).toBe("http://controller.kagent.svc");
  });

  it("leaves URLs without an /api suffix unchanged", () => {
    process.env.BACKEND_INTERNAL_URL = "http://controller.kagent.svc";
    expect(getBackendRoot()).toBe("http://controller.kagent.svc");
  });

  it("returns an empty root for a relative /api base (same-origin)", () => {
    process.env.BACKEND_INTERNAL_URL = "/api";
    expect(getBackendRoot()).toBe("");
  });
});
