package runner

import "time"

// ResetModelCacheForTest clears the package-level model cache so tests observe a cold call.
func ResetModelCacheForTest() {
	modelCacheLock.Lock()
	cachedModels = nil
	cacheExpiry = time.Time{}
	modelCacheLock.Unlock()
}
