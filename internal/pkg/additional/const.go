package additional

const (
	latestTag       = "latest"
	collectorPrefix = "[AdditionalImagesCollector] "
	errMsg          = collectorPrefix + "%s"

	// additionalImagesExtractDir is the working-dir subdirectory that holds
	// state cached between the mirrorToDisk and diskToMirror phases for
	// additionalImages entries.
	additionalImagesExtractDir = "hold-additional-images"
	// tagListCacheDir holds one JSON file per repository listing the tags
	// resolved for a TagsByRegex entry during mirrorToDisk, so diskToMirror
	// (which may run disconnected, on a different machine) can replay the
	// same tag set without a live registry call.
	tagListCacheDir = "tag-regex-cache"
)
