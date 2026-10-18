package services

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

type mockImageRepository struct {
	repo    *mockProductRepository // images live on its products
	addErr  error
	moveErr error
	moved   []int
}

func (m *mockImageRepository) Add(_ context.Context, img domain.Image) (domain.Image, error) {
	if m.addErr != nil {
		return domain.Image{}, m.addErr
	}
	p, ok := m.repo.products[img.ProductID]
	if !ok {
		return domain.Image{}, domain.NewNotFoundError("product not found")
	}
	img.Position = int32(len(p.Images))
	p.Images = append(p.Images, img)
	m.repo.products[p.ID] = p
	return img, nil
}

func (m *mockImageRepository) Delete(_ context.Context, productID, imageID string) (domain.Image, error) {
	p, ok := m.repo.products[productID]
	if !ok {
		return domain.Image{}, domain.NewNotFoundError("image not found")
	}
	for i, img := range p.Images {
		if img.ID == imageID {
			p.Images = slices.Delete(p.Images, i, i+1)
			m.repo.products[p.ID] = p
			return img, nil
		}
	}
	return domain.Image{}, domain.NewNotFoundError("image not found")
}

func (m *mockImageRepository) Move(_ context.Context, _, _ string, index int) error {
	if m.moveErr != nil {
		return m.moveErr
	}
	m.moved = append(m.moved, index)
	return nil
}

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

type mockProcessor struct{ err error }

func (m *mockProcessor) Process(_ context.Context, original []byte) ([]byte, []byte, error) {
	if m.err != nil {
		return nil, nil, m.err
	}
	return []byte("small " + string(original)), []byte("large " + string(original)), nil
}

type photoFixture struct {
	service   *Product
	repo      *mockProductRepository
	images    *mockImageRepository
	store     *mockImageStore
	processor *mockProcessor
}

func newPhotoFixture(ps ...domain.Product) *photoFixture {
	repo := newMockProductRepository(ps...)
	f := &photoFixture{
		repo:      repo,
		images:    &mockImageRepository{repo: repo},
		store:     newMockImageStore(),
		processor: &mockProcessor{},
	}
	f.service = MustNewProduct(ProductInput{Repo: repo, Images: f.images, Store: f.store, Processor: f.processor})
	return f
}

func newTestProductService(repo *mockProductRepository) *Product {
	return MustNewProduct(ProductInput{
		Repo: repo, Images: &mockImageRepository{repo: repo}, Store: newMockImageStore(), Processor: &mockProcessor{},
	})
}

func TestAddPhoto(t *testing.T) {
	f := newPhotoFixture(existing)

	img, err := f.service.AddPhoto(context.Background(), existing.ID, []byte("photo"))
	if err != nil {
		t.Fatal(err)
	}
	if uuid.Validate(img.ID) != nil || img.ProductID != existing.ID {
		t.Errorf("image = %+v", img)
	}
	want := map[string]string{
		"produtos/" + img.ID + "-600.jpg":  "image/jpeg:small photo",
		"produtos/" + img.ID + "-1200.jpg": "image/jpeg:large photo",
	}
	if img.Key600 != "produtos/"+img.ID+"-600.jpg" || img.Key1200 != "produtos/"+img.ID+"-1200.jpg" {
		t.Errorf("keys %q and %q", img.Key600, img.Key1200)
	}
	for k, v := range want {
		if f.store.files[k] != v {
			t.Errorf("stored %q = %q, want %q", k, f.store.files[k], v)
		}
	}
}

