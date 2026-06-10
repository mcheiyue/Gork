package account

type AccountUpsert struct {
	Token string         `json:"token"`
	Pool  string         `json:"pool"`
	Tags  []string       `json:"tags"`
	Ext   map[string]any `json:"ext"`
}

type AccountPatch struct {
	Token          string         `json:"token"`
	Pool           *string        `json:"pool,omitempty"`
	Status         *AccountStatus `json:"status,omitempty"`
	Tags           []string       `json:"tags,omitempty"`
	AddTags        []string       `json:"add_tags,omitempty"`
	RemoveTags     []string       `json:"remove_tags,omitempty"`
	QuotaAuto      map[string]any `json:"quota_auto,omitempty"`
	QuotaFast      map[string]any `json:"quota_fast,omitempty"`
	QuotaExpert    map[string]any `json:"quota_expert,omitempty"`
	QuotaHeavy     map[string]any `json:"quota_heavy,omitempty"`
	QuotaGrok43    map[string]any `json:"quota_grok_4_3,omitempty"`
	QuotaConsole   map[string]any `json:"quota_console,omitempty"`
	UsageUseDelta  *int           `json:"usage_use_delta,omitempty"`
	UsageFailDelta *int           `json:"usage_fail_delta,omitempty"`
	UsageSyncDelta *int           `json:"usage_sync_delta,omitempty"`
	LastUseAt      *int64         `json:"last_use_at,omitempty"`
	LastFailAt     *int64         `json:"last_fail_at,omitempty"`
	LastFailReason *string        `json:"last_fail_reason,omitempty"`
	LastSyncAt     *int64         `json:"last_sync_at,omitempty"`
	LastClearAt    *int64         `json:"last_clear_at,omitempty"`
	StateReason    *string        `json:"state_reason,omitempty"`
	ExtMerge       map[string]any `json:"ext_merge,omitempty"`
	ClearFailures  bool           `json:"clear_failures"`
}

type ListAccountsQuery struct {
	Page           int            `json:"page"`
	PageSize       int            `json:"page_size"`
	Pool           *string        `json:"pool,omitempty"`
	Status         *AccountStatus `json:"status,omitempty"`
	Tags           []string       `json:"tags"`
	ExcludeTags    []string       `json:"exclude_tags"`
	IncludeDeleted bool           `json:"include_deleted"`
	SortBy         string         `json:"sort_by"`
	SortDesc       bool           `json:"sort_desc"`
}

type BulkReplacePoolCommand struct {
	Pool    string          `json:"pool"`
	Upserts []AccountUpsert `json:"upserts"`
}

func NewAccountUpsert(token string) AccountUpsert {
	upsert := AccountUpsert{Token: token}
	upsert.Normalize()
	return upsert
}

func (u *AccountUpsert) Normalize() {
	if u.Pool == "" {
		u.Pool = "basic"
	}
	if u.Tags == nil {
		u.Tags = []string{}
	}
	if u.Ext == nil {
		u.Ext = map[string]any{}
	}
}

func DefaultListAccountsQuery() ListAccountsQuery {
	query := ListAccountsQuery{Page: 1, PageSize: 50, SortBy: "updated_at", SortDesc: true}
	query.Normalize()
	return query
}

func (q *ListAccountsQuery) Normalize() {
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize < 1 {
		q.PageSize = 50
	}
	if q.PageSize > 2000 {
		q.PageSize = 2000
	}
	if q.Tags == nil {
		q.Tags = []string{}
	}
	if q.ExcludeTags == nil {
		q.ExcludeTags = []string{}
	}
	if q.SortBy == "" {
		q.SortBy = "updated_at"
	}
}

func (c *BulkReplacePoolCommand) Normalize() {
	if c.Upserts == nil {
		c.Upserts = []AccountUpsert{}
	}
	for i := range c.Upserts {
		c.Upserts[i].Normalize()
	}
}
