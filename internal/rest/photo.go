package rest

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

const (
	// maxPhotoBytes is per file. Phone photos are 3 to 8 MB; a PNG
	// screenshot can be more.
	maxPhotoBytes = 15 << 20

	// maxPhotoUploadBytes allows a whole product's worth of photos in one
	// go. Files past the first megabyte are spooled to disk while the
	// form is parsed, not held in memory.
	maxPhotoUploadBytes = domain.MaxImages*maxPhotoBytes + 1<<20

	// photoUploadTimeout replaces the server's 30-second read timeout for
	// uploads only: several photos over a phone connection take longer.
	photoUploadTimeout = 10 * time.Minute
)

type photoDeletePage struct {
	adminLayout
	Product domain.Product
	Photo   domain.Image
	Number  int // 1 for the cover
}

func photosURL(productID string) string { return "/admin/products/" + productID + "#fotos" }

// UploadPhotos adds every photo in the form, one at a time, and reports
// what could not be added without losing what could.
func (h *Product) UploadPhotos(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	rc := http.NewResponseController(w)
	deadline := time.Now().Add(photoUploadTimeout)
	if err := rc.SetReadDeadline(deadline); err != nil {
		slog.WarnContext(ctx, "couldn't extend the upload's read deadline", "error", err)
	}
	if err := rc.SetWriteDeadline(deadline); err != nil {
		slog.WarnContext(ctx, "couldn't extend the upload's write deadline", "error", err)
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxPhotoUploadBytes)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			h.flash(r, sessionPhotoFlashErrorKey, fmt.Sprintf(
				"As fotos passaram do limite de uma vez só. Envie menos fotos por vez, cada uma com até %d MB.", maxPhotoBytes>>20))
		} else {
			h.flash(r, sessionPhotoFlashErrorKey, "Não consegui receber as fotos. Tente de novo.")
		}
		seeOther(w, r, photosURL(id))
		return
	}
	defer func() {
		if err := r.MultipartForm.RemoveAll(); err != nil {
			slog.WarnContext(ctx, "couldn't remove uploaded temporary files", "error", err)
		}
	}()

	files := r.MultipartForm.File["photos"]
	if len(files) == 0 {
		h.flash(r, sessionPhotoFlashErrorKey, "Escolha pelo menos uma foto para enviar.")
		seeOther(w, r, photosURL(id))
		return
	}

	added := 0
	var problems problemList
	for _, fh := range files {
		name := shortName(fh.Filename)
		if fh.Size > maxPhotoBytes {
			problems.add(fmt.Sprintf("passa de %d MB", maxPhotoBytes>>20), name)
			continue
		}
		data, err := readUpload(fh)
		if err != nil {
			slog.ErrorContext(ctx, "couldn't read uploaded photo", "error", err)
			problems.add("não consegui ler o arquivo", name)
			continue
		}

		_, err = h.service.AddPhoto(ctx, id, data)
		var invalid *domain.ValidationError
		var notFound *domain.NotFoundError
		switch {
		case err == nil:
			added++
		case errors.As(err, &invalid):
			problems.add(strings.TrimSuffix(invalid.Message, "."), name)
		case errors.As(err, &notFound):
			h.renderMessage(w, r, http.StatusNotFound,
				"Peça não encontrada", "Talvez alguém a tenha apagado enquanto você enviava as fotos.")
			return
		default:
			slog.ErrorContext(ctx, "couldn't add photo", "product_id", id, "error", err)
			problems.add("não consegui salvar agora, tente de novo em instantes", name)
		}
	}

	switch added {
	case 0:
	case 1:
		h.flash(r, sessionPhotoFlashKey, "1 foto adicionada.")
	default:
		h.flash(r, sessionPhotoFlashKey, strconv.Itoa(added)+" fotos adicionadas.")
	}
	if len(problems) > 0 {
		h.flash(r, sessionPhotoFlashErrorKey, problems.String())
	}
	seeOther(w, r, photosURL(id))
}

// MovePhoto takes the index wanted rather than "up" or "down", so a double
// tap asks for the same place twice instead of moving the photo twice.
func (h *Product) MovePhoto(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r) {
		return
	}
	ctx := r.Context()
	id := r.PathValue("id")

	index, err := strconv.Atoi(r.PostForm.Get("to"))
	if err != nil {
		h.flash(r, sessionPhotoFlashErrorKey, "Não consegui ler o pedido. Tente de novo.")
		seeOther(w, r, photosURL(id))
		return
	}

	err = h.service.MovePhoto(ctx, id, r.PathValue("photo"), index)
	var notFound *domain.NotFoundError
	switch {
	case errors.As(err, &notFound):
		h.flash(r, sessionPhotoFlashErrorKey, "Essa foto não existe mais. Talvez alguém a tenha apagado.")
	case err != nil:
		slog.ErrorContext(ctx, "couldn't move photo", "error", err)
		h.flash(r, sessionPhotoFlashErrorKey, "Não consegui mudar a ordem agora. Tente de novo em instantes.")
	case index == 0:
		h.flash(r, sessionPhotoFlashKey, "Pronto, essa é a nova capa.")
	}
	seeOther(w, r, photosURL(id))
}

func (h *Product) ConfirmDeletePhoto(w http.ResponseWriter, r *http.Request) {
	p, ok := h.load(w, r)
	if !ok {
		return
	}
	photoID := r.PathValue("photo")
	for i, img := range p.Images {
		if img.ID == photoID {
			h.templates.render(w, r, http.StatusOK, "photo_delete.html", photoDeletePage{
				adminLayout: h.layout(r),
				Product:     p,
				Photo:       img,
				Number:      i + 1,
			})
			return
		}
	}
	h.renderMessage(w, r, http.StatusNotFound,
		"Foto não encontrada", "Talvez ela já tenha sido apagada. Volte para a peça.")
}

func (h *Product) DeletePhoto(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	err := h.service.DeletePhoto(ctx, id, r.PathValue("photo"))
	var notFound *domain.NotFoundError
	if err != nil && !errors.As(err, &notFound) {
		slog.ErrorContext(ctx, "couldn't delete photo", "error", err)
		h.flash(r, sessionPhotoFlashErrorKey, "Não consegui apagar a foto agora. Ela continua na peça; tente de novo em instantes.")
		seeOther(w, r, photosURL(id))
		return
	}
	// Gone either way, as with products.
	h.flash(r, sessionPhotoFlashKey, "Foto apagada.")
	seeOther(w, r, photosURL(id))
}

func readUpload(fh *multipart.FileHeader) ([]byte, error) {
	f, err := fh.Open()
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

// problemList groups the files that failed by what went wrong, so ten
// photos over the limit read as one sentence, not ten.
type problemList []problem

type problem struct {
	reason string
	names  []string
}

func (l *problemList) add(reason, name string) {
	for i := range *l {
		if (*l)[i].reason == reason {
			(*l)[i].names = append((*l)[i].names, name)
			return
		}
	}
	*l = append(*l, problem{reason: reason, names: []string{name}})
}

func (l problemList) String() string {
	parts := make([]string, len(l))
	count := 0
	for i, p := range l {
		parts[i] = strings.Join(p.names, ", ") + ": " + p.reason + "."
		count += len(p.names)
	}
	lead := "Não deu para adicionar estas fotos"
	if count == 1 {
		lead = "Não deu para adicionar esta foto"
	}
	return lead + " — " + strings.Join(parts, " ")
}
