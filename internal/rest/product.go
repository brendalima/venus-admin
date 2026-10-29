package rest

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/alexedwards/scs/v2"

	"github.com/brendalima/venus-admin/internal/core/domain"
	"github.com/brendalima/venus-admin/internal/core/services"
)

// maxProductFormBytes leaves room for a long description and list of
// details, and nothing near what would cost memory to parse.
const maxProductFormBytes = 64 << 10

// archivedListPath is where archived pieces are listed, and the only
// place that leads to deleting one for good.
const archivedListPath = "/admin/arquivadas"

const (
	sessionFlashKey      = "flash"
	sessionFlashErrorKey = "flash_error"
)

// adminLayout is what every logged-in page shows around its content: who
// is logged in, and the outcome of the last action.
type adminLayout struct {
	Email      string
	Flash      string
	FlashError string
	// Section is the part of the admin the page is in, which the top
	// navigation marks: sectionProducts or sectionCollections.
	Section string
}

const (
	sectionProducts    = "products"
	sectionCollections = "collections"
)

// newAdminLayout takes the flash messages out of the session: shown
// once, then gone.
func newAdminLayout(r *http.Request, sessions *scs.SessionManager, section string) adminLayout {
	ctx := r.Context()
	user, _ := currentUser(ctx)
	return adminLayout{
		Email:      user.Email,
		Flash:      sessions.PopString(ctx, sessionFlashKey),
		FlashError: sessions.PopString(ctx, sessionFlashErrorKey),
		Section:    section,
	}
}

type productListPage struct {
	adminLayout
	Products []domain.Product
	Site     domain.SiteStatus
	// SiteUnknown is set when the site's status could not be read. The
	// list still shows, and so does the button.
	SiteUnknown bool
	// Archived counts the archived pieces, for the link to them: without
	// the number nobody remembers the screen exists. ArchivedUnknown
	// hides it when it could not be counted.
	Archived        int
	ArchivedUnknown bool
}

type archivedListPage struct {
	adminLayout
	Products []domain.Product
}

type productFormPage struct {
	adminLayout
	ID       string // empty when creating
	Archived bool   // out of the collection; the form offers to bring it back
	Form     productForm
	Errors   map[string]string // by field
	Alert    string            // a problem that belongs to no field
	Sizes    []string          // every size on offer

	// Collections are the ones a piece can be put in.
	Collections        []domain.Collection
	CollectionsUnknown bool

	// Colours are the blocks of the colour section, each with its photos.
	// ColourErrors are about the colours as a whole, such as their number.
	Colours      []colourBlock
	ColourErrors []string

	// current is the product being edited, as stored, when the handler
	// has it already; renderForm loads it otherwise.
	current *domain.Product
}

// productConfirmPage asks before archiving or deleting a product.
type productConfirmPage struct {
	adminLayout
	Product domain.Product
}

type messagePage struct {
	adminLayout
	Title string
	Text  string
}

// productForm is the form as typed, kept as text so a rejected form comes
// back exactly as the person left it.
type productForm struct {
	Name         string
	Price        string
	Sizes        []string
	Description  string
	CollectionID string
	Details      string // one per line
	Slug         string
	SoldOut      bool
}

func (f productForm) HasSize(size string) bool { return slices.Contains(f.Sizes, size) }

func readProductForm(r *http.Request) productForm {
	return productForm{
		Name:         r.PostForm.Get("name"),
		Price:        r.PostForm.Get("price"),
		Sizes:        r.PostForm["sizes"],
		Description:  r.PostForm.Get("description"),
		CollectionID: r.PostForm.Get("collection_id"),
		Details:      r.PostForm.Get("details"),
		Slug:         r.PostForm.Get("slug"),
		SoldOut:      r.PostForm.Get("sold_out") != "",
	}
}

