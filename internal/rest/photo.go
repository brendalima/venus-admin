package rest

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

const (
	// maxPhotoBytes is per file. Phone photos are 3 to 8 MB; a PNG
	// screenshot can be more. static/photos.js checks the same limit
	// before sending.
	maxPhotoBytes = 15 << 20

	// maxProductUploadBytes allows as many new photos as one save may
	// carry, with the form. Files past the first megabyte are spooled to
	// disk while the form is parsed, not held in memory.
	maxProductUploadBytes = domain.MaxNewPhotosPerSave*maxPhotoBytes + maxProductFormBytes

	// productUploadTimeout replaces the server's 30-second read timeout
	// when a product is saved: its photos go with it, and several over a
	// phone connection take longer.
	productUploadTimeout = 10 * time.Minute
)

// The fields photos.js adds to the product form. photo_order lists the
// final order, cover first, as "id:<image id>" for a photo the product has
// and "new:<n>" for the nth file in "photos"; photo_remove names each
// photo to delete. A form without photo_order was sent without
// JavaScript: its files, if any, go after the existing photos.
const (
	photoOrderField  = "photo_order"
	photoRemoveField = "photo_remove"
	photoFilesField  = "photos"
)

// uploadedPhoto reads its file only when the service gets to it, one at a
// time.
type uploadedPhoto struct{ fh *multipart.FileHeader }

func (u uploadedPhoto) Bytes() ([]byte, error) {
	f, err := u.fh.Open()
	if err != nil {
		return nil, fmt.Errorf("couldn't open upload: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxPhotoBytes+1))
	if err != nil {
		return nil, fmt.Errorf("couldn't read upload: %w", err)
	}
	return data, nil
}

// productSubmission is a product form as posted: the fields, and the photo
// edit with the names of its new files, for messages.
type productSubmission struct {
	form      productForm
	photos    *domain.PhotoEdit // nil when the photos are left as they are
	fileNames []string          // of photos.New, in order
}

// readProductSubmission parses the product form, which is multipart when
// it carries photos. When it cannot be used, it answers for the handler
// and returns false.
func (h *Product) readProductSubmission(w http.ResponseWriter, r *http.Request, page productFormPage) (productSubmission, bool) {
	ctx := r.Context()

	rc := http.NewResponseController(w)
	deadline := time.Now().Add(productUploadTimeout)
	if err := rc.SetReadDeadline(deadline); err != nil {
		slog.WarnContext(ctx, "couldn't extend the save's read deadline", "error", err)
	}
	if err := rc.SetWriteDeadline(deadline); err != nil {
		slog.WarnContext(ctx, "couldn't extend the save's write deadline", "error", err)
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxProductUploadBytes)
	var err error
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		err = r.ParseMultipartForm(1 << 20)
	} else {
		err = r.ParseForm()
	}
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			page.Alert = fmt.Sprintf("As fotos passaram do limite de uma vez só. Envie menos fotos, cada uma com até %d MB. Nada foi salvo.", maxPhotoBytes>>20)
		} else {
			page.Alert = "Não consegui ler o formulário. Tente de novo; nada foi salvo."
		}
		h.renderForm(w, r, http.StatusBadRequest, page)
		return productSubmission{}, false
	}

	sub := productSubmission{form: readProductForm(r)}
	page.Form = sub.form
	var files []*multipart.FileHeader
	if r.MultipartForm != nil {
		files = r.MultipartForm.File[photoFilesField]
	}
	_, withJS := r.PostForm[photoOrderField]
	if !withJS && len(files) == 0 {
		return sub, true
	}

	edit := &domain.PhotoEdit{AppendNew: !withJS}
	var tooBig []string
	for _, fh := range files {
		sub.fileNames = append(sub.fileNames, shortName(fh.Filename))
		if fh.Size > maxPhotoBytes {
			tooBig = append(tooBig, shortName(fh.Filename))
		}
		edit.New = append(edit.New, uploadedPhoto{fh: fh})
	}
	if len(tooBig) > 0 {
		page.PhotoErrors = []string{fmt.Sprintf("%s: passa de %d MB.", strings.Join(tooBig, ", "), maxPhotoBytes>>20)}
		page.Alert = "Nada foi salvo. Confira as fotos marcadas abaixo."
		h.renderForm(w, r, http.StatusBadRequest, page)
		return productSubmission{}, false
	}

	if withJS {
		order, ok := parsePhotoOrder(r.PostForm.Get(photoOrderField))
		if !ok {
			page.Alert = "Não consegui entender a lista de fotos. Recarregue a página e tente de novo; nada foi salvo."
			h.renderForm(w, r, http.StatusBadRequest, page)
			return productSubmission{}, false
		}
		edit.Order = order
		edit.Removed = r.PostForm[photoRemoveField]
	}
	sub.photos = edit
	return sub, true
}

