package app

import "testing"

func TestEmptyStatusDefinitionsPreserveActiveDefinitions(t *testing.T) {
	before := UnitStatuses()
	for _, empty := range [][]UnitStatusOption{nil, {}} {
		if err := setUnitStatuses(empty); err == nil {
			t.Fatal("empty definitions were accepted")
		}
		if len(UnitStatuses()) != len(before) || UnitStatuses()[0] != before[0] {
			t.Fatal("invalid definitions replaced active statuses")
		}
	}
}
