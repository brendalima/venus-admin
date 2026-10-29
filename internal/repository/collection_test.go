package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

// newTestCollection creates a collection with a name unique to this run,
// and removes it when the test ends.
func newTestCollection(t *testing.T, repo *Collection, name string) domain.Collection {
	t.Helper()
	c, err := repo.Create(context.Background(), domain.Collection{ID: uuid.New().String(), Name: name + " " + uuid.New().String()[:8]})
	if err != nil {
		t.Fatalf("creating collection: %v", err)
	}
	t.Cleanup(func() { _ = repo.Delete(context.Background(), c.ID) })
	return c
}

func TestCollectionCreateRenameAndList(t *testing.T) {
	db := testDB(t)
	repo := MustNewCollection(db)
	ctx := context.Background()

	older := newTestCollection(t, repo, "Verão")
	newer := newTestCollection(t, repo, "Inverno")
	if older.CreatedAt.IsZero() || older.Pieces != 0 {
		t.Errorf("created %+v, want a time and no pieces", older)
	}

	// Names clash in any case.
	var conflict *domain.ConflictError
	_, err := repo.Create(ctx, domain.Collection{ID: uuid.New().String(), Name: "  "})
	if err == nil {
		t.Error("a blank name was stored")
	}
	if _, err := repo.Create(ctx, domain.Collection{ID: uuid.New().String(), Name: older.Name}); !errors.As(err, &conflict) || conflict.Subject != domain.ConflictCollection {
		t.Errorf("same name: err = %v, want a collection conflict", err)
	}
	if _, err := repo.Rename(ctx, newer.ID, older.Name); !errors.As(err, &conflict) {
		t.Errorf("rename onto another's name: err = %v, want a conflict", err)
	}

	renamed, err := repo.Rename(ctx, older.ID, older.Name+" (2)")
	if err != nil || renamed.Name != older.Name+" (2)" {
		t.Fatalf("renamed %+v, %v", renamed, err)
	}
	var notFound *domain.NotFoundError
	if _, err := repo.Rename(ctx, uuid.New().String(), "Outono"); !errors.As(err, &notFound) {
		t.Errorf("rename unknown: err = %v, want not found", err)
	}
	if _, err := repo.GetByID(ctx, "not-a-uuid"); !errors.As(err, &notFound) {
		t.Errorf("get by a malformed ID: err = %v, want not found", err)
	}

	all, err := repo.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var mine []string
	for _, c := range all {
		if c.ID == older.ID || c.ID == newer.ID {
			mine = append(mine, c.ID)
		}
	}
	if len(mine) != 2 || mine[0] != newer.ID {
		t.Errorf("listed %v, want both, the newest first", mine)
	}
}

func TestCollectionPiecesAndDelete(t *testing.T) {
	db := testDB(t)
	repo := MustNewCollection(db)
	products := MustNewProduct(db)
	ctx := context.Background()
	c := newTestCollection(t, repo, "Verão")

	in := func() domain.Product {
		p := newTestProduct(t, db)
		p.CollectionID = c.ID
		created, err := products.Create(ctx, p)
		if err != nil {
			t.Fatalf("creating product: %v", err)
		}
		return created
	}
	first, second := in(), in()
	if first.CollectionID != c.ID || first.CollectionName != c.Name {
		t.Errorf("created in %q %q, want %q %q", first.CollectionID, first.CollectionName, c.ID, c.Name)
	}
	if _, err := products.SetArchived(ctx, second.ID, true); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetByID(ctx, c.ID)
	if err != nil || got.Pieces != 2 {
		t.Errorf("pieces %d, %v, want 2, the archived one counted", got.Pieces, err)
	}

	// A rename reaches the pieces, and lights the publish badge.
	// site_state is a single shared row; put it back as it was.
	var before sql.NullTime
	if err := db.QueryRowContext(ctx, `SELECT last_published_at FROM site_state`).Scan(&before); err != nil {
		t.Fatalf("reading site state: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `UPDATE site_state SET last_published_at = $1`, before)
	})
	if err := products.MarkPublished(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Rename(ctx, c.ID, c.Name+" (2)"); err != nil {
		t.Fatal(err)
	}
	if p, _ := products.GetByID(ctx, first.ID); p.CollectionName != c.Name+" (2)" {
		t.Errorf("piece's collection %q after the rename", p.CollectionName)
	}
	if changed, err := products.HasUnpublishedChanges(ctx); err != nil || !changed {
		t.Errorf("unpublished changes = %v, %v after a rename, want true", changed, err)
	}

	// A piece cannot join a collection that does not exist.
	stray := newTestProduct(t, db)
	stray.CollectionID = uuid.New().String()
	if _, err := products.Create(ctx, stray); err == nil {
		t.Error("a piece was stored in a collection that does not exist")
	}

	// Deleting the collection leaves its pieces, without one.
	if err := repo.Delete(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{first.ID, second.ID} {
		p, err := products.GetByID(ctx, id)
		if err != nil || p.CollectionID != "" || p.CollectionName != "" {
			t.Errorf("piece %+v, %v after deleting its collection, want it kept without one", p, err)
		}
	}
	var notFound *domain.NotFoundError
	if err := repo.Delete(ctx, c.ID); !errors.As(err, &notFound) {
		t.Errorf("deleting twice: err = %v, want not found", err)
	}

	// Out of a collection again.
	first.CollectionID = c.ID
	if _, err := products.Update(ctx, first); err == nil {
		t.Error("a piece was moved into a deleted collection")
	}
}
