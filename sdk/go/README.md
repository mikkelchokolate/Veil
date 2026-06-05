# Veil Go client

This package is generated from [`docs/openapi.yaml`](../../docs/openapi.yaml).
The generator currently emits a compatibility warning for OpenAPI 3.1, but
the checked-in client, contract tests, and Redocly validation are authoritative.

Regenerate and verify it from the repository root:

```sh
make generate-sdk
make verify-sdk
```

Example:

```go
client, err := veilclient.NewClientWithResponses(
    "https://vpn.example.com/hidden/",
    veilclient.WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
        req.Header.Set("X-Veil-Token", os.Getenv("VEIL_API_TOKEN"))
        return nil
    }),
)
if err != nil {
    log.Fatal(err)
}

status, err := client.GetApiStatusWithResponse(context.Background())
```

Browser-style cookie sessions must also send the current `X-CSRF-Token` on
mutating requests. Viewer sessions are read-only.