func productFormFrom(p domain.Product) productForm {
	price := ""
	if p.HasPrice() {
		price = formatReais(p.PriceCents)
	}
	return productForm{
		Name:         p.Name,
		Price:        price,
		Sizes:        p.Sizes,
		Description:  p.Description,
		CollectionID: p.CollectionID,
		Details:      strings.Join(p.Details, "\n"),
		Slug:         p.Slug,
		SoldOut:      p.SoldOut,
	}
}

// product converts the form to the domain. A price that cannot be read
// comes back as an error, and must not be saved: left at zero, it would
// pass for "no price yet".
func (f productForm) product() (domain.Product, error) {
	cents, err := parseReais(f.Price)
	return domain.Product{
		Name:         f.Name,
		PriceCents:   cents,
		Sizes:        f.Sizes,
		Description:  f.Description,
		CollectionID: f.CollectionID,
		Details:      strings.Split(strings.ReplaceAll(f.Details, "\r\n", "\n"), "\n"),
		Slug:         f.Slug,
		SoldOut:      f.SoldOut,
	}, err
}

type ProductInput struct {
	Service     services.ProductService
	Collections services.CollectionService
	Site        services.SiteService
	Sessions    *scs.SessionManager
	Templates   *Templates
}

// Product serves the catalogue screens. Every handler must be behind
// Auth.RequireLogin.
type Product struct {
	service     services.ProductService
	collections services.CollectionService
	site        services.SiteService
	sessions    *scs.SessionManager
	templates   *Templates
}

func NewProduct(in ProductInput) (*Product, error) {
	switch {
	case in.Service == nil:
		return nil, errors.New("missing product service dependency")
	case in.Collections == nil:
		return nil, errors.New("missing collection service dependency")
	case in.Site == nil:
		return nil, errors.New("missing site service dependency")
	case in.Sessions == nil:
		return nil, errors.New("missing session manager dependency")
	case in.Templates == nil:
		return nil, errors.New("missing templates dependency")
	}
	return &Product{
		service: in.Service, collections: in.Collections, site: in.Site, sessions: in.Sessions, templates: in.Templates,
	}, nil
}

func MustNewProduct(in ProductInput) *Product {
	h, err := NewProduct(in)
	if err != nil {
		panic(fmt.Errorf("couldn't create product handler: %w", err))
	}
	return h
}

func (h *Product) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	products, err := h.service.List(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "couldn't list products", "error", err)
		h.renderMessage(w, r, http.StatusInternalServerError,
			"Não consegui carregar as peças", "Tente de novo em instantes.")
		return
	}
	page := productListPage{adminLayout: h.layout(r), Products: products}
	if page.Site, err = h.site.Status(ctx); err != nil {
		slog.ErrorContext(ctx, "couldn't read site status", "error", err)
		page.SiteUnknown = true
		page.Site.CanPublish = true
	}
	if page.Archived, err = h.service.CountArchived(ctx); err != nil {
		slog.ErrorContext(ctx, "couldn't count archived products", "error", err)
		page.ArchivedUnknown = true
	}
	h.templates.render(w, r, http.StatusOK, "products.html", page)
}

func (h *Product) ListArchived(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	products, err := h.service.ListArchived(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "couldn't list archived products", "error", err)
		h.renderMessage(w, r, http.StatusInternalServerError,
			"Não consegui carregar as peças arquivadas", "Tente de novo em instantes.")
		return
	}
	h.templates.render(w, r, http.StatusOK, "products_archived.html", archivedListPage{
		adminLayout: h.layout(r),
		Products:    products,
	})
}

func (h *Product) New(w http.ResponseWriter, r *http.Request) {
	h.renderForm(w, r, http.StatusOK, productFormPage{})
}

func (h *Product) Create(w http.ResponseWriter, r *http.Request) {
	sub, ok := h.readProductSubmission(w, r, productFormPage{})
	if !ok {
		return
	}
	page := productFormPage{Form: sub.form}
	p, priceErr := sub.form.product()
	if priceErr != nil || sub.unreadable() {
		h.unreadable(w, r, page, sub, p, priceErr)
		return
	}

	var colours domain.ColoursEdit
	if sub.colours != nil {
		colours = sub.colours.edit
	}
	created, err := h.service.Create(r.Context(), p, colours)
	if err != nil {
		h.saveFailed(w, r, page, sub, err)
		return
	}
	h.flash(r, sessionFlashKey, "“"+created.Name+"” foi cadastrada.")
	seeOther(w, r, "/admin")
}

