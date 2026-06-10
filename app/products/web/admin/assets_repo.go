package admin

func defaultAdminAssetsRepoProvider() adminAssetsRepository {
	directory := adminAccountDirectory()
	if repo, ok := directory.(adminAssetsRepository); ok {
		return repo
	}
	return nil
}

func adminAssetsLastPage(page int, result adminAssetsListResult) bool {
	if result.Total > 0 {
		return page*adminAssetsPageSize >= result.Total
	}
	return page >= result.TotalPages
}
