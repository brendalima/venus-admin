// Command adduser gives a person access to the admin.
//
// It is the only way in: the admin has no public signup. It asks for the
// password twice without echoing it, so it never lands in the shell
// history or in the process list.
//
//	go run ./cmd/adduser -email ana@exemplo.com
//	fly ssh console -C "/adduser -email ana@exemplo.com"
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/term"

	"github.com/brendalima/venus-admin/adapters"
	"github.com/brendalima/venus-admin/internal/core/domain"
	"github.com/brendalima/venus-admin/internal/core/services"
	"github.com/brendalima/venus-admin/internal/repository"
)

func main() {
	if err := run(); err != nil {
		var invalid *domain.ValidationError
		if errors.As(err, &invalid) {
			// Whoever runs this may well be one of the owners.
			fmt.Fprintln(os.Stderr, invalid.Message)
			os.Exit(1)
		}
		var conflict *domain.ConflictError
		if errors.As(err, &conflict) {
			fmt.Fprintln(os.Stderr, "Já existe um acesso com esse e-mail.")
			os.Exit(1)
		}
		slog.Error("couldn't add user", "error", err)
		os.Exit(1)
	}
}

func run() error {
	email := flag.String("email", "", "email the person will log in with")
	flag.Parse()

	cfg, err := adapters.LoadConfig()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	in := bufio.NewReader(os.Stdin)
	if *email == "" {
		if *email, err = prompt(in, "E-mail: "); err != nil {
			return err
		}
	}
	password, err := promptPassword(in, "Senha: ")
	if err != nil {
		return err
	}
	confirmation, err := promptPassword(in, "Repita a senha: ")
	if err != nil {
		return err
	}
	if password != confirmation {
		return domain.NewValidationError("password", "As senhas não são iguais.")
	}

	db, err := adapters.NewDatabase(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	uService := services.MustNewUser(repository.MustNewUser(db), repository.MustNewSession(db))
	user, err := uService.Create(ctx, *email, password)
	if err != nil {
		return err
	}

	slog.Info("user added", "id", user.ID, "email", user.Email)
	return nil
}

func prompt(in *bufio.Reader, label string) (string, error) {
	fmt.Fprint(os.Stderr, label)
	line, err := in.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("couldn't read input: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// promptPassword reads without echo from a terminal. Piped input (a
// script, or a console without a TTY) is read as plain lines instead.
func promptPassword(in *bufio.Reader, label string) (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return prompt(in, label)
	}

	fmt.Fprint(os.Stderr, label)
	b, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("couldn't read password: %w", err)
	}
	return string(b), nil
}
