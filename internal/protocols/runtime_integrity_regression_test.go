package protocols

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/runtimeinstall"
)

func TestEveryRuntimeDescriptorIsPinnedAndDeclaresIntegrityAndVersionProbe(t *testing.T) {
	descriptors := append([]runtimeinstall.Runtime(nil), runtimeinstall.Catalog("amd64")...)
	for _, plugin := range NewRegistry().All() {
		provider, ok := AsRuntimeProvider(plugin)
		if !ok {
			continue
		}
		descriptors = append(descriptors, provider.RuntimeInstall("amd64"))
	}
	if len(descriptors) == 0 {
		t.Fatal("runtime descriptor catalog is empty")
	}
	for _, descriptor := range descriptors {
		t.Run(descriptor.Name, func(t *testing.T) {
			value := reflect.ValueOf(descriptor)
			for _, field := range []string{"Version", "Integrity", "VersionCommand", "VersionPattern"} {
				member := value.FieldByName(field)
				if !member.IsValid() {
					t.Errorf("runtime descriptor has no mandatory %s field", field)
					continue
				}
				switch member.Kind() {
				case reflect.String:
					if strings.TrimSpace(member.String()) == "" || strings.Contains(strings.ToLower(member.String()), "latest") {
						t.Errorf("runtime %s has unpinned/empty %s=%q", descriptor.Name, field, member.String())
					}
				case reflect.Slice:
					if member.Len() == 0 {
						t.Errorf("runtime %s has empty %s", descriptor.Name, field)
					}
				}
			}
			if strings.Contains(strings.ToLower(descriptor.SourcePackage), "@latest") {
				t.Errorf("runtime %s source package is mutable: %s", descriptor.Name, descriptor.SourcePackage)
			}
			if (descriptor.Method == runtimeinstall.MethodArchive || descriptor.Method == runtimeinstall.MethodRawBinary) &&
				descriptor.Integrity == "upstream-checksum" && descriptor.ChecksumMatch == nil {
				t.Errorf("release runtime %s has no mandatory checksum selector", descriptor.Name)
			}
			if descriptor.Integrity == "pinned-sha256" && strings.TrimSpace(descriptor.PinnedSHA256) == "" {
				t.Errorf("release runtime %s has no pinned archive digest", descriptor.Name)
			}
		})
	}
}
