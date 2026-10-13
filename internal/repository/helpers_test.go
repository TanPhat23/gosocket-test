package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type helperRow struct {
	ID   string `gorm:"primaryKey"`
	Name string
}

func helperDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.AutoMigrate(&helperRow{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Create(&helperRow{ID: "a", Name: "alpha"}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	return db
}

func TestFindFirst(t *testing.T) {
	db := helperDB(t)
	ctx := context.Background()

	t.Run("found", func(t *testing.T) {
		got, err := findFirst[helperRow](ctx, db, "id = ? AND name = ?", "a", "alpha")
		if err != nil || got == nil || got.ID != "a" || got.Name != "alpha" {
			t.Fatalf("got %+v, err %v", got, err)
		}
	})
	t.Run("not found", func(t *testing.T) {
		got, err := findFirst[helperRow](ctx, db, "id = ?", "missing")
		if !errors.Is(err, gorm.ErrRecordNotFound) || got != nil {
			t.Fatalf("got %+v, err %v", got, err)
		}
	})
	t.Run("query error", func(t *testing.T) {
		got, err := findFirst[helperRow](ctx, db, "no_such_column = ?", 1)
		if err == nil || errors.Is(err, gorm.ErrRecordNotFound) || got != nil {
			t.Fatalf("got %+v, err %v", got, err)
		}
	})
}

func TestNotFoundIfNone(t *testing.T) {
	db := helperDB(t)

	if err := notFoundIfNone(db.Model(&helperRow{}).Where("id = ?", "a").Update("name", "b")); err != nil {
		t.Fatalf("rows>0: %v", err)
	}
	if err := notFoundIfNone(db.Model(&helperRow{}).Where("id = ?", "zzz").Update("name", "b")); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("rows==0: %v", err)
	}
	res := db.Exec("UPDATE no_such_table SET x = 1")
	if err := notFoundIfNone(res); err == nil || errors.Is(err, gorm.ErrRecordNotFound) || err != res.Error {
		t.Fatalf("res.Error must be returned as-is: %v", err)
	}
}
