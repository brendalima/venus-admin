package rest

import (
	"log/slog"
	"net/http"
)

// Publish starts the site's rebuild from the product list's button. It
// does not wait for the build, so the answer only says it started.
func (h *Product) Publish(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := h.site.Publish(ctx); err != nil {
		slog.ErrorContext(ctx, "couldn't publish", "error", err)
		h.flash(r, sessionFlashErrorKey, "Não consegui publicar agora. O site continua como estava: tente de novo em instantes.")
		seeOther(w, r, "/admin")
		return
	}
	slog.InfoContext(ctx, "site publish started")
	h.flash(r, sessionFlashKey, "Publicação iniciada — o site atualiza em cerca de 1 minuto.")
	seeOther(w, r, "/admin")
}
