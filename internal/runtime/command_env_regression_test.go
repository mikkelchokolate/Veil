package runtime

import (
	"reflect"
	"testing"
)

func TestMergeCommandEnvReplacesInsteadOfDuplicatingOverrides(t *testing.T) {
	got := mergeCommandEnv([]string{"PATH=/bin", "LC_ALL=ru_RU.UTF-8", "LANG=ru_RU.UTF-8"}, []string{"LC_ALL=C", "LANG=C"})
	want := []string{"PATH=/bin", "LC_ALL=C", "LANG=C"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
}
