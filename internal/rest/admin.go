package rest

import (
	"errors"
	"fmt"
	"net/http"
)

type adminPage struct {
	Email string
}

// Admin serves the admin screens. The home page is a placeholder until
// the product screens land.
type Admin struct {
	templates *Templates
}

func NewAdmin(templates *Templates) (*Admin, error) {
	if templates == nil {
		return nil, errors.New("missing templates dependency")
	}
	return &Admin{templates: templates}, nil
}

func MustNewAdmin(templates *Templates) *Admin {
	a, err := NewAdmin(templates)
	if err != nil {
		panic(fmt.Errorf("couldn't create admin handler: %w", err))
	}
	return a
}

func (a *Admin) Home(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/admin", http.StatusFound)
}

// Index must be behind Auth.RequireLogin.
func (a *Admin) Index(w http.ResponseWriter, r *http.Request) {
	// RequireLogin always sets the user; ok is false only if this
	// handler was wired without it, which the tests catch.
	user, _ := currentUser(r.Context())
	a.templates.render(w, r, http.StatusOK, "admin.html", adminPage{Email: user.Email})
}
