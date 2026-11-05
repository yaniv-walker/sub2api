//go:build unit

package repository

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestModelCatalogCASConfigurationAndSnapshotGuard(t *testing.T) {
	for _, rows := range []int64{0, 1} {
		t.Run(string(rune('0'+rows)), func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			t.Cleanup(func() { _ = client.Close() })
			repo := newAccountRepositoryWithSQL(client, db, nil)
			account := &service.Account{ID: 15, Platform: "openai", Type: "apikey", UpdatedAt: time.Now()}
			next := service.AccountModelCatalogSnapshot{Models: []string{"new-model"}, Status: "ready", ConfigUpdatedAt: account.UpdatedAt}
			body, err := json.Marshal(next)
			require.NoError(t, err)
			mock.ExpectExec(regexp.QuoteMeta(compareAndSwapModelCatalogSQL)).
				WithArgs(account.ID, account.UpdatedAt, "openai", "apikey", "null", string(body)).
				WillReturnResult(sqlmock.NewResult(0, rows))
			applied, err := repo.CompareAndSwapModelCatalog(context.Background(), account, next)
			require.NoError(t, err)
			require.Equal(t, rows > 0, applied)
			require.NoError(t, mock.ExpectationsWereMet())
			require.NotContains(t, compareAndSwapModelCatalogSQL, "scheduler_outbox", "snapshot persistence must not recursively trigger discovery")
			require.NotContains(t, compareAndSwapModelCatalogSQL, "updated_at = NOW()", "an observation must not become a configuration save")
		})
	}
}
