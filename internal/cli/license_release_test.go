//go:build !licensetest

package cli

import appstate "github.com/nocaptain-oss/archie-link/internal/state"

import "testing"

func TestReleaseBuildDoesNotEnableTestLicense(t *testing.T) {
	t.Setenv("ARCHIE_LINK_TEST_LICENSE", "1")
	provider, err := lifecycleEntitlement("install-1", appstate.Store{})
	if err != nil {
		t.Fatal(err)
	}
	if provider != nil {
		t.Fatal("release build must not enable the test entitlement provider")
	}
}

func TestReleaseBuildDisablesRuntimePublicKeyOverride(t *testing.T) {
	t.Setenv("ARCHIE_LINK_PUBLIC_KEY", "not-a-release-trust-root")
	if value, ok := runtimePublicKeyOverride(); ok || value != "" {
		t.Fatalf("runtime public key override enabled in release build: %q", value)
	}
}
