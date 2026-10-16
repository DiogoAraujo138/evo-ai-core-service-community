package chordataempresa

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

const (
	empA     = "11111111-1111-1111-1111-111111111111"
	empB     = "22222222-2222-2222-2222-222222222222"
	inativa  = "33333333-3333-3333-3333-333333333333"
	usuario1 = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	usuario2 = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	ninguem  = "cccccccc-cccc-cccc-cccc-cccccccccccc"
)

type falsa struct {
	vinculos map[string][]string
	ativas   map[string]bool
	quebrada bool
}

func (f falsa) Vinculadas(_ context.Context, u string) ([]string, error) {
	if f.quebrada {
		return nil, errors.New("banco fora do ar")
	}
	return f.vinculos[u], nil
}

func (f falsa) EmpresaAtiva(_ context.Context, id string) (bool, error) {
	if f.quebrada {
		return false, errors.New("banco fora do ar")
	}
	return f.ativas[id], nil
}

func base() falsa {
	return falsa{
		vinculos: map[string][]string{usuario1: {empA}, usuario2: {empA, empB}},
		ativas:   map[string]bool{empA: true, empB: true},
	}
}

func status(err error) int {
	var r *Recusa
	if errors.As(err, &r) {
		return r.Status
	}
	return 0
}

func TestResolver(t *testing.T) {
	ctx := context.Background()
	casos := []struct {
		nome, user, papel, cab string
		quebrada               bool
		empresa                string
		status                 int
	}{
		{"uma empresa dispensa cabeçalho", usuario1, "agent", "", false, empA, 0},
		{"não escolhe empresa alheia", usuario1, "agent", empB, false, "", http.StatusForbidden},
		{"duas empresas exigem escolha", usuario2, "agent", "", false, "", http.StatusBadRequest},
		{"duas empresas, escolhe B (maiúsculas)", usuario2, "agent", "22222222-2222-2222-2222-222222222222", false, empB, 0},
		{"sem vínculo", ninguem, "agent", "", false, "", http.StatusForbidden},
		{"super_admin sem cabeçalho", ninguem, "super_admin", "", false, "", http.StatusBadRequest},
		{"super_admin, empresa inativa", ninguem, "super_admin", inativa, false, "", http.StatusForbidden},
		{"super_admin, empresa ativa", ninguem, "super_admin", empB, false, empB, 0},
		{"cabeçalho malformado", usuario1, "agent", "abc'; drop", false, "", http.StatusBadRequest},
		{"sem usuário", "", "agent", empA, false, "", http.StatusUnauthorized},
		{"banco fora do ar não segue sem escopo", usuario1, "agent", "", true, "", 0},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			f := base()
			f.quebrada = c.quebrada
			got, err := Resolver(ctx, f, c.user, c.papel, c.cab)
			if c.quebrada {
				if err == nil || got != "" {
					t.Fatalf("esperava erro, veio %q %v", got, err)
				}
				return
			}
			if status(err) != c.status || got != c.empresa {
				t.Fatalf("got=%q err=%v (status %d); esperado %q status %d", got, err, status(err), c.empresa, c.status)
			}
		})
	}
}

func TestLigado(t *testing.T) {
	t.Setenv("MULTI_EMPRESA", "")
	if Ligado() {
		t.Fatal("desligado por padrão")
	}
	t.Setenv("MULTI_EMPRESA", "on")
	if !Ligado() {
		t.Fatal("deveria estar ligado")
	}
}