func TestAddPhotoFailures(t *testing.T) {
	full := existing
	for range domain.MaxImages {
		full.Images = append(full.Images, domain.Image{ID: uuid.New().String()})
	}

	tests := []struct {
		name         string
		product      domain.Product
		productID    string
		processErr   error
		putErrAt     int
		addErr       error
		wantField    string
		wantNotFound bool
	}{
		{name: "unknown product", product: existing, productID: uuid.New().String(), wantNotFound: true},
		{name: "photo limit", product: full, productID: existing.ID, wantField: "photos"},
		{
			name: "not a photo", product: existing, productID: existing.ID,
			processErr: domain.NewValidationError("photos", "Esse arquivo não é uma foto"), wantField: "photos",
		},
		{name: "first upload fails", product: existing, productID: existing.ID, putErrAt: 1},
		{name: "second upload fails", product: existing, productID: existing.ID, putErrAt: 2},
		{name: "database fails", product: existing, productID: existing.ID, addErr: errors.New("connection refused")},
		{
			name: "product deleted meanwhile", product: existing, productID: existing.ID,
			addErr: domain.NewNotFoundError("product not found"), wantNotFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newPhotoFixture(tt.product)
			f.processor.err, f.store.putErrAt, f.images.addErr = tt.processErr, tt.putErrAt, tt.addErr

			_, err := f.service.AddPhoto(context.Background(), tt.productID, []byte("photo"))
			if err == nil {
				t.Fatal("expected an error")
			}
			var v *domain.ValidationError
			if tt.wantField != "" && (!errors.As(err, &v) || v.Field != tt.wantField) {
				t.Errorf("err = %v, want a ValidationError on %s", err, tt.wantField)
			}
			var nf *domain.NotFoundError
			if tt.wantNotFound != errors.As(err, &nf) {
				t.Errorf("err = %v, want NotFoundError: %v", err, tt.wantNotFound)
			}
			if len(f.store.files) != 0 {
				t.Errorf("files left behind: %v", f.store.files)
			}
		})
	}
}

func TestDeletePhoto(t *testing.T) {
	f := newPhotoFixture(existing)
	img, err := f.service.AddPhoto(context.Background(), existing.ID, []byte("photo"))
	if err != nil {
		t.Fatal(err)
	}

	// A storage failure does not undo the deletion the person asked for.
	f.store.deleteErr = errors.New("r2 unreachable")
	if err := f.service.DeletePhoto(context.Background(), existing.ID, img.ID); err != nil {
		t.Fatalf("delete with storage down: %v", err)
	}
	if len(f.repo.products[existing.ID].Images) != 0 {
		t.Error("the photo is still on the product")
	}

	var nf *domain.NotFoundError
	if err := f.service.DeletePhoto(context.Background(), existing.ID, img.ID); !errors.As(err, &nf) {
		t.Errorf("deleting twice: err = %v, want NotFoundError", err)
	}
}

func TestDeletePhotoRemovesFiles(t *testing.T) {
	f := newPhotoFixture(existing)
	img, err := f.service.AddPhoto(context.Background(), existing.ID, []byte("photo"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.service.DeletePhoto(context.Background(), existing.ID, img.ID); err != nil {
		t.Fatal(err)
	}
	if len(f.store.files) != 0 {
		t.Errorf("files left behind: %v", f.store.files)
	}
}

func TestProductDeleteRemovesPhotoFiles(t *testing.T) {
	f := newPhotoFixture(existing)
	for range 2 {
		if _, err := f.service.AddPhoto(context.Background(), existing.ID, []byte("photo")); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.store.files) != 4 {
		t.Fatalf("%d files stored, want 4", len(f.store.files))
	}

	if err := f.service.Delete(context.Background(), existing.ID); err != nil {
		t.Fatal(err)
	}
	if len(f.store.files) != 0 {
		t.Errorf("files left behind: %v", f.store.files)
	}
}

func TestMovePhoto(t *testing.T) {
	f := newPhotoFixture(existing)
	if err := f.service.MovePhoto(context.Background(), existing.ID, uuid.New().String(), 0); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.images.moved, []int{0}) {
		t.Errorf("moved to %v, want [0]", f.images.moved)
	}

	f.images.moveErr = domain.NewNotFoundError("image not found")
	var nf *domain.NotFoundError
	err := f.service.MovePhoto(context.Background(), existing.ID, "x", 1)
	if !errors.As(err, &nf) || !strings.Contains(err.Error(), "couldn't move photo") {
		t.Errorf("err = %v, want a wrapped NotFoundError", err)
	}
}
