//go:build licensetest

package cli

import appstate "github.com/nocaptain-oss/archie-link/internal/state"

import "testing"

func TestLicenseTestBuildEnablesScopedTestProvider(t *testing.T) {
	t.Setenv("ARCHIE_LINK_TEST_LICENSE", "1")
	provider, err := lifecycleEntitlement("install-1", appstate.Store{})
	if err != nil {
		t.Fatal(err)
	}
	if provider == nil {
		t.Fatal("licensetest build should provide a test entitlement")
	}
}
