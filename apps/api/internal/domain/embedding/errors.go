package domainembedding

import "errors"

var ErrDimensionMismatch = errors.New("embedding dimension mismatch")
var ErrVideoEmbeddingNotFound = errors.New("video embedding not found")
var ErrInvalidHashText = errors.New("invalid hash embedding text")
