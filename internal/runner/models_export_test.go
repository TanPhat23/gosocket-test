package runner

// ResetModelCacheForTest clears the package-level model cache so tests observe a cold call.
func ResetModelCacheForTest() {
	modelCache.Store(nil)
}
