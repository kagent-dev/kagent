# Model provider discovery

A `ModelProviderConfig` (`api.kagent.dev/v1alpha3`) describes one source of
models that kagent can list: an OpenAI-compatible endpoint such as a LiteLLM
proxy. Only `spec.type: OpenAI` is supported. If `spec.endpoint` is omitted, the
default endpoint for the type applies.

```yaml
apiVersion: api.kagent.dev/v1alpha3
kind: ModelProviderConfig
metadata:
  name: litellm
  namespace: kagent
spec:
  type: OpenAI
  endpoint: https://litellm.example/v1
  discoveryAuth:
    secretRef:
      name: litellm-discovery
      key: token
```

## Discovery

Discovery sends `GET <endpoint>/models` with `Accept: application/json`. When
`discoveryAuth` is set, the controller reads the Secret key from the
ModelProviderConfig's own namespace and sends it as `Authorization: Bearer`. The
Secret and key must exist and hold a non-empty value. The response must be a
JSON object with a `data` array of `{"id": ...}` entries. The controller reads at
most 2 MiB, drops empty ids, and returns the ids sorted and de-duplicated. A
request times out after 15 seconds. Redirects are not followed, so the token
only reaches the configured endpoint.

Discovery credentials are never used to invoke models.

## Status

The controller's ModelProviderConfig reconciler publishes the result:

- `Ready=True`, reason `DiscoverySucceeded`, and `status.modelCount` when the
  endpoint returned a model list;
- `Ready=False`, reason `DiscoveryFailed`, the error message, and
  `modelCount: 0` otherwise. The reconciler returns the error, so
  controller-runtime retries with backoff.

It runs again after 5 minutes, and whenever the spec or the referenced Secret
changes. It writes status only when it differs from the current one. Only
Ready configs are listed as configured providers.

## Listing models

The model list is not stored in the cluster. `ModelService.ListProviderModels`
with `refresh: true` fetches it from the endpoint on demand and requires
`update` permission on the ModelProviderConfig. A request without `refresh`
returns not found, because there is no cached inventory.

## Private certificate authorities

The controller verifies TLS with its process trust store. To reach an endpoint
signed by a private CA, mount the CA bundle into the controller and point
`SSL_CERT_FILE` at it, using the `controller.volumes`, `controller.volumeMounts`,
and `controller.env` Helm values.
