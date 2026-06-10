package storage

var LocalMediaCache = NewLocalMediaCacheStore(LocalMediaCacheOptions{})

func SaveLocalImage(raw []byte, mime, fileID string) (string, error) {
	return LocalMediaCache.SaveImage(raw, mime, fileID)
}

func SaveLocalVideo(raw []byte, fileID string) (string, error) {
	return LocalMediaCache.SaveVideo(raw, fileID)
}

func ClearLocalMediaFiles(mediaType MediaType) (int, error) {
	return LocalMediaCache.Clear(mediaType)
}

func DeleteLocalMediaFile(mediaType MediaType, name string) (bool, error) {
	return LocalMediaCache.Delete(mediaType, name)
}

func ReconcileLocalMediaCache(mediaTypes ...MediaType) error {
	if len(mediaTypes) == 0 {
		mediaTypes = []MediaType{MediaTypeImage, MediaTypeVideo}
	}
	for _, mediaType := range mediaTypes {
		if err := LocalMediaCache.Reconcile(mediaType); err != nil {
			return err
		}
	}
	return nil
}
