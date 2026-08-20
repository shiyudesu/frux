package applicationsearch

import "errors"

var ErrSearchFailed = errors.New("search failed")
var ErrSemanticContinuationUnavailable = errors.New("semantic search continuation unavailable")
var ErrInvalidHybridSearchConfig = errors.New("invalid hybrid search config")
var ErrSemanticVideoUnavailable = errors.New("semantic video discovery unavailable")
