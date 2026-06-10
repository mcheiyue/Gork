package account

const (
	StatusActive   = 0
	StatusCooling  = 1
	StatusExpired  = 2
	StatusDisabled = 3
	StatusDeleted  = 4
)

var allModeIDs = []int{0, 1, 2, 3, 4, 5}

type ModeKey struct {
	PoolID int
	ModeID int
}

type AccountSlot struct {
	Token         string
	PoolID        int
	StatusID      int
	QuotaAuto     int
	QuotaFast     int
	QuotaExpert   int
	QuotaHeavy    int
	QuotaGrok43   int
	QuotaConsole  int
	TotalAuto     int
	TotalFast     int
	TotalExpert   int
	TotalHeavy    int
	TotalGrok43   int
	TotalConsole  int
	WindowAuto    int
	WindowFast    int
	WindowExpert  int
	WindowHeavy   int
	WindowGrok43  int
	WindowConsole int
	ResetAuto     int
	ResetFast     int
	ResetExpert   int
	ResetHeavy    int
	ResetGrok43   int
	ResetConsole  int
	Health        float64
	LastUseS      int
	LastFailS     int
	FailCount     int
	Tags          []string
}

type AccountRuntimeTable struct {
	TokenByIdx []string
	IdxByToken map[string]int

	PoolByIdx   []int
	StatusByIdx []int

	QuotaAutoByIdx    []int
	QuotaFastByIdx    []int
	QuotaExpertByIdx  []int
	QuotaHeavyByIdx   []int
	QuotaGrok43ByIdx  []int
	QuotaConsoleByIdx []int

	TotalAutoByIdx    []int
	TotalFastByIdx    []int
	TotalExpertByIdx  []int
	TotalHeavyByIdx   []int
	TotalGrok43ByIdx  []int
	TotalConsoleByIdx []int

	WindowAutoByIdx    []int
	WindowFastByIdx    []int
	WindowExpertByIdx  []int
	WindowHeavyByIdx   []int
	WindowGrok43ByIdx  []int
	WindowConsoleByIdx []int

	ResetAutoAtByIdx    []int
	ResetFastAtByIdx    []int
	ResetExpertAtByIdx  []int
	ResetHeavyAtByIdx   []int
	ResetGrok43AtByIdx  []int
	ResetConsoleAtByIdx []int

	InflightByIdx      []int
	FailCountByIdx     []int
	HealthByIdx        []float64
	LastUseAtByIdx     []int
	LastFailAtByIdx    []int
	CoolingUntilSByIdx []int

	ModeAvailable map[ModeKey]map[int]bool
	TagIdx        map[string]map[int]bool

	Revision int
	Size     int
}

func MakeEmptyTable() *AccountRuntimeTable {
	return &AccountRuntimeTable{
		TokenByIdx:    []string{},
		IdxByToken:    map[string]int{},
		ModeAvailable: map[ModeKey]map[int]bool{},
		TagIdx:        map[string]map[int]bool{},
	}
}

func (t *AccountRuntimeTable) quotaCol(modeID int) []int {
	switch modeID {
	case 0:
		return t.QuotaAutoByIdx
	case 1:
		return t.QuotaFastByIdx
	case 2:
		return t.QuotaExpertByIdx
	case 3:
		return t.QuotaHeavyByIdx
	case 4:
		return t.QuotaGrok43ByIdx
	default:
		return t.QuotaConsoleByIdx
	}
}

func (t *AccountRuntimeTable) windowCol(modeID int) []int {
	switch modeID {
	case 0:
		return t.WindowAutoByIdx
	case 1:
		return t.WindowFastByIdx
	case 2:
		return t.WindowExpertByIdx
	case 3:
		return t.WindowHeavyByIdx
	case 4:
		return t.WindowGrok43ByIdx
	default:
		return t.WindowConsoleByIdx
	}
}

func (t *AccountRuntimeTable) totalCol(modeID int) []int {
	switch modeID {
	case 0:
		return t.TotalAutoByIdx
	case 1:
		return t.TotalFastByIdx
	case 2:
		return t.TotalExpertByIdx
	case 3:
		return t.TotalHeavyByIdx
	case 4:
		return t.TotalGrok43ByIdx
	default:
		return t.TotalConsoleByIdx
	}
}

func (t *AccountRuntimeTable) resetCol(modeID int) []int {
	switch modeID {
	case 0:
		return t.ResetAutoAtByIdx
	case 1:
		return t.ResetFastAtByIdx
	case 2:
		return t.ResetExpertAtByIdx
	case 3:
		return t.ResetHeavyAtByIdx
	case 4:
		return t.ResetGrok43AtByIdx
	default:
		return t.ResetConsoleAtByIdx
	}
}

