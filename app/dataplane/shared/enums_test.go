package shared

import (
	"reflect"
	"testing"
)

func TestDataplaneEnumValuesMatchPython(t *testing.T) {
	if ModeAuto != 0 || ModeFast != 1 || ModeExpert != 2 || ModeHeavy != 3 ||
		ModeGrok43 != 4 || ModeConsole != 5 {
		t.Fatalf("mode ids changed")
	}
	if PoolBasic != 0 || PoolSuper != 1 || PoolHeavy != 2 {
		t.Fatalf("pool ids changed")
	}
	if StatusActive != 0 || StatusCooling != 1 || StatusExpired != 2 ||
		StatusDisabled != 3 || StatusDeleted != 4 {
		t.Fatalf("status ids changed")
	}
}

func TestDataplaneEnumMapsMatchPython(t *testing.T) {
	if !reflect.DeepEqual(PoolStringToID, map[string]int{"basic": 0, "super": 1, "heavy": 2}) {
		t.Fatalf("PoolStringToID = %#v", PoolStringToID)
	}
	if !reflect.DeepEqual(PoolIDToString, map[int]string{0: "basic", 1: "super", 2: "heavy"}) {
		t.Fatalf("PoolIDToString = %#v", PoolIDToString)
	}
	if !reflect.DeepEqual(StatusStringToID, map[string]int{"active": 0, "cooling": 1, "expired": 2, "disabled": 3}) {
		t.Fatalf("StatusStringToID = %#v", StatusStringToID)
	}
	if !reflect.DeepEqual(AllModeIDs, []int{0, 1, 2, 3, 4, 5}) {
		t.Fatalf("AllModeIDs = %#v", AllModeIDs)
	}
}
