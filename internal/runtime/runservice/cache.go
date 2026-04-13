package runservice

// TryServeCache attempts to serve the response from cache based on the provided input.
// CURRENTLY DISABLED: Always returns false as part of cache decoupling.
func (e *Executor) TryServeCache(input []byte) (bool, error) {
	return false, nil
}

// PersistCache saves the execution result to the cache when cache mode allows it.
// CURRENTLY DISABLED: No-op as part of cache decoupling.
func (e *Executor) PersistCache(result []byte) error {
	return nil
}