func (t *AccountRuntimeTable) addToIndexes(idx int) {
	poolID := t.PoolByIdx[idx]
	statusID := t.StatusByIdx[idx]
	if statusID != StatusActive {
		return
	}
	for _, modeID := range allModeIDs {
		if t.windowCol(modeID)[idx] > 0 {
			key := ModeKey{PoolID: poolID, ModeID: modeID}
			if t.ModeAvailable[key] == nil {
				t.ModeAvailable[key] = map[int]bool{}
			}
			t.ModeAvailable[key][idx] = true
		}
	}
}

func (t *AccountRuntimeTable) removeFromIndexes(idx int) {
	poolID := t.PoolByIdx[idx]
	for _, modeID := range allModeIDs {
		key := ModeKey{PoolID: poolID, ModeID: modeID}
		if bucket := t.ModeAvailable[key]; bucket != nil {
			delete(bucket, idx)
		}
	}
}

func (t *AccountRuntimeTable) removeFromTagIdx(idx int, tags []string) {
	for _, tag := range tags {
		if bucket := t.TagIdx[tag]; bucket != nil {
			delete(bucket, idx)
		}
	}
}

func (t *AccountRuntimeTable) addToTagIdx(idx int, tags []string) {
	for _, tag := range tags {
		if t.TagIdx[tag] == nil {
			t.TagIdx[tag] = map[int]bool{}
		}
		t.TagIdx[tag][idx] = true
	}
}

func (t *AccountRuntimeTable) AppendSlot(slot AccountSlot) int {
	idx := len(t.TokenByIdx)
	t.TokenByIdx = append(t.TokenByIdx, slot.Token)
	t.IdxByToken[slot.Token] = idx
	t.PoolByIdx = append(t.PoolByIdx, slot.PoolID)
	t.StatusByIdx = append(t.StatusByIdx, slot.StatusID)
	t.QuotaAutoByIdx = append(t.QuotaAutoByIdx, clampQuota(slot.QuotaAuto))
	t.QuotaFastByIdx = append(t.QuotaFastByIdx, clampQuota(slot.QuotaFast))
	t.QuotaExpertByIdx = append(t.QuotaExpertByIdx, clampQuota(slot.QuotaExpert))
	t.QuotaHeavyByIdx = append(t.QuotaHeavyByIdx, clampQuota(slot.QuotaHeavy))
	t.QuotaGrok43ByIdx = append(t.QuotaGrok43ByIdx, clampQuota(slot.QuotaGrok43))
	t.QuotaConsoleByIdx = append(t.QuotaConsoleByIdx, clampQuota(slot.QuotaConsole))
	t.TotalAutoByIdx = append(t.TotalAutoByIdx, clampTotal(slot.TotalAuto))
	t.TotalFastByIdx = append(t.TotalFastByIdx, clampTotal(slot.TotalFast))
	t.TotalExpertByIdx = append(t.TotalExpertByIdx, clampTotal(slot.TotalExpert))
	t.TotalHeavyByIdx = append(t.TotalHeavyByIdx, clampTotal(slot.TotalHeavy))
	t.TotalGrok43ByIdx = append(t.TotalGrok43ByIdx, clampTotal(slot.TotalGrok43))
	t.TotalConsoleByIdx = append(t.TotalConsoleByIdx, clampTotal(slot.TotalConsole))
	t.WindowAutoByIdx = append(t.WindowAutoByIdx, clampNonNegative(slot.WindowAuto))
	t.WindowFastByIdx = append(t.WindowFastByIdx, clampNonNegative(slot.WindowFast))
	t.WindowExpertByIdx = append(t.WindowExpertByIdx, clampNonNegative(slot.WindowExpert))
	t.WindowHeavyByIdx = append(t.WindowHeavyByIdx, clampNonNegative(slot.WindowHeavy))
	t.WindowGrok43ByIdx = append(t.WindowGrok43ByIdx, clampNonNegative(slot.WindowGrok43))
	t.WindowConsoleByIdx = append(t.WindowConsoleByIdx, clampNonNegative(slot.WindowConsole))
	t.ResetAutoAtByIdx = append(t.ResetAutoAtByIdx, slot.ResetAuto)
	t.ResetFastAtByIdx = append(t.ResetFastAtByIdx, slot.ResetFast)
	t.ResetExpertAtByIdx = append(t.ResetExpertAtByIdx, slot.ResetExpert)
	t.ResetHeavyAtByIdx = append(t.ResetHeavyAtByIdx, slot.ResetHeavy)
	t.ResetGrok43AtByIdx = append(t.ResetGrok43AtByIdx, slot.ResetGrok43)
	t.ResetConsoleAtByIdx = append(t.ResetConsoleAtByIdx, slot.ResetConsole)
	t.InflightByIdx = append(t.InflightByIdx, 0)
	t.FailCountByIdx = append(t.FailCountByIdx, clampFailCount(slot.FailCount))
	t.HealthByIdx = append(t.HealthByIdx, slot.Health)
	t.LastUseAtByIdx = append(t.LastUseAtByIdx, slot.LastUseS)
	t.LastFailAtByIdx = append(t.LastFailAtByIdx, slot.LastFailS)
	t.CoolingUntilSByIdx = append(t.CoolingUntilSByIdx, 0)
	t.Size++
	t.addToIndexes(idx)
	t.addToTagIdx(idx, slot.Tags)
	return idx
}

