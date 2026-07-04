package panel

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/service"
)

func TestSliceCatalogSliceMissing(t *testing.T) {
	catalog := NewSliceCatalog([]service.ManagedRuntime{})
	slice, ok := catalog.Slice("nonexistent")
	if ok {
		t.Fatalf("expected Slice(\"nonexistent\") to return ok=false, got true")
	}
	if slice.Name != "" || len(slice.RenderSlots) != 0 || len(slice.EventBindings) != 0 {
		t.Fatalf("expected empty Slice for missing name, got %+v", slice)
	}
}