func (h *Product) Edit(w http.ResponseWriter, r *http.Request) {
	p, ok := h.load(w, r)
	if !ok {
		return
	}
	h.renderForm(w, r, http.StatusOK, productFormPage{
		ID: p.ID, Form: productFormFrom(p), Colours: colourBlocksFrom(p), current: &p,
	})
}

func (h *Product) Update(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sub, ok := h.readProductSubmission(w, r, productFormPage{ID: id})
	if !ok {
		return
	}
	page := productFormPage{ID: id, Form: sub.form}
	p, priceErr := sub.form.product()
	p.ID = id
	if priceErr != nil || sub.unreadable() {
		h.unreadable(w, r, page, sub, p, priceErr)
		return
	}

	var colours *domain.ColoursEdit
	if sub.colours != nil {
		colours = &sub.colours.edit
	}
	updated, err := h.service.Update(r.Context(), p, colours)
	if err != nil {
		h.saveFailed(w, r, page, sub, err)
		return
	}
	h.flash(r, sessionFlashKey, "As alterações em “"+updated.Name+"” foram salvas.")
	// Back to the list it is on: an archived piece is not in the main one.
	if updated.Archived() {
		seeOther(w, r, archivedListPath)
		return
	}
	seeOther(w, r, "/admin")
}

