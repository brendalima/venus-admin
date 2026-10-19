package repository

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"

	"github.com/brendalima/venus-admin/adapters"
	"github.com/brendalima/venus-admin/internal/core/domain"
)

// Integration tests: they talk to a real Postgres and skip without one.
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	// Missing files are fine: the test skips below without a URL.
	_ = godotenv.Load("../../.env.local")
	_ = godotenv.Load("../../.env")

	// Only the dedicated test branch: these tests write and delete rows,
	// so they must never fall back to DATABASE_URL, which is dev's or
	// production's.
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("no TEST_DATABASE_URL — skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := adapters.NewDatabase(ctx, dsn)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := adapters.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrating: %v", err)
	}
	return db
}

// newTestProduct returns a valid product with a slug unique to this run,
// and removes it when the test ends.
func newTestProduct(t *testing.T, db *sql.DB) domain.Product {
	t.Helper()
	id := uuid.New().String()
	slug := "test-" + id
	t.Cleanup(func() {
		// Best effort: a leftover row is recognisable by its slug.
		_, _ = db.ExecContext(context.Background(), `DELETE FROM products WHERE id = $1`, id)
	})
	return domain.Product{
		ID:          id,
		Slug:        slug,
		Name:        "Biquíni de Teste",
		PriceCents:  18900,
		Sizes:       []string{"P", "M"},
		Description: "description",
	}
}

