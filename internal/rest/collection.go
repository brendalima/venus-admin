package rest

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/alexedwards/scs/v2"

	"github.com/brendalima/venus-admin/internal/core/domain"
	"github.com/brendalima/venus-admin/internal/core/services"
)

const (
	collectionsPath = "/admin/collections"
	// maxCollectionFormBytes holds a name with room to spare.
	maxCollectionFormBytes = 4 << 10
)

// collectionListPage lists the collections, with the form to add one on
// top. Name and Error are what was typed there, when it was refused.
type collectionListPage struct {
	adminLayout
	Collections []domain.Collection
	Name        string
	Error       string
}

// collectionPage renames or deletes one collection. Name and Error are
// what was typed, when a new name was refused.
type collectionPage struct {
	adminLayout
	Collection domain.Collection
	Name       string
	Error      string
}

type CollectionInput struct {
	Service   services.CollectionService
	Sessions  *scs.SessionManager
	Templates *Templates
}

// Collection serves the collection screens. Every handler must be behind
// Auth.RequireLogin.
type Collection struct {
	service   services.CollectionService
	sessions  *scs.SessionManager
	templates *Templates
}

func NewCollection(in CollectionInput) (*Collection, error) {
	switch {
	case in.Service == nil:
		return nil, errors.New("missing collection service dependency")
	case in.Sessions == nil:
		return nil, errors.New("missing session manager dependency")
	case in.Templates == nil:
		return nil, errors.New("missing templates dependency")
	}
	return &Collection{service: in.Service, sessions: in.Sessions, templates: in.Templates}, nil
}

func MustNewCollection(in CollectionInput) *Collection {
	h, err := NewCollection(in)
	if err != nil {
		panic(fmt.Errorf("couldn't create collection handler: %w", err))
	}
	return h
}

func (h *Collection) List(w http.ResponseWriter, r *http.Request) {
	h.renderList(w, r, http.StatusOK, collectionListPage{})
}

func (h *Collection) Create(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r) {
		return
	}
	name := r.PostForm.Get("name")
	created, err := h.service.Create(r.Context(), name)
	if msg, invalid := nameError(err); invalid {
		h.renderList(w, r, http.StatusBadRequest, collectionListPage{Name: name, Error: msg})
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "couldn't create collection", "error", err)
		h.renderMessage(w, r, http.StatusInternalServerError,
			"Não consegui cadastrar a coleção", "Nada foi salvo. Tente de novo em instantes.")
		return
	}
	h.flash(r, "A coleção “"+created.Name+"” foi cadastrada. Escolha-a no formulário de cada peça.")
	seeOther(w, r, collectionsPath)
}

func (h *Collection) Edit(w http.ResponseWriter, r *http.Request) {
	c, ok := h.load(w, r)
	if !ok {
		return
	}
	h.render(w, r, http.StatusOK, "collection_form.html", collectionPage{Collection: c, Name: c.Name})
}

func (h *Collection) Update(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r) {
		return
	}
	c, ok := h.load(w, r)
	if !ok {
		return
	}
	name := r.PostForm.Get("name")
	renamed, err := h.service.Rename(r.Context(), c.ID, name)
	if msg, invalid := nameError(err); invalid {
		h.render(w, r, http.StatusBadRequest, "collection_form.html", collectionPage{Collection: c, Name: name, Error: msg})
		return
	}
	if h.notFound(w, r, err) {
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "couldn't rename collection", "error", err)
		h.renderMessage(w, r, http.StatusInternalServerError,
			"Não consegui renomear a coleção", "O nome continua o mesmo. Tente de novo em instantes.")
		return
	}
	h.flash(r, "A coleção agora se chama “"+renamed.Name+"”.")
	seeOther(w, r, collectionsPath)
}

func (h *Collection) ConfirmDelete(w http.ResponseWriter, r *http.Request) {
	c, ok := h.load(w, r)
	if !ok {
		return
	}
	h.render(w, r, http.StatusOK, "collection_delete.html", collectionPage{Collection: c})
}

func (h *Collection) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c, ok := h.load(w, r)
	if !ok {
		return
	}
	err := h.service.Delete(ctx, c.ID)
	var notFound *domain.NotFoundError
	if err != nil && !errors.As(err, &notFound) {
		slog.ErrorContext(ctx, "couldn't delete collection", "error", err)
		h.renderMessage(w, r, http.StatusInternalServerError,
			"Não consegui apagar a coleção", "Ela continua cadastrada. Tente de novo em instantes.")
		return
	}
	// Gone either way: if someone else deleted it a moment ago, the
	// outcome is the one asked for.
	slog.InfoContext(ctx, "collection deleted", "collection_id", c.ID, "pieces", c.Pieces)
	h.flash(r, "A coleção “"+c.Name+"” foi apagada.")
	seeOther(w, r, collectionsPath)
}

func (h *Collection) renderList(w http.ResponseWriter, r *http.Request, status int, page collectionListPage) {
	collections, err := h.service.List(r.Context())
	if err != nil {
		slog.ErrorContext(r.Context(), "couldn't list collections", "error", err)
		h.renderMessage(w, r, http.StatusInternalServerError,
			"Não consegui carregar as coleções", "Tente de novo em instantes.")
		return
	}
	page.adminLayout = h.layout(r)
	page.Collections = collections
	h.templates.render(w, r, status, "collections.html", page)
}

func (h *Collection) render(w http.ResponseWriter, r *http.Request, status int, name string, page collectionPage) {
	page.adminLayout = h.layout(r)
	h.templates.render(w, r, status, name, page)
}

// load fetches the collection named in the URL, or answers for the
// handler when it cannot.
func (h *Collection) load(w http.ResponseWriter, r *http.Request) (domain.Collection, bool) {
	c, err := h.service.GetByID(r.Context(), r.PathValue("id"))
	if h.notFound(w, r, err) {
		return domain.Collection{}, false
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "couldn't load collection", "error", err)
		h.renderMessage(w, r, http.StatusInternalServerError,
			"Não consegui carregar a coleção", "Tente de novo em instantes.")
		return domain.Collection{}, false
	}
	return c, true
}

// notFound answers for the handler when err says the collection is gone.
func (h *Collection) notFound(w http.ResponseWriter, r *http.Request, err error) bool {
	var notFound *domain.NotFoundError
	if !errors.As(err, &notFound) {
		return false
	}
	h.renderMessage(w, r, http.StatusNotFound,
		"Coleção não encontrada", "Talvez ela tenha sido apagada. Volte para a lista de coleções.")
	return true
}

func (h *Collection) parseForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxCollectionFormBytes)
	if err := r.ParseForm(); err != nil {
		h.renderMessage(w, r, http.StatusBadRequest,
			"Não consegui ler o formulário", "Volte, confira o que foi escrito e tente de novo.")
		return false
	}
	return true
}

func (h *Collection) renderMessage(w http.ResponseWriter, r *http.Request, status int, title, text string) {
	h.templates.render(w, r, status, "message.html", messagePage{adminLayout: h.layout(r), Title: title, Text: text})
}

func (h *Collection) layout(r *http.Request) adminLayout {
	return newAdminLayout(r, h.sessions, sectionCollections)
}

func (h *Collection) flash(r *http.Request, msg string) {
	h.sessions.Put(r.Context(), sessionFlashKey, msg)
}

// nameError reports the message for the name when err refuses it.
func nameError(err error) (string, bool) {
	var invalid *domain.ValidationError
	if errors.As(err, &invalid) {
		return invalid.Message, true
	}
	return "", false
}
