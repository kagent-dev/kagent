export type HeaderValueSource =
  | { secretKeyRef: { name: string; key: string }; tokenPassthrough?: never; actorToken?: never }
  | { tokenPassthrough: Record<string, never>; secretKeyRef?: never; actorToken?: never }
  | { actorToken: { audiences: string[]; expirationSeconds?: number }; secretKeyRef?: never; tokenPassthrough?: never };

export interface Header {
  name: string;
  prefix?: string;
  valueFrom: HeaderValueSource;
}
