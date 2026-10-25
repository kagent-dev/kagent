export type HeaderValueSource =
  | { secretKeyRef: { name: string; key: string }; tokenPassthrough?: never }
  | { tokenPassthrough: Record<string, never>; secretKeyRef?: never };

export interface Header {
  name: string;
  prefix?: string;
  valueFrom: HeaderValueSource;
}
