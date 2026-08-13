import { fn } from "storybook/test";

import type { BaseResponse } from "@/types";

export interface NamespaceResponse {
  name: string;
  status: string;
}

export const listNamespaces = fn<
  () => Promise<BaseResponse<NamespaceResponse[]>>
>(async () => ({
  message: "Namespaces fetched",
  data: [
    { name: "default", status: "Active" },
    { name: "kagent", status: "Active" },
  ],
}));
