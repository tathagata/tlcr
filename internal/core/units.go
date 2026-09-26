package core

// CurrentUnits uses the same parser and fallback for scans and fresh source reads.
func (idx *Index) CurrentUnits(path string, _ *FileEntry, source string) []Unit {
	registry := idx.registry
	if registry == nil {
		registry = DefaultRegistry()
	}
	entry, ok := registry.Parse(path, []byte(source))
	if !ok {
		return []Unit{}
	}
	return entry.Units
}
