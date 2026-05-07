package api

import "testing"

func TestUniqueStringListAppendsOnlyMissingValues(t *testing.T) {
	list := NewUniqueStringList([]string{"a", "b"})
	list = list.Append("b").Append("c")
	values := list.Values()
	if len(values) != 3 || values[0] != "a" || values[1] != "b" || values[2] != "c" {
		t.Fatalf("values = %+v", values)
	}
	values[0] = "mutated"
	if list.Values()[0] != "a" {
		t.Fatal("Values should return a copy")
	}
}
