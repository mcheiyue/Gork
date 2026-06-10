package openai

import (
	"net/http"
	"strings"
	"time"

	"github.com/jiujiu532/grok2api/app/control/model"
)

var poolIDToName = map[int]string{
	0: "basic",
	1: "super",
	2: "heavy",
}

func handleListModels(w http.ResponseWriter, r *http.Request) {
	pools := routerAvailablePools(r)
	created := time.Now().Unix()
	data := make([]map[string]any, 0)
	for _, spec := range model.ListEnabled() {
		if !modelAvailableForPools(spec, pools) {
			continue
		}
		data = append(data, routerModelBody(spec, created))
	}
	writeRouterJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   data,
	})
}

func handleGetModel(w http.ResponseWriter, r *http.Request) {
	modelID := strings.TrimPrefix(r.URL.Path, "/v1/models/")
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
	for _, poolID := range spec.PoolCandidates() {
		pool := poolIDToName[poolID]
		if _, ok := pools[pool]; ok && routerSupportsMode(pool, int(spec.ModeID)) {
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
