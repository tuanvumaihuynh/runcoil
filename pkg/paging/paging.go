// Package paging pages through a list ordered by a UUID key. The page token is opaque to
// clients: it holds the key of the last item of the page before.
package paging

import (
	"encoding/base64"
	"errors"
	"uuid"
)

// The page sizes of a list.
const (
	DefaultPageSize = 50
	MaxPageSize     = 100
)

// ErrInvalidPageToken is returned for a page token that no List gave.
var ErrInvalidPageToken = errors.New("page token is not valid")

// Params asks for one page of a list.
type Params struct {
	// PageSize is the most items the page holds. Zero means DefaultPageSize, and a size
	// above MaxPageSize is MaxPageSize.
	PageSize int32
	// PageToken is the NextPageToken of the page before. Empty asks for the first page.
	PageToken string
}

// Size returns the most items the page holds.
func (p Params) Size() int32 {
	switch {
	case p.PageSize <= 0:
		return DefaultPageSize
	case p.PageSize > MaxPageSize:
		return MaxPageSize
	default:
		return p.PageSize
	}
}

// Limit returns how many items to fetch: one more than the page holds, so NewList can
// tell whether another page follows.
func (p Params) Limit() int32 {
	return p.Size() + 1
}

// AfterID returns the key the page starts after, or nil for the first page. It returns
// ErrInvalidPageToken for a token that no List gave.
func (p Params) AfterID() (*uuid.UUID, error) {
	if p.PageToken == "" {
		return nil, nil
	}

	b, err := base64.RawURLEncoding.DecodeString(p.PageToken)
	if err != nil || len(b) != len(uuid.UUID{}) {
		return nil, ErrInvalidPageToken
	}

	id := uuid.UUID(b)

	return &id, nil
}

// List is one page of a list.
type List[T any] struct {
	Items []T
	// NextPageToken asks for the page after this one. It is empty on the last page.
	NextPageToken string
}

// NewList makes the page of params from items fetched with params.Limit(). key returns
// the key of an item.
func NewList[T any](params Params, items []T, key func(T) uuid.UUID) List[T] {
	size := int(params.Size())
	if len(items) <= size {
		return List[T]{Items: items}
	}

	items = items[:size]
	last := key(items[size-1])

	return List[T]{Items: items, NextPageToken: base64.RawURLEncoding.EncodeToString(last[:])}
}
