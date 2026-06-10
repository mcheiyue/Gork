package admin

import (
	"net/http"
)

func handleAdminTokensList(w http.ResponseWriter, r *http.Request) {
	repo, err := adminTokensRepo()
	if err != nil {
		writeAdminError(w, err)
		return
	}
	query, err := adminTokensListQuery(r)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	result, err := repo.ListAccounts(r.Context(), query)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	facets, err := repo.ListFacets(r.Context())
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeAdminJSON(w, http.StatusOK, adminTokensListPayload(result, facets))
}

func adminTokensListPayload(result adminAssetsListResult, facets adminTokensFacetSnapshot) map[string]any {
	tokens := make([]map[string]any, 0, len(result.Items))
	for _, item := range result.Items {
		tokens = append(tokens, adminTokenSerialize(item))
	}
	return map[string]any{
		"tokens": tokens,
		"pagination": map[string]any{
			"total": result.Total, "page": result.Page, "page_size": result.PageSize, "total_pages": result.TotalPages,
		},
		"facets":   facets.ToMap(),
		"revision": result.Revision,
	}
}
