package domainsearch

import "errors"

var (
	ErrEmptyQuery    = errors.New("search query is required")
	ErrInvalidQuery  = errors.New("invalid search query")
	ErrQueryTooLong  = errors.New("search query exceeds 64 Unicode code points")
	ErrInvalidLimit  = errors.New("invalid search limit")
	ErrInvalidCursor = errors.New("invalid search cursor")
)
