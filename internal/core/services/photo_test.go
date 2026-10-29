package services

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

type mockImageStore struct {
	files     map[string]string // content by key
	putErrAt  int               // the Nth Put fails, counting from 1; 0 never
	puts      int
	deleteErr error
}

func newMockImageStore() *mockImageStore { return &mockImageStore{files: map[string]string{}} }

func (m *mockImageStore) Put(_ context.Context, key string, data []byte, contentType string) error {
	m.puts++
	if m.puts == m.putErrAt {
		return errors.New("r2 unreachable")
	}
	m.files[key] = contentType + ":" + string(data)
	return nil
}

func (m *mockImageStore) Delete(_ context.Context, key string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.files, key)
	return nil
}

// notAPhoto is what mockProcessor refuses, as the real one refuses a
// file that is not an image.
const notAPhoto = "not a photo"

type mockProcessor struct {
	err       error
	processed int
}

func (m *mockProcessor) Process(_ context.Context, original []byte) ([]byte, []byte, error) {
	if m.err != nil {
		return nil, nil, m.err
	}
	if string(original) == notAPhoto {
		return nil, nil, domain.NewValidationError("photos", "Esse arquivo não é uma foto em JPEG, PNG ou WebP.")
	}
	m.processed++
	return []byte("small " + string(original)), []byte("large " + string(original)), nil
}

// source is an uploaded photo whose bytes are known up front.
type source string

func (s source) Bytes() ([]byte, error) { return []byte(s), nil }

type failingSource struct{}

func (failingSource) Bytes() ([]byte, error) { return nil, errors.New("temporary file gone") }

type photoFixture struct {
	service   *Product
	repo      *mockProductRepository
	store     *mockImageStore
	processor *mockProcessor
}

func newPhotoFixture(ps ...domain.Product) *photoFixture {
	f := &photoFixture{repo: newMockProductRepository(ps...), store: newMockImageStore(), processor: &mockProcessor{}}
	f.service = MustNewProduct(ProductInput{Repo: f.repo, Collections: newMockCollectionRepository(), Store: f.store, Processor: f.processor})
	return f
}

func newTestProductService(repo *mockProductRepository) *Product {
	return MustNewProduct(ProductInput{Repo: repo, Collections: newMockCollectionRepository(), Store: newMockImageStore(), Processor: &mockProcessor{}})
}

// withImages is existing with stored photos a, b and c in its colour, in
// that order.
func withImages(f *photoFixture) (domain.Image, domain.Image, domain.Image) {
	images := addImages(f, 0, "a", "b", "c")
	return images[0], images[1], images[2]
}

// addImages stores photos with the names given in the colour at index n
// of existing.
func addImages(f *photoFixture, n int, names ...string) []domain.Image {
	p := f.repo.products[existing.ID]
	c := &p.Colours[n]
	for _, name := range names {
		img := domain.Image{ID: uuid.New().String(), ColourID: c.ID, Key600: name + "-600", Key1200: name + "-1200"}
		c.Images = append(c.Images, img)
		f.store.files[img.Key600], f.store.files[img.Key1200] = "x", "x"
	}
	f.repo.products[existing.ID] = p
	return c.Images
}

// newWithPhotos is a colour edit for a new product, with one colour and
// the photo edit given.
func newWithPhotos(e *domain.PhotoEdit) domain.ColoursEdit {
	return domain.ColoursEdit{Colours: []domain.ColourEdit{{Name: "Bege", Photos: *e}}}
}

// photosOf is a colour edit for existing that keeps its one colour and
// applies e to its photos.
func photosOf(e *domain.PhotoEdit) *domain.ColoursEdit {
	c := existing.Colours[0]
	return &domain.ColoursEdit{Colours: []domain.ColourEdit{{ID: c.ID, Name: c.Name, Photos: *e}}}
}

// photoField is the field of an error about new photo i of the first
// colour.
func photoField(i int) string { return domain.ColourField(0, domain.PhotoErrorField(i)) }

func ids(images []domain.Image) []string {
	out := make([]string, len(images))
	for i, img := range images {
		out[i] = img.ID
	}
	return out
}

func TestCreateWithPhotos(t *testing.T) {
	f := newPhotoFixture()
	edit := &domain.PhotoEdit{
		Order: []domain.PhotoSlot{{NewIndex: 1}, {NewIndex: 0}},
		New:   []domain.PhotoSource{source("one"), source("two")},
	}

	p, err := f.service.Create(context.Background(), validInput(), newWithPhotos(edit))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Images()) != 2 {
		t.Fatalf("%d images, want 2", len(p.Images()))
	}
	cover := p.Images()[0]
	if f.store.files[cover.Key1200] != "image/jpeg:large two" || f.store.files[cover.Key600] != "image/jpeg:small two" {
		t.Errorf("the cover is not the second file: %q", f.store.files[cover.Key1200])
	}
	if cover.Key600 != "produtos/"+cover.ID+"-600.jpg" {
		t.Errorf("cover key %q", cover.Key600)
	}
}

