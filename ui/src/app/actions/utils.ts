import { getBackendUrl } from "@/lib/utils";

export async function getCurrentUserId() {
  // TODO: this should come from login state
  return "admin@kagent.dev";
}

type ApiOptions = RequestInit & {
  method?: "GET" | "POST" | "PUT" | "DELETE" | "PATCH";
};

export async function fetchApi<T>(path: string, options: ApiOptions = {}): Promise<T> {
  const userId = await getCurrentUserId();
  // Ensure path starts with a slash
  const cleanPath = path.startsWith("/") ? path : `/${path}`;
  const url = `${getBackendUrl()}${cleanPath}`;
  const urlWithUser = url.includes("?") ? `${url}&user_id=${userId}` : `${url}?user_id=${userId}`;
  try {
    const response = await fetch(urlWithUser, {
      ...options,
      headers: {
        "Content-Type": "application/json",
        Accept: "application/json",
        ...options.headers,
      },
      signal: AbortSignal.timeout(15000), // 15 second timeout
    });

    if (!response.ok) {
      const errorMessage = `Request failed with status ${response.status}. ${url}`;
      throw new Error(errorMessage);
    }

    // Handle 204 No Content response (common for DELETE)
    if (response.status === 204) {
      return {} as T;
    }

    const contentType = response.headers.get("content-type");
    if (!contentType || !contentType.includes("application/json")) {
      throw new Error("Response was not JSON");
    }

    const jsonResponse = await response.json();
    return jsonResponse?.data || jsonResponse;
  } catch (error) {
    if (error instanceof TypeError && error.message === "Failed to fetch") {
      throw new Error(`Network error - Could not reach backend server. ${url}`);
    }
    if (error instanceof DOMException && error.name === "AbortError") {
      throw new Error(`Request timed out - server took too long to respond. ${url}`);
    }

    console.error("Error in fetchApi:", {
      path,
      url: `${path}`,
      error: error instanceof Error ? error.message : "Unknown error",
    });

    // Include more error details for debugging
    throw new Error(`Failed to fetch (${url}): ${error instanceof Error ? error.message : "Unknown error"}`);
  }
}
