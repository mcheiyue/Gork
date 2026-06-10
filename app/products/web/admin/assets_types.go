package admin

type adminAssetsListQuery struct {
	Page        int
	PageSize    int
	Pool        string
	Status      string
	Tags        []string
	ExcludeTags []string
	SortBy      string
	SortDesc    bool
}

type adminAssetsListResult struct {
	Items      []adminAssetsAccount
	Total      int
	Page       int
	PageSize   int
	TotalPages int
	Revision   int64
}

type adminAssetsAccount struct {
	Token          string
	Pool           string
	Status         string
	Tags           []string
	Quota          map[string]any
	UsageUseCount  int
	UsageFailCount int
	LastUseAt      int64
	LastFailAt     int64
	LastFailReason string
	LastSyncAt     int64
	LastClearAt    int64
	StateReason    string
	Ext            map[string]any
	Deleted        bool
	DeletedAt      any
}