func TestUpdateWithPhotos(t *testing.T) {
	f := newPhotoFixture(existing)
	a, b, c := withImages(f)
	in := existing
	in.Name = "Biquíni Aurora Novo"

	// c becomes the cover, b goes, a new photo goes in the middle.
	updated, err := f.service.Update(context.Background(), in, photosOf(&domain.PhotoEdit{
		Order:   []domain.PhotoSlot{{ImageID: c.ID}, {NewIndex: 0}, {ImageID: a.ID}},
		Removed: []string{b.ID},
		New:     []domain.PhotoSource{source("new")},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Biquíni Aurora Novo" {
		t.Errorf("name = %q", updated.Name)
	}
	if got := ids(updated.Images()); len(got) != 3 || got[0] != c.ID || got[2] != a.ID {
		t.Errorf("order %v, want [c new a]", got)
	}
	newID := updated.Images()[1].ID
	if got := f.repo.changes; !slices.Equal(got.AddedImages, []string{newID}) || !slices.Equal(got.RemovedImages, []string{b.ID}) {
		t.Errorf("changes %+v, want the new photo added and b removed", got)
	}
	if _, ok := f.store.files[b.Key600]; ok {
		t.Error("the removed photo's files are still stored")
	}
	if f.store.files[updated.Images()[1].Key1200] != "image/jpeg:large new" {
		t.Error("the new photo was not stored")
	}
}

func TestUpdateAppendsWithoutJavaScript(t *testing.T) {
	f := newPhotoFixture(existing)
	a, b, c := withImages(f)

	updated, err := f.service.Update(context.Background(), existing, photosOf(&domain.PhotoEdit{
		AppendNew: true,
		New:       []domain.PhotoSource{source("new")},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(updated.Images()); len(got) != 4 || !slices.Equal(got[:3], []string{a.ID, b.ID, c.ID}) {
		t.Errorf("order %v, want a, b, c and the new one last", got)
	}
}

func TestUpdateWithoutPhotosLeavesThem(t *testing.T) {
	f := newPhotoFixture(existing)
	a, b, c := withImages(f)

	updated, err := f.service.Update(context.Background(), existing, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(updated.Images()); !slices.Equal(got, []string{a.ID, b.ID, c.ID}) {
		t.Errorf("order %v, want them untouched", got)
	}
}

func TestPhotoEditNothingSaved(t *testing.T) {
	type edit func(a, b, c domain.Image) *domain.PhotoEdit
	keepAll := func(a, b, c domain.Image) []domain.PhotoSlot {
		return []domain.PhotoSlot{{ImageID: a.ID}, {ImageID: b.ID}, {ImageID: c.ID}}
	}
	tests := []struct {
		name          string
		input         func(domain.Product) domain.Product
		edit          edit
		putErrAt      int
		repoErr       error
		wantFields    []string // validation errors expected, by field
		wantStale     bool
		wantProcessed int // photos processed before giving up
	}{
		{
			name: "a new photo is not a photo",
			edit: func(a, b, c domain.Image) *domain.PhotoEdit {
				return &domain.PhotoEdit{
					Order: append(keepAll(a, b, c), domain.PhotoSlot{NewIndex: 0}, domain.PhotoSlot{NewIndex: 1}),
					New:   []domain.PhotoSource{source("good"), source(notAPhoto)},
				}
			},
			wantFields: []string{photoField(1)}, wantProcessed: 1,
		},
		{
			// Both problems at once: a single save says everything to fix.
			name:  "bad name and bad photo",
			input: func(p domain.Product) domain.Product { p.Name = ""; return p },
			edit: func(a, b, c domain.Image) *domain.PhotoEdit {
				return &domain.PhotoEdit{
					Order: append(keepAll(a, b, c), domain.PhotoSlot{NewIndex: 0}),
					New:   []domain.PhotoSource{source(notAPhoto)},
				}
			},
			wantFields: []string{"name", photoField(0)},
		},
		{
			name: "a photo added elsewhere",
			edit: func(a, _, c domain.Image) *domain.PhotoEdit {
				return &domain.PhotoEdit{Order: []domain.PhotoSlot{{ImageID: a.ID}, {ImageID: c.ID}}}
			},
			wantStale: true,
		},
		{
			name: "a photo deleted elsewhere",
			edit: func(a, b, c domain.Image) *domain.PhotoEdit {
				return &domain.PhotoEdit{Order: append(keepAll(a, b, c), domain.PhotoSlot{ImageID: uuid.New().String()})}
			},
			wantStale: true,
		},
		{
			name: "too many photos",
			edit: func(a, b, c domain.Image) *domain.PhotoEdit {
				e := &domain.PhotoEdit{Order: keepAll(a, b, c)}
				for i := range domain.MaxImagesPerColour {
					e.New = append(e.New, source("p"))
					e.Order = append(e.Order, domain.PhotoSlot{NewIndex: i})
				}
				return e
			},
			wantFields: []string{domain.ColourField(0, "photos")},
		},
		{
			name: "storage fails midway",
			edit: func(a, b, c domain.Image) *domain.PhotoEdit {
				return &domain.PhotoEdit{
					Order: append(keepAll(a, b, c), domain.PhotoSlot{NewIndex: 0}, domain.PhotoSlot{NewIndex: 1}),
					New:   []domain.PhotoSource{source("one"), source("two")},
				}
			},
			putErrAt: 3, wantProcessed: 2,
		},
		{
			name: "database fails",
			edit: func(a, b, c domain.Image) *domain.PhotoEdit {
				return &domain.PhotoEdit{Order: append(keepAll(a, b, c), domain.PhotoSlot{NewIndex: 0}), New: []domain.PhotoSource{source("one")}}
			},
			repoErr: errors.New("connection refused"), wantProcessed: 1,
		},
		{
			name: "upload unreadable",
			edit: func(a, b, c domain.Image) *domain.PhotoEdit {
				return &domain.PhotoEdit{Order: append(keepAll(a, b, c), domain.PhotoSlot{NewIndex: 0}), New: []domain.PhotoSource{failingSource{}}}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newPhotoFixture(existing)
			a, b, c := withImages(f)
			f.store.putErrAt, f.repo.writeErr = tt.putErrAt, tt.repoErr
			files := len(f.store.files)
			in := existing
			if tt.input != nil {
				in = tt.input(in)
			}

			_, err := f.service.Update(context.Background(), in, photosOf(tt.edit(a, b, c)))
			if err == nil {
				t.Fatal("expected an error")
			}

			got := map[string]bool{}
			var walk func(error)
			walk = func(err error) {
				var joined interface{ Unwrap() []error }
				if errors.As(err, &joined) {
					for _, e := range joined.Unwrap() {
						walk(e)
					}
					return
				}
				var v *domain.ValidationError
				if errors.As(err, &v) {
					got[v.Field] = true
				}
			}
			walk(err)
			for _, field := range tt.wantFields {
				if !got[field] {
					t.Errorf("no validation error on %s in %v", field, err)
				}
			}
			var stale *domain.StaleError
			if tt.wantStale != errors.As(err, &stale) {
				t.Errorf("err = %v, want StaleError: %v", err, tt.wantStale)
			}
			if f.processor.processed != tt.wantProcessed {
				t.Errorf("%d photos processed, want %d", f.processor.processed, tt.wantProcessed)
			}

			// Nothing saved: the product, its photos and the bucket are as
			// they were.
			if f.repo.writes != 0 && tt.repoErr == nil {
				t.Errorf("%d writes reached the repository", f.repo.writes)
			}
			if got := ids(f.repo.products[existing.ID].Images()); !slices.Equal(got, []string{a.ID, b.ID, c.ID}) {
				t.Errorf("photos %v, want them untouched", got)
			}
			if len(f.store.files) != files {
				t.Errorf("%d files stored, want the original %d", len(f.store.files), files)
			}
		})
	}
}

func TestProductDeleteRemovesPhotoFiles(t *testing.T) {
	f := newPhotoFixture(existing)
	withImages(f)
	if err := f.service.Delete(context.Background(), existing.ID); err != nil {
		t.Fatal(err)
	}
	if len(f.store.files) != 0 {
		t.Errorf("files left behind: %v", f.store.files)
	}
}

func TestRemovedPhotoFilesFailingToDeleteDoNotFailTheSave(t *testing.T) {
	f := newPhotoFixture(existing)
	a, b, c := withImages(f)
	f.store.deleteErr = errors.New("r2 unreachable")

	updated, err := f.service.Update(context.Background(), existing, photosOf(&domain.PhotoEdit{
		Order:   []domain.PhotoSlot{{ImageID: a.ID}, {ImageID: c.ID}},
		Removed: []string{b.ID},
	}))
	if err != nil {
		t.Fatalf("the save failed because of an orphan file: %v", err)
	}
	if got := ids(updated.Images()); !slices.Equal(got, []string{a.ID, c.ID}) {
		t.Errorf("order %v, want [a c]", got)
	}
}