// SetSoldOut is the list's quick toggle. Through HTMX it answers with the
// product's row alone; without JavaScript, the form posts and the list
// reloads.
func (h *Product) SetSoldOut(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r) {
		return
	}
	ctx := r.Context()

	// The form carries the state wanted, not "toggle": a double tap then
	// asks for the same thing twice instead of undoing itself.
	soldOut, err := strconv.ParseBool(r.PostForm.Get("sold_out"))
	if err != nil {
		h.renderMessage(w, r, http.StatusBadRequest,
			"Não consegui ler o pedido", "Volte para a lista e tente de novo.")
		return
	}

	p, err := h.service.SetSoldOut(ctx, r.PathValue("id"), soldOut)
	var notFound *domain.NotFoundError
	if errors.As(err, &notFound) {
		h.flash(r, sessionFlashErrorKey, "Essa peça não existe mais. Talvez alguém a tenha apagado.")
		seeOther(w, r, "/admin")
		return
	}
	if err != nil {
		slog.ErrorContext(ctx, "couldn't set sold out", "error", err)
		h.flash(r, sessionFlashErrorKey, "Não consegui salvar agora. Tente de novo em instantes.")
		seeOther(w, r, "/admin")
		return
	}

	if isHTMX(r) {
		h.templates.renderFragment(w, r, http.StatusOK, "products.html", "product-row", p)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// SetStock is the list's quick "−1" on one size of one colour. Like
// SetSoldOut, the form carries the quantity wanted, not "one less": a
// double tap then asks for the same thing twice instead of taking two.
func (h *Product) SetStock(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r) {
		return
	}
	ctx := r.Context()

	quantity, err := strconv.ParseInt(r.PostForm.Get("quantity"), 10, 32)
	if err != nil {
		h.renderMessage(w, r, http.StatusBadRequest,
			"Não consegui ler o pedido", "Volte para a lista e tente de novo.")
		return
	}

	p, err := h.service.SetStock(ctx, r.PathValue("id"), r.PathValue("colourID"), r.PathValue("size"), int32(quantity))
	var notFound *domain.NotFoundError
	if errors.As(err, &notFound) {
		h.flash(r, sessionFlashErrorKey, "Essa peça ou essa cor não existe mais. Talvez alguém a tenha apagado.")
		seeOther(w, r, "/admin")
		return
	}
	var invalid *domain.ValidationError
	if errors.As(err, &invalid) {
		// The list was older than the piece, whose sizes changed since.
		h.flash(r, sessionFlashErrorKey, invalid.Message+". Nada foi salvo; confira as quantidades abaixo.")
		seeOther(w, r, "/admin")
		return
	}
	if err != nil {
		slog.ErrorContext(ctx, "couldn't set stock", "error", err)
		h.flash(r, sessionFlashErrorKey, "Não consegui salvar agora. Tente de novo em instantes.")
		seeOther(w, r, "/admin")
		return
	}

	if isHTMX(r) {
		h.templates.renderFragment(w, r, http.StatusOK, "products.html", "product-row", p)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (h *Product) ConfirmArchive(w http.ResponseWriter, r *http.Request) {
	p, ok := h.load(w, r)
	if !ok {
		return
	}
	h.templates.render(w, r, http.StatusOK, "product_archive.html", productConfirmPage{
		adminLayout: h.layout(r),
		Product:     p,
	})
}

func (h *Product) Archive(w http.ResponseWriter, r *http.Request) {
	p, ok := h.setArchived(w, r, true)
	if !ok {
		return
	}
	h.flash(r, sessionFlashKey, "“"+p.Name+"” saiu do site. Ela continua guardada em “Peças arquivadas”.")
	seeOther(w, r, "/admin")
}

// Unarchive puts a product back on the site. Through HTMX, from the
// archived list, it answers with a row saying so in place of the
// product's; otherwise it goes to the main list, where the piece now is.
func (h *Product) Unarchive(w http.ResponseWriter, r *http.Request) {
	p, ok := h.setArchived(w, r, false)
	if !ok {
		return
	}
	if isHTMX(r) {
		h.templates.renderFragment(w, r, http.StatusOK, "products_archived.html", "restored-row", p)
		return
	}
	h.flash(r, sessionFlashKey, "“"+p.Name+"” voltou para o site.")
	seeOther(w, r, "/admin")
}

// setArchived archives or restores the product named in the URL, or
// answers for the handler when it cannot.
func (h *Product) setArchived(w http.ResponseWriter, r *http.Request, archived bool) (domain.Product, bool) {
	ctx := r.Context()
	p, err := h.service.SetArchived(ctx, r.PathValue("id"), archived)
	var notFound *domain.NotFoundError
	if errors.As(err, &notFound) {
		h.flash(r, sessionFlashErrorKey, "Essa peça não existe mais. Talvez alguém a tenha apagado.")
		seeOther(w, r, "/admin")
		return domain.Product{}, false
	}
	if err != nil {
		slog.ErrorContext(ctx, "couldn't set archived", "archived", archived, "error", err)
		h.flash(r, sessionFlashErrorKey, "Não consegui salvar agora. Tente de novo em instantes.")
		seeOther(w, r, "/admin")
		return domain.Product{}, false
	}
	return p, true
}

func (h *Product) ConfirmDelete(w http.ResponseWriter, r *http.Request) {
	p, ok := h.load(w, r)
	if !ok {
		return
	}
	h.templates.render(w, r, http.StatusOK, "product_delete.html", productConfirmPage{
		adminLayout: h.layout(r),
		Product:     p,
	})
}

func (h *Product) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, ok := h.load(w, r)
	if !ok {
		return
	}

	err := h.service.Delete(ctx, p.ID)
	var notFound *domain.NotFoundError
	if err != nil && !errors.As(err, &notFound) {
		slog.ErrorContext(ctx, "couldn't delete product", "error", err)
		h.renderMessage(w, r, http.StatusInternalServerError,
			"Não consegui apagar a peça", "Ela continua cadastrada. Tente de novo em instantes.")
		return
	}
	// Gone either way: if someone else deleted it a moment ago, the
	// outcome is the one asked for.
	slog.InfoContext(ctx, "product deleted", "product_id", p.ID, "slug", p.Slug)
	h.flash(r, sessionFlashKey, "“"+p.Name+"” foi apagada de vez.")
	seeOther(w, r, archivedListPath)
}

// load fetches the product named in the URL, or answers for the handler
// when it cannot.
func (h *Product) load(w http.ResponseWriter, r *http.Request) (domain.Product, bool) {
	ctx := r.Context()
	p, err := h.service.GetByID(ctx, r.PathValue("id"))
	var notFound *domain.NotFoundError
	if errors.As(err, &notFound) {
		h.renderMessage(w, r, http.StatusNotFound,
			"Peça não encontrada", "Talvez ela tenha sido apagada. Volte para a lista de peças.")
		return domain.Product{}, false
	}
	if err != nil {
		slog.ErrorContext(ctx, "couldn't load product", "error", err)
		h.renderMessage(w, r, http.StatusInternalServerError,
			"Não consegui carregar a peça", "Tente de novo em instantes.")
		return domain.Product{}, false
	}
	return p, true
}

func (h *Product) parseForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxProductFormBytes)
	if err := r.ParseForm(); err != nil {
		h.renderMessage(w, r, http.StatusBadRequest,
			"Não consegui ler o formulário", "Volte, confira o que foi escrito e tente de novo.")
		return false
	}
	return true
}

