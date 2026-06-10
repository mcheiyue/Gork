package account

import "testing"

func TestNewLeaseMatchesPythonDataclassFields(t *testing.T) {
	lease := NewLease(7, "sso-token", 2, 5, 123456)
	if lease.LeaseID <= 0 {
		t.Fatalf("LeaseID = %d, want positive monotonic id", lease.LeaseID)
	}
	if lease.Idx != 7 || lease.Token != "sso-token" || lease.PoolID != 2 ||
		lease.ModeID != 5 || lease.SelectedAt != 123456 {
		t.Fatalf("lease fields mismatch: %#v", lease)
	}
}

func TestNewLeaseUsesMonotonicIDs(t *testing.T) {
	first := NewLease(1, "one", 0, 1, 10)
	second := NewLease(2, "two", 1, -1, 20)
	if second.LeaseID != first.LeaseID+1 {
		t.Fatalf("lease ids = %d then %d, want consecutive ids", first.LeaseID, second.LeaseID)
	}
}
