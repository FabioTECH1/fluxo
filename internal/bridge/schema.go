package bridge

import (
	"context"
	"database/sql"

	"fluxo/internal/database"
)

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func checkSchema(ctx context.Context, q queryer) error {
	return database.ValidateCurrentSchema(ctx, q)
}

func checkSchemaMode(ctx context.Context, q queryer, collectAll bool) error {
	return database.ValidateCurrentSchemaMode(ctx, q, collectAll)
}