// saveFailed answers a create or update the service refused, showing the
// form again as it was sent. New photos cannot be put back into a file
// field, so without JavaScript they have to be chosen again; with it, the
// browser still holds them and only the messages are swapped in.
func (h *Product) saveFailed(w http.ResponseWriter, r *http.Request, page productFormPage, sub productSubmission, err error) {
	ctx := r.Context()

	var notFound *domain.NotFoundError
	if errors.As(err, &notFound) {
		h.renderMessage(w, r, http.StatusNotFound,
			"Peça não encontrada", "Talvez alguém a tenha apagado enquanto você editava.")
		return
	}
	var stale *domain.StaleError
	if errors.As(err, &stale) {
		page.Alert = "As cores ou as fotos desta peça mudaram enquanto você editava, talvez em outro aparelho. " +
			"Nada foi salvo: recarregue a página para vê-las como estão agora e refaça as mudanças."
		page.Colours = sub.blocks()
		h.renderForm(w, r, http.StatusConflict, page)
		return
	}

	errs := fieldErrors(err)
	if len(errs) == 0 {
		slog.ErrorContext(ctx, "couldn't save product", "error", err)
		page.Alert = "Não consegui salvar agora. Nada foi salvo: tente de novo em instantes."
		page.Colours = sub.blocks()
		h.renderForm(w, r, http.StatusInternalServerError, page)
		return
	}
	h.renderInvalid(w, r, page, sub, errs)
}

// unreadable answers a form whose price or quantities could not be read,
// without calling the service: whatever was typed must not be saved as
// "no price" or "not counted". The domain still checks everything else,
// so it is all marked at once. The slug is left out: the service derives
// it, and has not run.
func (h *Product) unreadable(w http.ResponseWriter, r *http.Request, page productFormPage, sub productSubmission, p domain.Product, priceErr error) {
	p = p.Normalized()
	err := p.Validate()
	if sub.colours != nil {
		err = errors.Join(err, sub.colours.edit.Normalized(p.Sizes).Validate())
	}
	errs := fieldErrors(err)
	delete(errs, "slug")
	if priceErr != nil {
		errs["price"] = "Escreva o preço só com números, como 189,90, ou deixe vazio"
	}
	if sub.colours != nil {
		for field, msg := range sub.colours.unreadable {
			errs[field] = msg
		}
	}
	h.renderInvalid(w, r, page, sub, errs)
}

