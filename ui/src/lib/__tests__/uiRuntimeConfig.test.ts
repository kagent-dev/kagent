import { getUiRuntimeConfig } from "@/app/actions/config";

describe("getUiRuntimeConfig", () => {
  const originalNamespace = process.env.KAGENT_NAMESPACE;

  afterEach(() => {
    if (originalNamespace === undefined) {
      delete process.env.KAGENT_NAMESPACE;
    } else {
      process.env.KAGENT_NAMESPACE = originalNamespace;
    }
  });

  it("defaults the namespace to kagent when KAGENT_NAMESPACE is unset", async () => {
    delete process.env.KAGENT_NAMESPACE;

    await expect(getUiRuntimeConfig()).resolves.toMatchObject({
      defaultNamespace: "kagent",
    });
  });

  it("uses the install namespace provided by the Helm chart", async () => {
    process.env.KAGENT_NAMESPACE = "dev-kagent";

    await expect(getUiRuntimeConfig()).resolves.toMatchObject({
      defaultNamespace: "dev-kagent",
    });
  });

  it("ignores a blank install namespace", async () => {
    process.env.KAGENT_NAMESPACE = "   ";

    await expect(getUiRuntimeConfig()).resolves.toMatchObject({
      defaultNamespace: "kagent",
    });
  });
});
