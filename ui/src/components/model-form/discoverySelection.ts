/** Map a chosen client protocol to ModelConfig settings, preserving gateway prefixes. */
export function invocationSettings(format: string, endpoint: string): {provider: string; params: Record<string, string>} {
  if (format === "messages") {
    return {provider: "Anthropic", params: {baseUrl: endpoint.replace(/\/v1\/?$/, "")}};
  }
  return {provider: "OpenAI", params: {baseUrl: endpoint, apiFormat: format}};
}
