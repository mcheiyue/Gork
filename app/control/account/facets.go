package account

type AccountFacetSnapshot struct {
	Status map[string]int
	NSFW   map[string]int
	Pools  map[string]int
	Stats  map[string]int
}

func NewAccountFacetSnapshot() AccountFacetSnapshot {
	return AccountFacetSnapshot{
		Status: map[string]int{"all": 0, "active": 0, "cooling": 0, "invalid": 0, "disabled": 0},
		NSFW:   map[string]int{"all": 0, "enabled": 0, "disabled": 0},
		Pools:  map[string]int{"all": 0, "basic": 0, "super": 0, "heavy": 0},
		Stats:  map[string]int{"active": 0, "cooling": 0, "invalid": 0, "disabled": 0, "calls": 0, "success": 0, "fail": 0, "qa": 0, "qf": 0, "qe": 0, "qh": 0, "qc": 0},
	}
}
