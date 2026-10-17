// Command api serves the Vênus Oasis admin.
//
// This is the composition root: the only place that knows every layer
// and wires them together. It holds no business rules.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/brendalima/venus-admin/adapters"
	"github.com/brendalima/venus-admin/internal/core/services"
	"github.com/brendalima/venus-admin/internal/repository"
	"github.com/brendalima/venus-admin/internal/rest"
	"github.com/brendalima/venus-admin/internal/storage"
)

func main() {
	if err := run(); err != nil {
		slog.Error("exiting on error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := adapters.LoadConfig()
	if err != nil {
		return err
	}

	logger := newLogger(cfg)
	slog.SetDefault(logger)
	logger.Info("starting", "env", cfg.Env, "port", cfg.Port, "go", runtime.Version())

	// Fly sends SIGTERM when stopping an idle machine, and in-flight
	// requests need to finish before the process exits.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := adapters.NewDatabase(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	hRepository := repository.MustNewHealth(db)
	uRepository := repository.MustNewUser(db)
	sRepository := repository.MustNewSession(db)
	pRepository := repository.MustNewProduct(db)
	prRepository := repository.MustNewPasswordReset(db)

	mailer := newMailer(cfg, logger)

	imageStore := newImageStore(cfg, logger)

	hService := services.MustNewHealth(hRepository)
	uService := services.MustNewUser(uRepository, sRepository)
	pService := services.MustNewProduct(services.ProductInput{
		Repo:      pRepository,
		Store:     imageStore,
		Processor: storage.NewJPEGProcessor(),
	})
	siteService := newSiteService(cfg, logger, pRepository)
	prService := services.MustNewPasswordReset(services.PasswordResetInput{
		Users:   uRepository,
		Resets:  prRepository,
		Mailer:  mailer,
		BaseURL: cfg.PublicURL,
	})

	templates := rest.MustNewTemplates(cfg.ImageBaseURL)
	sessions := rest.MustNewSessionManager(sRepository, cfg.IsProd())
	rHealth := rest.MustNewHealth(hService)
	rAuth := rest.MustNewAuth(rest.AuthInput{
		Service:  uService,
		Sessions: sessions,
		// Ten tries in fifteen minutes is plenty for someone who mistypes,
		// and makes guessing a password hopeless.
		Limiter:        rest.NewLoginLimiter(10, 15*time.Minute),
		Templates:      templates,
		ClientIPHeader: cfg.ClientIPHeader,
	})
	rProduct := rest.MustNewProduct(rest.ProductInput{
		Service:   pService,
		Site:      siteService,
		Sessions:  sessions,
		Templates: templates,
	})
	rPasswordReset := rest.MustNewPasswordReset(rest.PasswordResetInput{
		Service:  prService,
		Sessions: sessions,
		// Each request may send an email; the service also caps links per
		// account, so this only stops one address from spraying many.
		Limiter:        rest.NewLoginLimiter(5, 15*time.Minute),
		Templates:      templates,
		ClientIPHeader: cfg.ClientIPHeader,
	})
	private := func(h http.HandlerFunc) http.Handler { return rAuth.RequireLogin(h) }

	// Everything a person sees goes through the session; the health
	// checks and static files stay outside it, so they never touch the
	// database.
	app := http.NewServeMux()
	app.Handle("GET /{$}", http.RedirectHandler("/admin", http.StatusFound))
	app.HandleFunc("GET /login", rAuth.LoginPage)
	app.HandleFunc("POST /login", rAuth.Login)
	app.HandleFunc("POST /logout", rAuth.Logout)
	app.HandleFunc("GET /forgot-password", rPasswordReset.ForgotPage)
	app.HandleFunc("POST /forgot-password", rPasswordReset.Forgot)
	app.HandleFunc("GET /reset-password", rPasswordReset.ResetPage)
	app.HandleFunc("POST /reset-password", rPasswordReset.Reset)
	app.Handle("GET /admin", private(rProduct.List))
	app.Handle("GET /admin/products/new", private(rProduct.New))
	app.Handle("POST /admin/products", private(rProduct.Create))
	app.Handle("GET /admin/products/{id}", private(rProduct.Edit))
	app.Handle("POST /admin/products/{id}", private(rProduct.Update))
	app.Handle("POST /admin/products/{id}/sold-out", private(rProduct.SetSoldOut))
	app.Handle("GET /admin/products/{id}/delete", private(rProduct.ConfirmDelete))
	app.Handle("POST /admin/products/{id}/delete", private(rProduct.Delete))
	app.Handle("POST /admin/publish", private(rProduct.Publish))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", rHealth.Live)
	mux.HandleFunc("GET /readyz", rHealth.Ready)
	mux.Handle("GET /static/", http.StripPrefix("/static/", rest.MustNewStatic()))
	// The site's build reads the catalogue with a token, not a session.
	if cfg.APIToken != "" {
		rAPI := rest.MustNewAPI(rest.APIInput{Service: pService, Token: cfg.APIToken, ImageBaseURL: cfg.ImageBaseURL})
		mux.Handle("GET /api/products", rAPI.RequireToken(http.HandlerFunc(rAPI.Products)))
	} else {
		logger.Warn("no API_TOKEN: /api/products is off")
	}
	if cfg.MediaDir != "" {
		mux.Handle("GET /media/", http.StripPrefix("/media/", rest.MustNewMedia(cfg.MediaDir)))
	}
	mux.Handle("/", sessions.LoadAndSave(app))

	handler := rest.Chain(mux,
		rest.RecoverPanics(logger),
		rest.LogRequests(logger),
		rest.SecureHeaders,
		rest.RejectCrossOrigin,
	)

	return serve(ctx, logger, newServer(cfg.Addr(), handler))
}

// newImageStore keeps photos in R2 when it is configured. Config only
// allows going without it in development, where photos stay on disk.
func newImageStore(cfg adapters.Config, logger *slog.Logger) storage.ImageStore {
	if !cfg.R2.Configured() {
		logger.Warn("no R2 settings: photos are kept on disk", "dir", cfg.MediaDir)
		return storage.MustNewDisk(cfg.MediaDir)
	}
	return storage.MustNewR2(storage.R2Input{
		AccountID:       cfg.R2.AccountID,
		AccessKeyID:     cfg.R2.AccessKeyID,
		SecretAccessKey: cfg.R2.SecretAccessKey,
		Bucket:          cfg.R2.Bucket,
	})
}

// newSiteService publishes through the deploy hook when it is configured.
// Without it, development logs instead, and production does not offer
// publishing at all: the button would promise an update that never comes.
func newSiteService(cfg adapters.Config, logger *slog.Logger, repo repository.SiteRepository) *services.Site {
	if cfg.DeployHookURL != "" {
		return services.MustNewSite(services.SiteInput{
			Repo:       repo,
			Publisher:  storage.MustNewDeployHook(cfg.DeployHookURL, nil),
			CanPublish: true,
		})
	}
	logger.Warn("no DEPLOY_HOOK_URL: publishing does not reach the site")
	return services.MustNewSite(services.SiteInput{
		Repo:       repo,
		Publisher:  storage.LogPublisher{},
		CanPublish: !cfg.IsProd(),
	})
}

// newMailer sends through Resend when a key is configured. Config only
// allows going without one in development, where emails are logged.
func newMailer(cfg adapters.Config, logger *slog.Logger) storage.Mailer {
	if cfg.ResendAPIKey == "" {
		logger.Warn("no RESEND_API_KEY: emails will be logged, not sent")
		return storage.LogMailer{}
	}
	return storage.MustNewResend(storage.ResendInput{APIKey: cfg.ResendAPIKey, From: cfg.MailFrom})
}

func newLogger(cfg adapters.Config) *slog.Logger {
	if cfg.IsProd() {
		// JSON in production so Fly's log search is usable.
		return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
}
