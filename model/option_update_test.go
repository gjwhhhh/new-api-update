package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpdateOptionReturnsDatabaseWriteErrors(t *testing.T) {
	transaction := DB.Begin()
	require.NoError(t, transaction.Error)
	require.NoError(t, transaction.Rollback().Error)

	previousDB := DB
	DB = transaction
	t.Cleanup(func() {
		DB = previousDB
	})

	require.Error(t, UpdateOption("option-write-error-test", "value"))
}
