package runtimeinstall

import "context"

func fixedRuntimeVersion(output string) func(context.Context, string, []string) (string, error) {
	return func(context.Context, string, []string) (string, error) {
		return output, nil
	}
}