func insertImage(t *testing.T, db *sql.DB, productID string, position int) string {
	t.Helper()
	id := uuid.New().String()
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO product_images (id, product_id, key_600, key_1200, position)
		 VALUES ($1, $2, 'k600', 'k1200', $3)`,
		id, productID, position)
	if err != nil {
		t.Fatalf("inserting image: %v", err)
	}
	return id
}

func TestProductCreateAndGet(t *testing.T) {
	db := testDB(t)
	repo := MustNewProduct(db)
	ctx := context.Background()

	first, err := repo.Create(ctx, newTestProduct(t, db))
	if err != nil {
		t.Fatalf("creating: %v", err)
	}
	if first.CreatedAt.IsZero() || first.UpdatedAt.IsZero() {
		t.Error("timestamps were not filled in")
	}
	second, err := repo.Create(ctx, newTestProduct(t, db))
	if err != nil {
		t.Fatalf("creating second: %v", err)
	}
	if second.Position <= first.Position {
		t.Errorf("new product position %d is not after %d", second.Position, first.Position)
	}

	byID, err := repo.GetByID(ctx, first.ID)
	if err != nil {
		t.Fatalf("getting by id: %v", err)
	}
	bySlug, err := repo.GetBySlug(ctx, first.Slug)
	if err != nil {
		t.Fatalf("getting by slug: %v", err)
	}

	for name, got := range map[string]domain.Product{"by id": byID, "by slug": bySlug} {
		if got.ID != first.ID || got.Name != first.Name || got.PriceCents != 18900 {
			t.Errorf("%s: got %+v", name, got)
		}
		if len(got.Sizes) != 2 || got.Sizes[1] != "M" {
			t.Errorf("%s: sizes = %v", name, got.Sizes)
		}
		// Details was nil on input: it must be stored as '{}', not NULL.
		if got.Details == nil || len(got.Details) != 0 {
			t.Errorf("%s: details = %#v, want an empty list", name, got.Details)
		}
		if got.Images == nil {
			t.Errorf("%s: images is nil, want an empty list", name)
		}
	}
}

func TestProductImagesComeInOrder(t *testing.T) {
	db := testDB(t)
	repo := MustNewProduct(db)
	ctx := context.Background()

	p, err := repo.Create(ctx, newTestProduct(t, db))
	if err != nil {
		t.Fatalf("creating: %v", err)
	}
	second := insertImage(t, db, p.ID, 2)
	first := insertImage(t, db, p.ID, 1)

	got, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("getting: %v", err)
	}
	if len(got.Images) != 2 {
		t.Fatalf("got %d images, want 2", len(got.Images))
	}
	cover, _ := got.Cover()
	if cover.ID != first || cover.Key600 != "k600" {
		t.Errorf("cover = %+v, want the image at position 1", cover)
	}
	if got.Images[1].ID != second {
		t.Errorf("second image = %s, want %s", got.Images[1].ID, second)
	}

	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	for _, lp := range list {
		if lp.ID == p.ID && len(lp.Images) != 2 {
			t.Errorf("listed product has %d images, want 2", len(lp.Images))
		}
	}
}

func TestProductUpdate(t *testing.T) {
	db := testDB(t)
	repo := MustNewProduct(db)
	ctx := context.Background()

	p, err := repo.Create(ctx, newTestProduct(t, db))
	if err != nil {
		t.Fatalf("creating: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	p.Name = "Maiô de Teste"
	p.PriceCents = 21900
	p.SoldOut = true
	p.Details = []string{"forro duplo"}

	updated, err := repo.Update(ctx, p)
	if err != nil {
		t.Fatalf("updating: %v", err)
	}
	if !updated.UpdatedAt.After(p.CreatedAt) {
		t.Errorf("updated_at %v is not after created_at %v", updated.UpdatedAt, p.CreatedAt)
	}

	got, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("getting: %v", err)
	}
	if got.Name != "Maiô de Teste" || got.PriceCents != 21900 || !got.SoldOut || len(got.Details) != 1 {
		t.Errorf("update did not stick: %+v", got)
	}
}

func TestProductSlugConflict(t *testing.T) {
	db := testDB(t)
	repo := MustNewProduct(db)
	ctx := context.Background()

	existing, err := repo.Create(ctx, newTestProduct(t, db))
	if err != nil {
		t.Fatalf("creating: %v", err)
	}

	clash := newTestProduct(t, db)
	clash.Slug = existing.Slug

	var conflict *domain.ConflictError
	if _, err := repo.Create(ctx, clash); !errors.As(err, &conflict) {
		t.Errorf("create with a used slug: err = %v, want ConflictError", err)
	}

	other, err := repo.Create(ctx, newTestProduct(t, db))
	if err != nil {
		t.Fatalf("creating other: %v", err)
	}
	other.Slug = existing.Slug
	if _, err := repo.Update(ctx, other); !errors.As(err, &conflict) {
		t.Errorf("update to a used slug: err = %v, want ConflictError", err)
	}
}

func TestProductNotFound(t *testing.T) {
	db := testDB(t)
	repo := MustNewProduct(db)
	ctx := context.Background()

	missing := uuid.New().String()
	tests := []struct {
		name string
		call func() error
	}{
		{"get unknown id", func() error { _, err := repo.GetByID(ctx, missing); return err }},
		{"get malformed id", func() error { _, err := repo.GetByID(ctx, "not-a-uuid"); return err }},
		{"get unknown slug", func() error { _, err := repo.GetBySlug(ctx, "no-such-slug-"+missing); return err }},
		{"update unknown id", func() error {
			p := newTestProduct(t, db)
			p.ID = missing
			_, err := repo.Update(ctx, p)
			return err
		}},
		{"delete unknown id", func() error { return repo.Delete(ctx, missing) }},
		{"delete malformed id", func() error { return repo.Delete(ctx, "42") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var notFound *domain.NotFoundError
			if err := tt.call(); !errors.As(err, &notFound) {
				t.Errorf("err = %v, want NotFoundError", err)
			}
		})
	}
}

func TestProductDeleteCascadesToImages(t *testing.T) {
	db := testDB(t)
	repo := MustNewProduct(db)
	ctx := context.Background()

	p, err := repo.Create(ctx, newTestProduct(t, db))
	if err != nil {
		t.Fatalf("creating: %v", err)
	}
	insertImage(t, db, p.ID, 1)

	if err := repo.Delete(ctx, p.ID); err != nil {
		t.Fatalf("deleting: %v", err)
	}

	var notFound *domain.NotFoundError
	if _, err := repo.GetByID(ctx, p.ID); !errors.As(err, &notFound) {
		t.Errorf("deleted product is still readable: err = %v", err)
	}
	var images int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM product_images WHERE product_id = $1`, p.ID).Scan(&images); err != nil {
		t.Fatalf("counting images: %v", err)
	}
	if images != 0 {
		t.Errorf("%d image rows survived the delete", images)
	}
}

// Every kind of catalogue change must raise the "pending changes" badge,
// including the two a per-product updated_at cannot see: a delete and a
// photo change.
func TestUnpublishedChanges(t *testing.T) {
	db := testDB(t)
	repo := MustNewProduct(db)
	ctx := context.Background()

	// site_state is a single shared row; put it back as it was.
	var before sql.NullTime
	if err := db.QueryRowContext(ctx, `SELECT last_published_at FROM site_state`).Scan(&before); err != nil {
		t.Fatalf("reading site state: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `UPDATE site_state SET last_published_at = $1`, before)
	})

	p, err := repo.Create(ctx, newTestProduct(t, db))
	if err != nil {
		t.Fatalf("creating: %v", err)
	}

	changes := []struct {
		name   string
		change func() error
	}{
		{"update", func() error { p.Name = "Outro Nome"; _, err := repo.Update(ctx, p); return err }},
		{"image added", func() error { insertImage(t, db, p.ID, 1); return nil }},
		{"archived", func() error { _, err := repo.SetArchived(ctx, p.ID, true); return err }},
		{"restored", func() error { _, err := repo.SetArchived(ctx, p.ID, false); return err }},
		{"delete", func() error { return repo.Delete(ctx, p.ID) }},
	}

	for _, c := range changes {
		t.Run(c.name, func(t *testing.T) {
			if err := repo.MarkPublished(ctx); err != nil {
				t.Fatalf("marking published: %v", err)
			}
			// Other packages' integration tests may write to the same
			// database concurrently, so this can, rarely, see their change.
			pending, err := repo.HasUnpublishedChanges(ctx)
			if err != nil {
				t.Fatalf("checking: %v", err)
			}
			if pending {
				t.Error("pending changes right after publishing")
			}

			// now() is the transaction start time; make sure the change
			// lands strictly after the publish.
			time.Sleep(10 * time.Millisecond)
			if err := c.change(); err != nil {
				t.Fatalf("changing: %v", err)
			}

			pending, err = repo.HasUnpublishedChanges(ctx)
			if err != nil {
				t.Fatalf("checking: %v", err)
			}
			if !pending {
				t.Error("change was not reported as pending")
			}
		})
	}
}

