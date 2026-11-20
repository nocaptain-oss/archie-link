package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"

	"github.com/nocaptain-oss/archie-link/internal/adapter"
	"github.com/nocaptain-oss/archie-link/internal/coexist"
)

func runCoexist(args []string, stdout, stderr io.Writer, deps Dependencies) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: archielink coexist <inspect|gate|baseline> [--json]")
		return 2
	}
	subcommand := args[0]
	jsonOutput := false
	profilePath := ""
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--json":
			jsonOutput = true
		case "--profile":
			i++
			if i >= len(args) {
				fmt.Fprintln(stderr, "--profile requires a path")
				return 2
			}
			profilePath = args[i]
		default:
			fmt.Fprintf(stderr, "unknown coexist option: %s\n", args[i])
			return 2
		}
	}

	switch subcommand {
	case "inspect":
		profile, code := providerProfile(stdout, stderr, deps, jsonOutput)
		if code != 0 {
			return code
		}
		if jsonOutput {
			return encodeResult(stdout, stderr, profile)
		}
		fmt.Fprintf(stdout, "os=%s routes=%d dns=%d proxies=%d incomplete=%d\n",
			profile.OS, len(profile.Routes), len(profile.DNSResolvers), len(profile.Proxies), len(profile.IncompleteLayers))
		return 0
	case "gate":
		profile, code := providerProfile(stdout, stderr, deps, jsonOutput)
		if code != 0 {
			return code
		}
		decision := coexist.Evaluate(profile)
		if jsonOutput {
			return encodeResult(stdout, stderr, decision)
		}
		fmt.Fprintf(stdout, "mode=%s allowed=%t findings=%d\n", decision.Mode, decision.Allowed, len(decision.Findings))
		return 0
	case "baseline":
		if profilePath == "" {
			fmt.Fprintln(stderr, "coexist baseline requires --profile")
			return 2
		}
		endpoints, err := coexist.LoadEndpoints(profilePath)
		if err != nil {
			return writeCoexistError(stdout, stderr, jsonOutput, "invalid_profile", err, 2)
		}
		checker := deps.BaselineChecker
		if checker == nil {
			checker = coexist.DefaultHTTPChecker{}
		}
		results := coexist.RunBaseline(context.Background(), endpoints, checker)
		if jsonOutput {
			return encodeResult(stdout, stderr, results)
		}
		for _, result := range results {
			fmt.Fprintf(stdout, "%s ok=%t direct=%d upstream=%d\n", result.ID, result.OK, result.Direct, result.Upstream)
		}
		return 0
	default:
		fmt.Fprintf(stderr, "unknown coexist command: %s\n", subcommand)
		return 2
	}
}

func providerProfile(stdout, stderr io.Writer, deps Dependencies, jsonOutput bool) (coexist.Profile, int) {
	if deps.Registry == nil {
		return coexist.Profile{}, writeCoexistError(stdout, stderr, jsonOutput, "preflight_failed", errors.New("adapter registry is not configured"), 5)
	}
	goos := deps.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	platformAdapter, err := deps.Registry.For(goos)
	if err != nil {
		code := 5
		errorCode := "preflight_failed"
		if errors.Is(err, adapter.ErrUnsupportedPlatform) {
			code = 3
			errorCode = "unsupported_platform"
		}
		return coexist.Profile{}, writeCoexistError(stdout, stderr, jsonOutput, errorCode, err, code)
	}
	provider, ok := platformAdapter.(coexist.Provider)
	if !ok {
		return coexist.Profile{}, writeCoexistError(stdout, stderr, jsonOutput, "unsupported_platform",
			errors.New("platform does not implement coexistence discovery"), 3)
	}
	profile, err := provider.Coexistence(context.Background())
	if err != nil {
		return coexist.Profile{}, writeCoexistError(stdout, stderr, jsonOutput, "preflight_failed", err, 5)
	}
	return profile, 0
}

func writeCoexistError(stdout, stderr io.Writer, jsonOutput bool, code string, err error, exitCode int) int {
	if jsonOutput {
		_ = writeJSON(stdout, map[string]any{
			"schema_version": "1",
			"error": map[string]string{
				"code":    code,
				"message": err.Error(),
			},
		})
	}
	fmt.Fprintf(stderr, "%s: %s\n", code, strings.TrimSpace(err.Error()))
	return exitCode
}
