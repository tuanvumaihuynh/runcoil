package paging

import (
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSize(t *testing.T) {
	assert.Equal(t, int32(DefaultPageSize), Params{}.Size())
	assert.Equal(t, int32(DefaultPageSize), Params{PageSize: -1}.Size())
	assert.Equal(t, int32(2), Params{PageSize: 2}.Size())
	assert.Equal(t, int32(MaxPageSize), Params{PageSize: MaxPageSize + 1}.Size())
	assert.Equal(t, int32(3), Params{PageSize: 2}.Limit())
}

func TestNewListAndAfterID(t *testing.T) {
	ids := []uuid.UUID{uuid.NewV7(), uuid.NewV7(), uuid.NewV7()}
	key := func(id uuid.UUID) uuid.UUID { return id }

	first := NewList(Params{PageSize: 2}, ids, key)

	assert.Equal(t, ids[:2], first.Items)
	require.NotEmpty(t, first.NextPageToken)

	afterID, err := Params{PageToken: first.NextPageToken}.AfterID()
	require.NoError(t, err)
	assert.Equal(t, &ids[1], afterID)

	last := NewList(Params{PageSize: 2}, ids[2:], key)

	assert.Equal(t, ids[2:], last.Items)
	assert.Empty(t, last.NextPageToken)
}

func TestAfterIDFirstPage(t *testing.T) {
	afterID, err := Params{}.AfterID()

	require.NoError(t, err)
	assert.Nil(t, afterID)
}

func TestAfterIDInvalid(t *testing.T) {
	for _, token := range []string{"abc", "!!", "AAAA"} {
		_, err := Params{PageToken: token}.AfterID()
		assert.ErrorIs(t, err, ErrInvalidPageToken, token)
	}
}
