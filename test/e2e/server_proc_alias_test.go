//go:build e2e

package e2e

// testServer keeps protocol helper signatures concise while using the
// real subprocess-backed E2E harness.
type testServer = serverProc
