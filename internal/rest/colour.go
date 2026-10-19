package rest

import (
	"fmt"
	"mime/multipart"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

// The colour section of the product form. Each colour block has a key,
// which names its fields:
//
//	colour_<k>_id            the colour's ID, empty for a new one
//	colour_<k>_name
//	colour_<k>_stock_<size>  a quantity, empty when the size is not counted
//	colour_<k>_remove        set to delete an existing colour on save
//	photo_order_<k>          as photo_order was, sent by photos.js only
//	photo_remove_<k>         one per photo to delete, by photos.js only
//	photos_<k>               the new files
//
// colourKeyField lists the keys in the order of the blocks on screen.
// Keys are never reused on a page, so they may have gaps where a block
// was taken off; they are not positions. Without JavaScript the form
// posts as it is: no photo_order_<k>, and each colour's files go after
// its existing photos.
const colourKeyField = "colour_key"

// maxColourBlocks bounds what a form may post, removed colours included.
// More is not something the form produces.
const maxColourBlocks = 3 * domain.MaxColours

func colourField(key, field string) string { return "colour_" + key + "_" + field }
func photoOrderField(key string) string    { return "photo_order_" + key }
func photoRemoveField(key string) string   { return "photo_remove_" + key }
func photoFilesField(key string) string    { return "photos_" + key }

// colourBlock is one colour of the form, as stored or as posted.
type colourBlock struct {
	Key    string
	ID     string            // empty for a new colour
	Name   string            // as typed
	Stock  map[string]string // quantities by size, as typed
	Remove bool              // marked to be deleted on save

	// Photos are the ones stored; changes made on screen live in the
	// browser (static/photos.js) until the form is saved.
	Photos []domain.Image

	Errors      map[string]string // by field within the colour: "name", "stock.P"
	PhotoErrors []string          // one per file that could not be used, naming it
}

// blank reports whether b is a new colour with nothing typed in it.
func (b colourBlock) blank() bool {
	if b.ID != "" || strings.TrimSpace(b.Name) != "" {
		return false
	}
	for _, v := range b.Stock {
		if strings.TrimSpace(v) != "" {
			return false
		}
	}
	return true
}

// colourBlocksFrom shows a product's colours as stored.
func colourBlocksFrom(p domain.Product) []colourBlock {
	blocks := make([]colourBlock, len(p.Colours))
	for i, c := range p.Colours {
		stock := map[string]string{}
		for _, s := range c.Stock {
			stock[s.Size] = strconv.Itoa(int(s.Quantity))
		}
		blocks[i] = colourBlock{Key: strconv.Itoa(i), ID: c.ID, Name: c.Name, Stock: stock, Photos: c.Images}
	}
	return blocks
}

// withStoredPhotos gives each posted block of an existing colour the
// photos it has, which a rejected form shows again: the photos chosen on
// screen are in the browser, not in what was posted.
func withStoredPhotos(blocks []colourBlock, p domain.Product) []colourBlock {
	for i, b := range blocks {
		if j := slices.IndexFunc(p.Colours, func(c domain.Colour) bool { return b.ID != "" && c.ID == b.ID }); j >= 0 {
			blocks[i].Photos = p.Colours[j].Images
		}
	}
	return blocks
}

// postedColours is the colour section of a submission: the edit for the
// service, and how its entries map back to the blocks on screen.
type postedColours struct {
	edit   domain.ColoursEdit
	blocks []colourBlock // as posted, in order, removed ones included

	// Per entry of edit.Colours: its block, and the names of its new
	// files, for messages.
	blockOf   []int
	fileNames [][]string

	// unreadable holds quantities that are not numbers, by the field the
	// service would name them with. They must not be saved: dropped,
	// they would read as "not counted".
	unreadable map[string]string

	// tooBig names, per block, the files over maxPhotoBytes.
	tooBig map[int][]string
}

// readColours reads the colour section of a parsed form, for a product
// with sizes. It reports false when the form has none, and an error when
// it is not something the form produces.
func readColours(r *http.Request, sizes []string) (postedColours, bool, error) {
	keys, posted := r.PostForm[colourKeyField]
	if !posted {
		return postedColours{}, false, nil
	}
	if len(keys) > maxColourBlocks {
		return postedColours{}, true, fmt.Errorf("%d colour blocks", len(keys))
	}

	var files map[string][]*multipart.FileHeader
	if r.MultipartForm != nil {
		files = r.MultipartForm.File
	}

	pc := postedColours{edit: domain.ColoursEdit{Colours: []domain.ColourEdit{}}, unreadable: map[string]string{}, tooBig: map[int][]string{}}
	seen := map[string]bool{}
	for _, key := range keys {
		if n, err := strconv.Atoi(key); err != nil || n < 0 || strconv.Itoa(n) != key || seen[key] {
			return postedColours{}, true, fmt.Errorf("colour key %q", key)
		}
		seen[key] = true

		b := colourBlock{
			Key:    key,
			ID:     strings.TrimSpace(r.PostForm.Get(colourField(key, "id"))),
			Name:   r.PostForm.Get(colourField(key, "name")),
			Stock:  map[string]string{},
			Remove: r.PostForm.Get(colourField(key, "remove")) != "",
		}
		for _, size := range domain.AllSizes() {
			if v, ok := r.PostForm[colourField(key, "stock_"+size)]; ok {
				b.Stock[size] = v[0]
			}
		}
		pc.blocks = append(pc.blocks, b)
		blockIndex := len(pc.blocks) - 1

		// A new colour taken off before saving was never anything.
		if b.Remove {
			if b.ID != "" {
				pc.edit.Removed = append(pc.edit.Removed, b.ID)
			}
			continue
		}

		pos := len(pc.edit.Colours)
		c := domain.ColourEdit{ID: b.ID, Name: b.Name}
		// Only the sizes ticked are read: the others' fields are hidden,
		// and whatever they hold means nothing.
		for _, size := range sizes {
			v := strings.TrimSpace(b.Stock[size])
			if v == "" {
				continue
			}
			q, err := strconv.ParseInt(v, 10, 32)
			if err != nil {
				pc.unreadable[domain.ColourField(pos, "stock."+size)] = "Escreva a quantidade só com números, ou deixe vazio"
				continue
			}
			c.Stock = append(c.Stock, domain.Stock{Size: size, Quantity: int32(q)})
		}

		photos, names, tooBig, err := readColourPhotos(r, key, files[photoFilesField(key)])
		if err != nil {
			return postedColours{}, true, err
		}
		c.Photos = photos
		if len(tooBig) > 0 {
			pc.tooBig[blockIndex] = tooBig
		}

		pc.edit.Colours = append(pc.edit.Colours, c)
		pc.blockOf = append(pc.blockOf, blockIndex)
		pc.fileNames = append(pc.fileNames, names)
	}
	return pc, true, nil
}

// readColourPhotos reads the photo edit of the colour block key: the
// order photos.js arranged, or, without it, the files to go after the
// photos there are.
func readColourPhotos(r *http.Request, key string, files []*multipart.FileHeader) (domain.PhotoEdit, []string, []string, error) {
	_, withJS := r.PostForm[photoOrderField(key)]
	edit := domain.PhotoEdit{AppendNew: !withJS}
	var names, tooBig []string
	for _, fh := range files {
		names = append(names, shortName(fh.Filename))
		if fh.Size > maxPhotoBytes {
			tooBig = append(tooBig, shortName(fh.Filename))
		}
		edit.New = append(edit.New, uploadedPhoto{fh: fh})
	}
	if !withJS {
		return edit, names, tooBig, nil
	}
	order, ok := parsePhotoOrder(r.PostForm.Get(photoOrderField(key)))
	if !ok {
		return domain.PhotoEdit{}, nil, nil, fmt.Errorf("photo order of colour %s", key)
	}
	edit.Order = order
	edit.Removed = r.PostForm[photoRemoveField(key)]
	return edit, names, tooBig, nil
}

// placeErrors moves the errors about colours out of errs, which
// fieldErrors collected from the service, onto the blocks they are about.
// What concerns the colours as a whole, such as their number or the
// photos of the whole piece, comes back to show above them.
func (pc *postedColours) placeErrors(errs map[string]string) []string {
	var general []string
	for _, field := range []string{"colours", "photos"} {
		if msg, ok := errs[field]; ok {
			general = append(general, msg)
			delete(errs, field)
		}
	}

	for pos, bi := range pc.blockOf {
		b := &pc.blocks[bi]
		take := func(field string) (string, bool) {
			msg, ok := errs[domain.ColourField(pos, field)]
			delete(errs, domain.ColourField(pos, field))
			return msg, ok
		}
		if msg, ok := take("name"); ok {
			b.setError("name", msg)
		}
		for _, size := range domain.AllSizes() {
			if msg, ok := take("stock." + size); ok {
				b.setError("stock."+size, msg)
			}
		}
		if msg, ok := take("photos"); ok {
			b.PhotoErrors = append(b.PhotoErrors, msg)
		}
		for i, name := range pc.fileNames[pos] {
			if msg, ok := take(domain.PhotoErrorField(i)); ok {
				b.PhotoErrors = append(b.PhotoErrors, name+": "+msg)
			}
		}
	}

	// Anything else about colours still has to be shown somewhere, or the
	// answer would say to check the marked fields and mark none.
	var rest []string
	for field := range errs {
		if strings.HasPrefix(field, "colours.") {
			rest = append(rest, field)
		}
	}
	slices.Sort(rest)
	for _, field := range rest {
		general = append(general, errs[field])
		delete(errs, field)
	}
	return general
}

func (b *colourBlock) setError(field, msg string) {
	if b.Errors == nil {
		b.Errors = map[string]string{}
	}
	b.Errors[field] = msg
}

// colourView is a colour block with what its template needs from the
// page around it.
type colourView struct {
	colourBlock
	Number int // counted from 1, for the heading
	Only   bool
	// Extra is the block for adding a colour without JavaScript, which
	// sits in a noscript: it must not hold one of its own, since the
	// first </noscript> would end both.
	Extra      bool
	Stock      []stockField
	MaxPhotos  int
	MaxPhotoMB int
	SaveLabel  string
}

// stockField is one size's quantity in a colour block.
type stockField struct {
	Size  string
	Value string
	Shown bool // the product comes in this size
	Error string
}

// HasSizes reports whether any size is ticked, without which there is no
// quantity to give.
func (v colourView) HasSizes() bool {
	return slices.ContainsFunc(v.Stock, func(s stockField) bool { return s.Shown })
}

func (page productFormPage) colourView(b colourBlock, number int) colourView {
	v := colourView{
		colourBlock: b,
		Number:      number,
		Only:        len(page.Colours) <= 1,
		MaxPhotos:   domain.MaxImagesPerColour,
		MaxPhotoMB:  maxPhotoBytes >> 20,
		SaveLabel:   page.SaveLabel(),
	}
	for _, size := range domain.AllSizes() {
		v.Stock = append(v.Stock, stockField{
			Size: size, Value: b.Stock[size], Shown: page.Form.HasSize(size), Error: b.Errors["stock."+size],
		})
	}
	return v
}

// ColourViews are the colour blocks to show, in order.
func (page productFormPage) ColourViews() []colourView {
	views := make([]colourView, len(page.Colours))
	for i, b := range page.Colours {
		views[i] = page.colourView(b, i+1)
	}
	return views
}

// NewColourTemplate is the block photos.js copies for "Adicionar outra
// cor", with __N__ standing for the key it gives it.
func (page productFormPage) NewColourTemplate() colourView {
	v := page.colourView(colourBlock{Key: "__N__", Stock: map[string]string{}}, 0)
	v.Only = false
	return v
}

// ExtraColour is a blank block for adding a colour without JavaScript,
// which cannot add one on screen. It is nil when a blank one already
// shows.
func (page productFormPage) ExtraColour() *colourView {
	next := 0
	for _, b := range page.Colours {
		if !b.Remove && b.blank() {
			return nil
		}
		if n, err := strconv.Atoi(b.Key); err == nil && n >= next {
			next = n + 1
		}
	}
	v := page.colourView(colourBlock{Key: strconv.Itoa(next), Stock: map[string]string{}}, len(page.Colours)+1)
	v.Only = false
	v.Extra = true
	return &v
}

// NextColourKey is the first key photos.js may give a colour it adds.
func (page productFormPage) NextColourKey() int {
	next := 0
	for _, b := range page.Colours {
		if n, err := strconv.Atoi(b.Key); err == nil && n >= next {
			next = n + 1
		}
	}
	return next
}

func (productFormPage) MaxColours() int { return domain.MaxColours }

func (page productFormPage) SaveLabel() string {
	if page.ID != "" {
		return "Salvar alterações"
	}
	return "Cadastrar peça"
}
