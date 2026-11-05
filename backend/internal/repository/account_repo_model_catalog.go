package repository

import (
	"context"
	"encoding/json"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.AccountModelCatalogRepository = (*accountRepository)(nil)

// Only explicit full account saves request discovery. Quota/usage/token
// observations also emit scheduler events but must not trigger upstream calls.
func buildModelCatalogSavePayload(groupIDs []int64) map[string]any {
	payload := map[string]any{"model_catalog_sync": true}
	if len(groupIDs) > 0 {
		payload["group_ids"] = groupIDs
	}
	return payload
}

// Compare both configuration revision and the old snapshot. An older request
// cannot overwrite an administrator save or a newer discovery on another node.
// This isolated observation never emits an account-change event (no sync loop)
// and does not change the administrator-facing configuration revision.
const compareAndSwapModelCatalogSQL = `UPDATE accounts
SET extra = jsonb_set(COALESCE(extra, '{}'::jsonb), '{model_square_catalog}', $6::jsonb, true)
WHERE id = $1 AND deleted_at IS NULL AND status = 'active'
  AND updated_at = $2 AND platform = $3 AND type = $4
  AND COALESCE(extra -> 'model_square_catalog', 'null'::jsonb) = $5::jsonb`

func (r *accountRepository) CompareAndSwapModelCatalog(ctx context.Context, expected *service.Account, next service.AccountModelCatalogSnapshot) (bool, error) {
	if scoped := r.accountTransactionScope(ctx); scoped != r {
		return scoped.CompareAndSwapModelCatalog(ctx, expected, next)
	}
	if expected == nil {
		return false, service.ErrAccountNilInput
	}
	oldJSON, err := json.Marshal(expected.GetModelCatalogSnapshot())
	if err != nil {
		return false, err
	}
	nextJSON, err := json.Marshal(next)
	if err != nil {
		return false, err
	}
	result, err := clientFromContext(ctx, r.client).ExecContext(ctx, compareAndSwapModelCatalogSQL,
		expected.ID, expected.UpdatedAt, expected.Platform, expected.Type, string(oldJSON), string(nextJSON))
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected > 0 && dbent.TxFromContext(ctx) == nil {
		r.syncSchedulerAccountSnapshot(ctx, expected.ID)
	}
	return affected > 0, nil
}