// parsePhotoOrder reads photo_order. An empty value is a valid, empty list.
func parsePhotoOrder(v string) ([]domain.PhotoSlot, bool) {
	slots := []domain.PhotoSlot{}
	if strings.TrimSpace(v) == "" {
		return slots, true
	}
	for _, token := range strings.Split(v, ",") {
		kind, ref, ok := strings.Cut(strings.TrimSpace(token), ":")
		switch {
		case !ok || ref == "":
			return nil, false
		case kind == "id":
			slots = append(slots, domain.PhotoSlot{ImageID: ref})
		case kind == "new":
			n, err := strconv.Atoi(ref)
			if err != nil || n < 0 {
				return nil, false
			}
			slots = append(slots, domain.PhotoSlot{NewIndex: n})
		default:
			return nil, false
		}
	}
	return slots, true
}

// takePhotoErrors moves the photo errors out of errs, which fieldErrors
// collected, into lines that name the file. The general ones, such as the
// photo limit, keep their message as it is.
//
// Until the form shows colours, the errors about them, which the form has
// no place for, are listed here too: with one colour on screen, they can
// only be about its photos or the colour list as a whole.
func takePhotoErrors(errs map[string]string, fileNames []string) []string {
	var lines []string
	for _, field := range []string{"photos", domain.ColourField(0, "photos")} {
		if msg, ok := errs[field]; ok {
			lines = append(lines, msg)
			delete(errs, field)
		}
	}
	for i, name := range fileNames {
		field := domain.ColourField(0, domain.PhotoErrorField(i))
		if msg, ok := errs[field]; ok {
			lines = append(lines, name+": "+msg)
			delete(errs, field)
		}
	}
	var colourFields []string
	for field := range errs {
		if field == "colours" || strings.HasPrefix(field, "colours.") {
			colourFields = append(colourFields, field)
		}
	}
	slices.Sort(colourFields)
	for _, field := range colourFields {
		lines = append(lines, errs[field])
		delete(errs, field)
	}
	return lines
}

// defaultColourName is the colour a product gets while the form does not
// show colours yet: the same name migration 00008 gave every product that
// existed before them.
const defaultColourName = "Única"

// formPhotos are the photos the form shows: the first colour's, the one
// it edits until it shows colours.
func formPhotos(p domain.Product) []domain.Image {
	if len(p.Colours) == 0 {
		return []domain.Image{}
	}
	return p.Colours[0].Images
}

// newProductColours is the colour edit of a product created from the
// form: one colour, with the photos the form sent.
func newProductColours(photos *domain.PhotoEdit) domain.ColoursEdit {
	c := domain.ColourEdit{Name: defaultColourName}
	if photos != nil {
		c.Photos = *photos
	}
	return domain.ColoursEdit{Colours: []domain.ColourEdit{c}}
}

// editedColours is the colour edit of a product saved from the form: the
// photo edit applies to the first colour, and every colour keeps its
// name and stock as stored, since the form does not show them yet. A
// product without colours gets one, so its photos have somewhere to go.
func editedColours(current domain.Product, photos domain.PhotoEdit) *domain.ColoursEdit {
	if len(current.Colours) == 0 {
		e := newProductColours(&photos)
		return &e
	}
	e := &domain.ColoursEdit{}
	for i, c := range current.Colours {
		ce := domain.ColourEdit{ID: c.ID, Name: c.Name, Stock: c.Stock, Photos: domain.PhotoEdit{AppendNew: true}}
		if i == 0 {
			ce.Photos = photos
		}
		e.Colours = append(e.Colours, ce)
	}
	return e
}

// shortName keeps a file name readable in a message: phones name photos
// IMG_1234.JPG, but a name can be anything.
func shortName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "sem nome"
	}
	if utf8.RuneCountInString(name) > 40 {
		return string([]rune(name)[:39]) + "…"
	}
	return name
}