func listedIDs(t *testing.T, list func(context.Context) ([]domain.Product, error)) map[string]domain.Product {
	t.Helper()
	ps, err := list(context.Background())
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	byID := map[string]domain.Product{}
	for _, p := range ps {
		byID[p.ID] = p
	}
	return byID
}

// An archived piece must leave every list the site is built from, and
// come back from the archive exactly as it was.
func TestProductArchive(t *testing.T) {
	db := testDB(t)
	repo := MustNewProduct(db)
	ctx := context.Background()

	kept, err := repo.Create(ctx, newTestProduct(t, db))
	if err != nil {
		t.Fatalf("creating: %v", err)
	}
	p, err := repo.Create(ctx, newTestProduct(t, db))
	if err != nil {
		t.Fatalf("creating: %v", err)
	}
	insertImage(t, db, p.ID, 0)
	insertImage(t, db, p.ID, 1)
	countBefore, err := repo.CountArchived(ctx)
	if err != nil {
		t.Fatalf("counting: %v", err)
	}

	archived, err := repo.SetArchived(ctx, p.ID, true)
	if err != nil {
		t.Fatalf("archiving: %v", err)
	}
	if !archived.Archived() || len(archived.Images) != 2 || archived.Position != p.Position {
		t.Errorf("archived = %+v", archived)
	}

	listed := listedIDs(t, repo.List)
	if _, ok := listed[p.ID]; ok {
		t.Error("List returns an archived product: it would reach the site")
	}
	if _, ok := listed[kept.ID]; !ok {
		t.Error("List lost a product that was not archived")
	}
	inArchive := listedIDs(t, repo.ListArchived)
	if got, ok := inArchive[p.ID]; !ok || len(got.Images) != 2 || !got.Archived() {
		t.Errorf("ListArchived: %+v, found %v", got, ok)
	}
	if _, ok := inArchive[kept.ID]; ok {
		t.Error("ListArchived returns a product in the collection")
	}
	if n, err := repo.CountArchived(ctx); err != nil || n != countBefore+1 {
		t.Errorf("CountArchived = %d, %v; want %d", n, err, countBefore+1)
	}

	// The edit screen and the archive still load it.
	got, err := repo.GetByID(ctx, p.ID)
	if err != nil || !got.Archived() || !got.ArchivedAt.Equal(archived.ArchivedAt) {
		t.Errorf("GetByID = %+v, %v", got, err)
	}
	if got, err := repo.GetBySlug(ctx, p.Slug); err != nil || !got.Archived() {
		t.Errorf("GetBySlug = %+v, %v", got, err)
	}

	// Saving its fields leaves it archived, and says so.
	got.Name = "Outro Nome"
	if updated, err := repo.Update(ctx, got); err != nil || !updated.Archived() {
		t.Errorf("Update of an archived product = %+v, %v", updated, err)
	}

	restored, err := repo.SetArchived(ctx, p.ID, false)
	if err != nil {
		t.Fatalf("restoring: %v", err)
	}
	if restored.Archived() || restored.Position != p.Position || len(restored.Images) != 2 {
		t.Errorf("restored = %+v, want position %d and both images", restored, p.Position)
	}
	if _, ok := listedIDs(t, repo.List)[p.ID]; !ok {
		t.Error("a restored product is not back in List")
	}
	if _, ok := listedIDs(t, repo.ListArchived)[p.ID]; ok {
		t.Error("a restored product is still in ListArchived")
	}

	var nf *domain.NotFoundError
	for _, id := range []string{uuid.New().String(), "not-a-uuid"} {
		if _, err := repo.SetArchived(ctx, id, true); !errors.As(err, &nf) {
			t.Errorf("SetArchived(%q): err = %v, want NotFoundError", id, err)
		}
	}
}
