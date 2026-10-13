package repository

import (
	"context"

	"gorm.io/gorm"
)

// findFirst returns the first row of T matching the query, or the underlying
// error (including gorm.ErrRecordNotFound) unchanged.
func findFirst[T any](ctx context.Context, db *gorm.DB, query string, args ...any) (*T, error) {
	v, err := gorm.G[T](db).Where(query, args...).First(ctx)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// notFoundIfNone returns res.Error if set, gorm.ErrRecordNotFound if the
// statement affected no rows, and nil otherwise.
func notFoundIfNone(res *gorm.DB) error {
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
