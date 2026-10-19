package repository

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

func newImage() domain.Image {
	id := uuid.New().String()
	return domain.Image{ID: id, Key600: "produtos/" + id + "-600.jpg", Key1200: "produtos/" + id + "-1200.jpg"}
}

func newColour(name string, images ...domain.Image) domain.Colour {
	return domain.Colour{ID: uuid.New().String(), Name: name, Images: images}
}

func imageIDs(images []domain.Image) []string {
	ids := make([]string, len(images))
	for i, img := range images {
		ids[i] = img.ID
	}
	return ids
}

func colourNames(colours []domain.Colour) []string {
	names := make([]string, len(colours))
	for i, c := range colours {
		names[i] = c.Name
	}
	return names
}

// editColours loads the product, lets edit change it and say what it
// added and removed, and saves it with UpdateWithColours.
func editColours(ctx context.Context, repo *Product, id string, edit func(*domain.Product, *domain.ColourChanges)) error {
	p, err := repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	var changes domain.ColourChanges
	edit(&p, &changes)
	_, _, err = repo.UpdateWithColours(ctx, p, changes)
	return err
}

func TestProductCreateWithColours(t *testing.T) {
	db := testDB(t)
	repo := MustNewProduct(db)
	ctx := context.Background()

	p := newTestProduct(t, db)
	p.Sizes = []string{"P", "M", "G"}
	a, b, c := newImage(), newImage(), newImage()
	bege, vinho := newColour("Bege", b, a), newColour("Vinho", c)
	// Given out of size order: they come back in the product's.
	bege.Stock = []domain.Stock{{Size: "M", Quantity: 2}, {Size: "P", Quantity: 1}}
	vinho.Stock = []domain.Stock{{Size: "P", Quantity: 3}, {Size: "G", Quantity: 0}}
	p.Colours = []domain.Colour{bege, vinho}

	created, err := repo.Create(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}

	for name, got := range map[string]domain.Product{"created": created, "stored": stored} {
		if !slices.Equal(colourNames(got.Colours), []string{"Bege", "Vinho"}) {
			t.Fatalf("%s colours %v, want [Bege Vinho]", name, colourNames(got.Colours))
		}
		g, v := got.Colours[0], got.Colours[1]
		if g.ID != bege.ID || g.ProductID != p.ID || g.Position != 0 || v.Position != 1 || g.CreatedAt.IsZero() {
			t.Errorf("%s first colour %+v", name, g)
		}
		if got := imageIDs(g.Images); !slices.Equal(got, []string{b.ID, a.ID}) {
			t.Errorf("%s Bege images %v, want [b a]", name, got)
		}
		if img := g.Images[0]; img.ColourID != bege.ID || img.Key600 != b.Key600 || img.Position != 0 || g.Images[1].Position != 1 {
			t.Errorf("%s Bege cover %+v", name, img)
		}
		if got := imageIDs(v.Images); !slices.Equal(got, []string{c.ID}) {
			t.Errorf("%s Vinho images %v, want [c]", name, got)
		}
		if want := []domain.Stock{{Size: "P", Quantity: 1}, {Size: "M", Quantity: 2}}; !slices.Equal(g.Stock, want) {
			t.Errorf("%s Bege stock %v, want %v", name, g.Stock, want)
		}
		// Zero is stored: G is counted and unavailable.
		if want := []domain.Stock{{Size: "P", Quantity: 3}, {Size: "G", Quantity: 0}}; !slices.Equal(v.Stock, want) {
			t.Errorf("%s Vinho stock %v, want %v", name, v.Stock, want)
		}
	}

	// The release before colours reads photos by product_id.
	var withoutProduct int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM product_images WHERE colour_id = ANY($1) AND product_id IS DISTINCT FROM $2`,
		[]string{bege.ID, vinho.ID}, p.ID,
	).Scan(&withoutProduct); err != nil {
		t.Fatal(err)
	}
	if withoutProduct != 0 {
		t.Errorf("%d photos stored without their product_id", withoutProduct)
	}
}

// A product with colours Bege (photos a, b, c; P counted) and Vinho
// (photo d; not counted), in that order.
func productWithColours(t *testing.T, repo *Product, p domain.Product) domain.Product {
	t.Helper()
	bege := newColour("Bege", newImage(), newImage(), newImage())
	bege.Stock = []domain.Stock{{Size: "P", Quantity: 1}}
	p.Colours = []domain.Colour{bege, newColour("Vinho", newImage())}
	created, err := repo.Create(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func TestProductUpdateWithColours(t *testing.T) {
	db := testDB(t)
	repo := MustNewProduct(db)
	ctx := context.Background()
	p := productWithColours(t, repo, newTestProduct(t, db))
	bege, vinho := p.Colours[0], p.Colours[1]
	a, b, c, d := bege.Images[0], bege.Images[1], bege.Images[2], vinho.Images[0]

	// Vinho goes, photos and all. Bege moves second, loses b, gets e, and
	// c becomes its cover; its stock changes P, adds M. Azul is new, first,
	// with photo f and M counted.
	e, f := newImage(), newImage()
	azul := newColour("Azul", f)
	azul.Stock = []domain.Stock{{Size: "M", Quantity: 0}}
	bege.Images = []domain.Image{c, e, a}
	bege.Stock = []domain.Stock{{Size: "P", Quantity: 4}, {Size: "M", Quantity: 1}}
	p.Name = "Novo nome"
	p.Colours = []domain.Colour{azul, bege}

	updated, gone, err := repo.UpdateWithColours(ctx, p, domain.ColourChanges{
		AddedColours:   []string{azul.ID},
		RemovedColours: []string{vinho.ID},
		AddedImages:    []string{e.ID, f.ID},
		RemovedImages:  []string{b.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	goneIDs, want := imageIDs(gone), []string{b.ID, d.ID}
	slices.Sort(goneIDs)
	slices.Sort(want)
	if !slices.Equal(goneIDs, want) {
		t.Errorf("gone %v, want b and the removed colour's d", goneIDs)
	}
	for _, img := range gone {
		if img.Key1200 == "" {
			t.Errorf("gone image %s came back without its keys", img.ID)
		}
	}

	stored, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string]domain.Product{"returned": updated, "stored": stored} {
		if got.Name != "Novo nome" {
			t.Errorf("%s name = %q", name, got.Name)
		}
		if !slices.Equal(colourNames(got.Colours), []string{"Azul", "Bege"}) {
			t.Fatalf("%s colours %v, want [Azul Bege]", name, colourNames(got.Colours))
		}
		for i, c := range got.Colours {
			if int(c.Position) != i {
				t.Errorf("%s colour %s at position %d, want %d", name, c.Name, c.Position, i)
			}
			for j, img := range c.Images {
				if int(img.Position) != j || img.ColourID != c.ID {
					t.Errorf("%s %s photo %d: %+v", name, c.Name, j, img)
				}
			}
		}
		if got := imageIDs(got.Colours[0].Images); !slices.Equal(got, []string{f.ID}) {
			t.Errorf("%s Azul images %v, want [f]", name, got)
		}
		if got := imageIDs(got.Colours[1].Images); !slices.Equal(got, []string{c.ID, e.ID, a.ID}) {
			t.Errorf("%s Bege images %v, want [c e a]", name, got)
		}
		if !slices.Equal(got.Colours[0].Stock, azul.Stock) || !slices.Equal(got.Colours[1].Stock, bege.Stock) {
			t.Errorf("%s stock %v and %v, want %v and %v", name,
				got.Colours[0].Stock, got.Colours[1].Stock, azul.Stock, bege.Stock)
		}
	}

	var vinhoRows int
	if err := db.QueryRowContext(ctx,
		`SELECT (SELECT count(*) FROM product_colours WHERE id = $1) + (SELECT count(*) FROM product_images WHERE colour_id = $1)`,
		vinho.ID,
	).Scan(&vinhoRows); err != nil {
		t.Fatal(err)
	}
	if vinhoRows != 0 {
		t.Errorf("%d rows of the removed colour are left", vinhoRows)
	}
}

func TestProductUpdateWithColoursStock(t *testing.T) {
	db := testDB(t)
	repo := MustNewProduct(db)
	ctx := context.Background()
	p := productWithColours(t, repo, newTestProduct(t, db))

	// Bege stops being counted; Vinho starts.
	p.Colours[0].Stock = nil
	p.Colours[1].Stock = []domain.Stock{{Size: "M", Quantity: 2}}
	if _, _, err := repo.UpdateWithColours(ctx, p, domain.ColourChanges{}); err != nil {
		t.Fatal(err)
	}

	stored, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Colours[0].TracksStock() {
		t.Errorf("Bege stock %v, want none", stored.Colours[0].Stock)
	}
	if want := []domain.Stock{{Size: "M", Quantity: 2}}; !slices.Equal(stored.Colours[1].Stock, want) {
		t.Errorf("Vinho stock %v, want %v", stored.Colours[1].Stock, want)
	}
}

// The unique index on names is checked row by row: two colours trading
// names must not trip over each other halfway.
func TestProductUpdateWithColoursSwapsNames(t *testing.T) {
	db := testDB(t)
	repo := MustNewProduct(db)
	ctx := context.Background()
	p := productWithColours(t, repo, newTestProduct(t, db))
	bege, vinho := p.Colours[0].ID, p.Colours[1].ID

	p.Colours[0].Name, p.Colours[1].Name = "Vinho", "Bege"
	updated, _, err := repo.UpdateWithColours(ctx, p, domain.ColourChanges{})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Colours[0].ID != bege || updated.Colours[0].Name != "Vinho" ||
		updated.Colours[1].ID != vinho || updated.Colours[1].Name != "Bege" {
		t.Errorf("colours %+v, want the names traded", updated.Colours)
	}
}

func TestProductColourNameConflict(t *testing.T) {
	db := testDB(t)
	repo := MustNewProduct(db)
	ctx := context.Background()

	// The domain refuses this first; the index is what holds if it ever
	// disagrees with Postgres about case.
	p := newTestProduct(t, db)
	p.Colours = []domain.Colour{newColour("Bege"), newColour("bege")}
	var conflict *domain.ConflictError
	if _, err := repo.Create(ctx, p); !errors.As(err, &conflict) || conflict.Subject != domain.ConflictColourName {
		t.Errorf("create: err = %v, want a ConflictError on the colour name", err)
	}

	p = productWithColours(t, repo, newTestProduct(t, db))
	p.Colours[1].Name = "BEGE"
	if _, _, err := repo.UpdateWithColours(ctx, p, domain.ColourChanges{}); !errors.As(err, &conflict) || conflict.Subject != domain.ConflictColourName {
		t.Errorf("update: err = %v, want a ConflictError on the colour name", err)
	}

	// Another product may use the same name.
	other := newTestProduct(t, db)
	other.Colours[0].Name = "Bege"
	if _, err := repo.Create(ctx, other); err != nil {
		t.Errorf("a colour name used by another product was refused: %v", err)
	}
}

func TestProductUpdateWithColoursStale(t *testing.T) {
	db := testDB(t)
	repo := MustNewProduct(db)
	ctx := context.Background()

	tests := []struct {
		name string
		edit func(p *domain.Product) domain.ColourChanges
	}{
		{
			// Vinho exists, but this screen never mentioned it: it was
			// added elsewhere after the page loaded.
			name: "unknown colour",
			edit: func(p *domain.Product) domain.ColourChanges {
				p.Colours = p.Colours[:1]
				return domain.ColourChanges{}
			},
		},
		{
			name: "kept colour deleted meanwhile",
			edit: func(p *domain.Product) domain.ColourChanges {
				p.Colours = append(p.Colours, newColour("Azul"))
				return domain.ColourChanges{}
			},
		},
		{
			name: "unknown photo",
			edit: func(p *domain.Product) domain.ColourChanges {
				p.Colours[0].Images = p.Colours[0].Images[1:]
				return domain.ColourChanges{}
			},
		},
		{
			name: "kept photo deleted meanwhile",
			edit: func(p *domain.Product) domain.ColourChanges {
				p.Colours[0].Images = append(p.Colours[0].Images, newImage())
				return domain.ColourChanges{}
			},
		},
		{
			// Photos belong to their colour: moving one is not something
			// any screen offers.
			name: "photo moved to another colour",
			edit: func(p *domain.Product) domain.ColourChanges {
				moved := p.Colours[0].Images[0]
				p.Colours[0].Images = p.Colours[0].Images[1:]
				p.Colours[1].Images = append(p.Colours[1].Images, moved)
				return domain.ColourChanges{}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := productWithColours(t, repo, newTestProduct(t, db))
			before := p
			p.Name = "Não deveria salvar"
			p.Colours = slices.Clone(p.Colours)
			p.Colours[0].Stock = []domain.Stock{{Size: "P", Quantity: 9}}
			changes := tt.edit(&p)

			_, _, err := repo.UpdateWithColours(ctx, p, changes)
			var stale *domain.StaleError
			if !errors.As(err, &stale) {
				t.Fatalf("err = %v, want StaleError", err)
			}

			stored, err := repo.GetByID(ctx, p.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Name == "Não deveria salvar" {
				t.Error("the fields were saved anyway")
			}
			if !slices.Equal(colourNames(stored.Colours), colourNames(before.Colours)) ||
				!slices.Equal(imageIDs(stored.Images()), imageIDs(before.Images())) ||
				!slices.Equal(stored.Colours[0].Stock, before.Colours[0].Stock) {
				t.Errorf("stored %+v, want it untouched", stored.Colours)
			}
		})
	}
}

func TestProductUpdateKeepsColours(t *testing.T) {
	db := testDB(t)
	repo := MustNewProduct(db)
	ctx := context.Background()
	p := productWithColours(t, repo, newTestProduct(t, db))
	before := p

	// The plain Update, as the sold-out toggle uses it, with a stale or
	// empty colour list: it must not touch the colours.
	p.Colours = nil
	p.SoldOut = true
	updated, err := repo.Update(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(colourNames(updated.Colours), colourNames(before.Colours)) ||
		!slices.Equal(imageIDs(updated.Images()), imageIDs(before.Images())) ||
		!slices.Equal(updated.Colours[0].Stock, before.Colours[0].Stock) {
		t.Errorf("colours %+v, want them untouched", updated.Colours)
	}
}

func TestProductSetStock(t *testing.T) {
	db := testDB(t)
	repo := MustNewProduct(db)
	ctx := context.Background()
	p := productWithColours(t, repo, newTestProduct(t, db))
	bege, vinho := p.Colours[0].ID, p.Colours[1].ID

	for _, step := range []struct {
		colour, size string
		quantity     int32
	}{
		{bege, "P", 0},  // counted already: changed
		{bege, "M", 2},  // a new size for a counted colour
		{vinho, "P", 5}, // starts counting a colour
	} {
		if err := repo.SetStock(ctx, step.colour, step.size, step.quantity); err != nil {
			t.Fatalf("SetStock(%s, %d): %v", step.size, step.quantity, err)
		}
	}

	stored, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := []domain.Stock{{Size: "P", Quantity: 0}, {Size: "M", Quantity: 2}}; !slices.Equal(stored.Colours[0].Stock, want) {
		t.Errorf("Bege stock %v, want %v", stored.Colours[0].Stock, want)
	}
	if want := []domain.Stock{{Size: "P", Quantity: 5}}; !slices.Equal(stored.Colours[1].Stock, want) {
		t.Errorf("Vinho stock %v, want %v", stored.Colours[1].Stock, want)
	}

	var notFound *domain.NotFoundError
	for _, id := range []string{uuid.New().String(), "not-a-uuid"} {
		if err := repo.SetStock(ctx, id, "P", 1); !errors.As(err, &notFound) {
			t.Errorf("SetStock(%q): err = %v, want NotFoundError", id, err)
		}
	}
}
