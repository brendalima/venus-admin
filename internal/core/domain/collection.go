package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxCollectionNameLength is generous for a name like "Verão 2026", and
// keeps a paragraph pasted by mistake out of the site's headings.
const MaxCollectionNameLength = 60

// Collection groups pieces under a name, such as "Verão 2026". A piece
// belongs to one collection or to none.
type Collection struct {
	ID   string
	Name string
	// Pieces counts the pieces in the collection, archived ones included.
	// It is filled in by the lists, for the owners to see what deleting a
	// collection would touch.
	Pieces    int
	CreatedAt time.Time
}

// Normalized trims the name and keeps single spaces in it, so "Verão  2026"
// and "Verão 2026" are not two collections.
func (c Collection) Normalized() Collection {
	c.Name = strings.Join(strings.Fields(c.Name), " ")
	return c
}

// Validate checks the name.
func (c Collection) Validate() error {
	switch {
	case strings.TrimSpace(c.Name) == "":
		return NewValidationError("name", "Falta o nome da coleção")
	case utf8.RuneCountInString(c.Name) > MaxCollectionNameLength:
		return NewValidationError("name", fmt.Sprintf("O nome pode ter até %d letras", MaxCollectionNameLength))
	}
	return nil
}
