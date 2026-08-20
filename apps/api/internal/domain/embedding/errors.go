package domainembedding

import "errors"

var ErrDimensionMismatch = errors.New("embedding dimension mismatch")
var ErrVideoEmbeddingNotFound = errors.New("video embedding not found")
var ErrInvalidHashText = errors.New("invalid hash embedding text")
var ErrInvalidMultimodalJob = errors.New("invalid multimodal embedding job")
var ErrMultimodalJobNotFound = errors.New("multimodal embedding job not found")
var ErrMultimodalLeaseLost = errors.New("multimodal embedding lease lost")
var ErrInvalidMultimodalVectorFact = errors.New("invalid multimodal vector fact")
var ErrMultimodalVectorFactNotFound = errors.New("multimodal vector fact not found")
var ErrInvalidMultimodalProjection = errors.New("invalid multimodal projection")
var ErrMultimodalOperationConflict = errors.New("multimodal operation conflict")
