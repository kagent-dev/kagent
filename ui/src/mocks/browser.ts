import { setupWorker } from "msw/browser";
import { handlers } from "./handlers";

/**
 * The HTTP half of the mock backend — `/oauth2/userinfo`, and nothing else.
 *
 * The API itself is served as a transport rather than over the network, so this
 * worker is no longer where the fixtures live. Started by `startMockBackend`.
 */
export const worker = setupWorker(...handlers);
