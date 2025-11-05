package harmonydb

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDatabaseOpen(t *testing.T) {
	t.Run("open creates valid database", func(t *testing.T) {
		db, err := Open()
		require.NoError(t, err)
		assert.NotNil(t, db)
	})

	t.Run("multiple database instances", func(t *testing.T) {
		db1, err := Open()
		require.NoError(t, err)
		assert.NotNil(t, db1)

		db2, err := Open()
		require.NoError(t, err)
		assert.NotNil(t, db2)

		assert.NotSame(t, db1, db2)
	})
}