func (t *AccountRuntimeTable) UpdateSlot(idx int, slot AccountSlot, oldTags []string) {
	t.removeFromIndexes(idx)
	t.removeFromTagIdx(idx, oldTags)
	t.PoolByIdx[idx] = slot.PoolID
	t.StatusByIdx[idx] = slot.StatusID
	t.QuotaAutoByIdx[idx] = clampQuota(slot.QuotaAuto)
	t.QuotaFastByIdx[idx] = clampQuota(slot.QuotaFast)
	t.QuotaExpertByIdx[idx] = clampQuota(slot.QuotaExpert)
	t.QuotaHeavyByIdx[idx] = clampQuota(slot.QuotaHeavy)
	t.QuotaGrok43ByIdx[idx] = clampQuota(slot.QuotaGrok43)
	t.QuotaConsoleByIdx[idx] = clampQuota(slot.QuotaConsole)
	t.TotalAutoByIdx[idx] = clampTotal(slot.TotalAuto)
	t.TotalFastByIdx[idx] = clampTotal(slot.TotalFast)
	t.TotalExpertByIdx[idx] = clampTotal(slot.TotalExpert)
	t.TotalHeavyByIdx[idx] = clampTotal(slot.TotalHeavy)
	t.TotalGrok43ByIdx[idx] = clampTotal(slot.TotalGrok43)
	t.TotalConsoleByIdx[idx] = clampTotal(slot.TotalConsole)
	t.WindowAutoByIdx[idx] = clampNonNegative(slot.WindowAuto)
	t.WindowFastByIdx[idx] = clampNonNegative(slot.WindowFast)
	t.WindowExpertByIdx[idx] = clampNonNegative(slot.WindowExpert)
	t.WindowHeavyByIdx[idx] = clampNonNegative(slot.WindowHeavy)
	t.WindowGrok43ByIdx[idx] = clampNonNegative(slot.WindowGrok43)
	t.WindowConsoleByIdx[idx] = clampNonNegative(slot.WindowConsole)
	t.ResetAutoAtByIdx[idx] = slot.ResetAuto
	t.ResetFastAtByIdx[idx] = slot.ResetFast
	t.ResetExpertAtByIdx[idx] = slot.ResetExpert
	t.ResetHeavyAtByIdx[idx] = slot.ResetHeavy
	t.ResetGrok43AtByIdx[idx] = slot.ResetGrok43
	t.ResetConsoleAtByIdx[idx] = slot.ResetConsole
	t.FailCountByIdx[idx] = clampFailCount(slot.FailCount)
	t.LastUseAtByIdx[idx] = slot.LastUseS
	t.LastFailAtByIdx[idx] = slot.LastFailS
	t.addToIndexes(idx)
	t.addToTagIdx(idx, slot.Tags)
}

func (t *AccountRuntimeTable) GetToken(idx int) string {
	return t.TokenByIdx[idx]
}

func (t *AccountRuntimeTable) GetPoolID(idx int) int {
	return t.PoolByIdx[idx]
}

func (t *AccountRuntimeTable) QuotaFor(idx int, modeID int) int {
	return t.quotaCol(modeID)[idx]
}

func (t *AccountRuntimeTable) TotalFor(idx int, modeID int) int {
	return t.totalCol(modeID)[idx]
}

func (t *AccountRuntimeTable) WindowFor(idx int, modeID int) int {
	return t.windowCol(modeID)[idx]
}

func (t *AccountRuntimeTable) ResetFor(idx int, modeID int) int {
	return t.resetCol(modeID)[idx]
}

func (t *AccountRuntimeTable) IsActive(idx int) bool {
	return t.StatusByIdx[idx] == StatusActive
}

func (t *AccountRuntimeTable) IterLiveIndices() []int {
	out := []int{}
	for idx := range t.TokenByIdx {
		if t.StatusByIdx[idx] != StatusDeleted {
			out = append(out, idx)
		}
	}
	return out
}

func clampQuota(value int) int {
	if value < -1 {
		return -1
	}
	if value > 32767 {
		return 32767
	}
	return value
}

func clampTotal(value int) int {
	if value < 0 {
		return 0
	}
	if value > 32767 {
		return 32767
	}
	return value
}

func clampNonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func clampFailCount(value int) int {
	if value < 0 {
		return 0
	}
	if value > 65535 {
		return 65535
	}
	return value
}