func (h *Product) renderInvalid(w http.ResponseWriter, r *http.Request, page productFormPage, sub productSubmission, errs map[string]string) {
	if sub.colours != nil {
		page.ColourErrors = sub.colours.placeErrors(errs)
		page.Colours = sub.colours.blocks
	}
	page.Errors = errs
	// A new product's address comes from its name, and the form has no
	// address field to show the error next to.
	if msg, ok := page.Errors["slug"]; ok && page.ID == "" {
		delete(page.Errors, "slug")
		if _, taken := page.Errors["name"]; !taken {
			page.Errors["name"] = msg
		}
	}
	page.Alert = "Nada foi salvo. Confira o que está marcado abaixo."
	h.renderForm(w, r, http.StatusBadRequest, page)
}

func (h *Product) renderForm(w http.ResponseWriter, r *http.Request, status int, page productFormPage) {
	page.adminLayout = h.layout(r)
	page.Sizes = domain.AllSizes()
	if page.ID != "" && page.current == nil {
		// A rejected edit shows the form again; the photos come from the
		// database, not from what was posted. Without them the section
		// would look empty, so a failure here is only logged.
		p, err := h.service.GetByID(r.Context(), page.ID)
		if err != nil {
			slog.WarnContext(r.Context(), "couldn't load photos for the form", "error", err)
		}
		page.current = &p
	}
	if page.current != nil {
		page.Archived = page.current.Archived()
		if page.Colours == nil {
			page.Colours = colourBlocksFrom(*page.current)
		} else {
			page.Colours = withStoredPhotos(page.Colours, *page.current)
		}
	}
	page.Collections, page.CollectionsUnknown = h.formCollections(r, page)
	if len(page.Colours) == 0 {
		// Every piece needs a colour: one is ready to fill, so the rule
		// never shows on the way to saving a first piece.
		page.Colours = []colourBlock{{Key: "0", Stock: map[string]string{}}}
	}
	h.templates.render(w, r, status, "product_form.html", page)
}

// formCollections are the collections the form offers. Without the
// list, the piece can still be saved as it is: the select keeps the
// collection chosen, if any, and the page says why the others are
// missing.
func (h *Product) formCollections(r *http.Request, page productFormPage) ([]domain.Collection, bool) {
	collections, err := h.collections.List(r.Context())
	if err == nil {
		return collections, false
	}
	slog.WarnContext(r.Context(), "couldn't list collections for the form", "error", err)
	if page.Form.CollectionID == "" {
		return nil, true
	}
	name := "A coleção escolhida"
	if page.current != nil && page.current.CollectionID == page.Form.CollectionID {
		name = page.current.CollectionName
	}
	return []domain.Collection{{ID: page.Form.CollectionID, Name: name}}, true
}

func (h *Product) renderMessage(w http.ResponseWriter, r *http.Request, status int, title, text string) {
	h.templates.render(w, r, status, "message.html", messagePage{
		adminLayout: h.layout(r),
		Title:       title,
		Text:        text,
	})
}

func (h *Product) layout(r *http.Request) adminLayout {
	return newAdminLayout(r, h.sessions, sectionProducts)
}

func (h *Product) flash(r *http.Request, key, msg string) {
	h.sessions.Put(r.Context(), key, msg)
}

// fieldErrors collects the validation errors in err, which the domain
// joins, by field. The first message for a field wins.
func fieldErrors(err error) map[string]string {
	errs := map[string]string{}
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
			if _, seen := errs[v.Field]; !seen {
				errs[v.Field] = v.Message
			}
		}
	}
	walk(err)
	return errs
}

func isHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

// seeOther sends the browser on after a form post. HTMX would follow a
// plain redirect inside the fragment it asked for, so it is told to load
// the whole page instead.
func seeOther(w http.ResponseWriter, r *http.Request, url string) {
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", url)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, url, http.StatusSeeOther)
}

// unreadable reports whether a quantity could not be read.
func (s productSubmission) unreadable() bool {
	return s.colours != nil && len(s.colours.unreadable) > 0
}

// blocks are the colour blocks as posted, or nil without a colour
// section, to show the form again.
func (s productSubmission) blocks() []colourBlock {
	if s.colours == nil {
		return nil
	}
	return s.colours.blocks
}
