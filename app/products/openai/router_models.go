package openai

import (
	"net/http"
	"strings"
	"time"

	"github.com/dslzl/gork/app/control/model"
)

var poolIDToName = map[int]string{
	0: "basic",
	1: "super",
	2: "heavy",
}

func handleListModels(w http.ResponseWriter, r *http.Request) {
	pools := routerAvailablePools(r)
	accountPools := routerAccountPools(r.Context())
	created := time.Now().Unix()
	data := make([]map[string]any, 0)
	for _, spec := range model.ListEnabled() {
		if !modelAvailableForPools(spec, pools) {
			continue
		}
		if accountPools != nil && !modelHasAccountPool(spec, accountPools) {
			continue
		}
		data = append(data, routerModelBody(spec, created))
	}
	// Build 动态模型：仅 features.build_provider=true 且有 active 账号时追加
	for _, spec := range listBuildModelSpecs(r.Context()) {
		data = append(data, routerModelBody(spec, created))
	}
	writeRouterJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   data,
	})
}

func handleGetModel(w http.ResponseWriter, r *http.Request) {
	modelID := strings.TrimPrefix(r.URL.Path, "/v1/models/")
	// Build 前缀走 Resolve（静态 registry 无 build/*）
	if model.IsBuildModelName(modelID) {
		if !buildFeatureEnabled() {
			writeRouterJSON(w, http.StatusNotFound, map[string]any{
				"error": map[string]any{
					"message": "Model '" + modelID + "' not found",
					"type":    "invalid_request_error",
				},
			})
			return
		}
		spec, err := model.Resolve(modelID)
		if err != nil {
			writeRouterJSON(w, http.StatusNotFound, map[string]any{
				"error": map[string]any{
					"message": "Model '" + modelID + "' not found",
					"type":    "invalid_request_error",
				},
			})
			return
		}
		writeRouterJSON(w, http.StatusOK, routerModelBody(spec, time.Now().Unix()))
		return
	}
	spec, ok := model.Get(modelID)
	if !ok || !modelAvailableForPools(spec, routerAvailablePools(r)) {
		writeRouterJSON(w, http.StatusNotFound, map[string]any{
			"error": map[string]any{
				"message": "Model '" + modelID + "' not found",
				"type":    "invalid_request_error",
			},
		})
		return
	}
	writeRouterJSON(w, http.StatusOK, routerModelBody(spec, time.Now().Unix()))
}

func routerModelBody(spec model.ModelSpec, created int64) map[string]any {
	return map[string]any{
		"id":       spec.ModelName,
		"object":   "model",
		"created":  created,
		"owned_by": "xai",
		"name":     spec.PublicName,
	}
}

func modelAvailableForPools(spec model.ModelSpec, pools map[string]struct{}) bool {
	if !spec.Enabled {
		return false
	}
	if len(pools) == 0 {
		return true
	}
	for _, poolID := range spec.PoolCandidates() {
		pool := poolIDToName[poolID]
		if _, ok := pools[pool]; ok && routerSupportsMode(pool, int(spec.ModeID)) {
			return true
		}
	}
	return false
}

func modelHasAccountPool(spec model.ModelSpec, accountPools map[string]int) bool {
	for _, poolID := range spec.PoolCandidates() {
		pool := poolIDToName[poolID]
		if count, ok := accountPools[pool]; ok && count > 0 {
			return true
		}
	}
	return false
}

func routerSupportsMode(pool string, modeID int) bool {
	switch pool {
	case "super":
		return modeID == 0 || modeID == 1 || modeID == 2 || modeID == 4 || modeID == 5
	case "heavy":
		return modeID == 0 || modeID == 1 || modeID == 2 || modeID == 3 || modeID == 4 || modeID == 5
	default:
		return modeID == 1 || modeID == 5
	}
}
