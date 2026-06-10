package products

import (
	"context"

	controlaccount "github.com/jiujiu532/grok2api/app/control/account"
	"github.com/jiujiu532/grok2api/app/control/model"
	dataaccount "github.com/jiujiu532/grok2api/app/dataplane/account"
	platformconfig "github.com/jiujiu532/grok2api/app/platform/config"
)

const randomMaxRetries = 5

// ReserveAccountQuery mirrors the Python directory.reserve(...) parameters.
type ReserveAccountQuery struct {
	PoolCandidates []int
	ModeID         model.ModeID
	NowSOverride   *int64
	ExcludeTokens  []string
}

type ReserveAccountOptions struct {
	ExcludeTokens []string
	NowSOverride  *int64
}

type AccountDirectory interface {
	Reserve(context.Context, ReserveAccountQuery) (any, error)
}

type AccountRefreshService interface {
	RefreshOnDemand(context.Context) error
}

type refreshOnDemandRuntimeService interface {
	RefreshOnDemand(context.Context) (controlaccount.RefreshResult, error)
}

var (
	accountSelectionStrategy = defaultAccountSelectionStrategy
	accountSelectionGetInt   = defaultAccountSelectionGetInt
	accountSelectionGetBool  = defaultAccountSelectionGetBool
	accountRefreshService    = defaultAccountRefreshService
)

type controlRefreshServiceAdapter struct {
	service refreshOnDemandRuntimeService
}

func (a controlRefreshServiceAdapter) RefreshOnDemand(ctx context.Context) error {
	_, err := a.service.RefreshOnDemand(ctx)
	return err
}

func defaultAccountSelectionStrategy() string {
	return dataaccount.CurrentStrategy()
}

func defaultAccountSelectionGetInt(key string, defaultValue int) int {
	return platformconfig.GlobalConfig.GetInt(key, defaultValue)
}

func defaultAccountSelectionGetBool(key string, defaultValue bool) bool {
	return platformconfig.GlobalConfig.GetBool(key, defaultValue)
}

func defaultAccountRefreshService() AccountRefreshService {
	service := controlaccount.GetRefreshService()
	if service == nil {
		return nil
	}
	onDemand, ok := any(service).(refreshOnDemandRuntimeService)
	if !ok {
		return nil
	}
	return controlRefreshServiceAdapter{service: onDemand}
}

func SelectionMaxRetries() int {
	if accountSelectionStrategy() == "random" {
		return randomMaxRetries
	}
	return accountSelectionGetInt("retry.max_retries", 1)
}

func ModeCandidates(spec model.ModelSpec) []model.ModeID {
	primary := spec.ModeID
	if spec.IsChat() && spec.ModeID == model.ModeAuto && accountSelectionGetBool("features.auto_chat_mode_fallback", true) {
		return []model.ModeID{primary, model.ModeFast, model.ModeExpert}
	}
	return []model.ModeID{primary}
}

func ReserveAccount(ctx context.Context, directory AccountDirectory, spec model.ModelSpec, options ReserveAccountOptions) (any, model.ModeID, bool, error) {
	lease, selectedMode, ok, err := reserveFromCandidates(ctx, directory, spec, options)
	if err != nil || ok {
		return lease, selectedMode, ok, err
	}

	if accountSelectionStrategy() == "random" {
		return nil, spec.ModeID, false, nil
	}

	refresh := accountRefreshService()
	if refresh == nil {
		return nil, spec.ModeID, false, nil
	}
	if err := refresh.RefreshOnDemand(ctx); err != nil {
		return nil, spec.ModeID, false, err
	}

	return reserveFromCandidates(ctx, directory, spec, options)
}

func reserveFromCandidates(ctx context.Context, directory AccountDirectory, spec model.ModelSpec, options ReserveAccountOptions) (any, model.ModeID, bool, error) {
	for _, candidate := range ModeCandidates(spec) {
		lease, err := directory.Reserve(ctx, ReserveAccountQuery{
			PoolCandidates: spec.PoolCandidates(),
			ModeID:         candidate,
			NowSOverride:   options.NowSOverride,
			ExcludeTokens:  options.ExcludeTokens,
		})
		if err != nil {
			return nil, spec.ModeID, false, err
		}
		if lease != nil {
			return lease, candidate, true, nil
		}
	}
	return nil, spec.ModeID, false, nil
}
