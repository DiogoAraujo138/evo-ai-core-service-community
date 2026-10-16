// Package chordataempresa liga o multi-empresa Chordata (ADR-0007, Caminho C) ao Core.
//
// Desligado por padrão: sem MULTI_EMPRESA=on nada muda. Ligado, cada requisição ganha uma
// transação com app.empresa_id (RLS do Postgres) e o GORM é roteado para ela. Sem empresa
// decidida a requisição é recusada; fora de uma requisição o banco não enxerga linha nenhuma.
package chordataempresa

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"regexp"
	"strings"
)

// Header é o mesmo cabeçalho usado pelo CRM, Flow e Processor.
const Header = "X-Evo-Tenant-Id"

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Ligado diz se MULTI_EMPRESA está ligada (lido a cada chamada).
func Ligado() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MULTI_EMPRESA"))) {
	case "on", "1", "true", "sim", "yes":
		return true
	}
	return false
}

// Recusa é uma decisão negativa com o status HTTP correspondente.
type Recusa struct {
	Status   int
	Mensagem string
}

func (r *Recusa) Error() string { return r.Mensagem }

// Consulta lê empresas e vínculos (tabelas sem RLS).
type Consulta interface {
	Vinculadas(ctx context.Context, userID string) ([]string, error)
	EmpresaAtiva(ctx context.Context, empresaID string) (bool, error)
}

// Resolver decide a empresa da requisição. userID vazio = não autenticado (não acontece depois do EvoAuth).
func Resolver(ctx context.Context, c Consulta, userID, papel, cabecalho string) (string, error) {
	if userID == "" {
		return "", &Recusa{http.StatusUnauthorized, "Autenticação necessária"}
	}
	pedida := strings.ToLower(strings.TrimSpace(cabecalho))
	if pedida != "" && !uuidRe.MatchString(pedida) {
		return "", &Recusa{http.StatusBadRequest, "Empresa inválida"}
	}
	falta := &Recusa{http.StatusBadRequest, "Informe a empresa no cabeçalho " + Header}

	if papel == "super_admin" {
		if pedida == "" {
			return "", falta
		}
		return exigirAtiva(ctx, c, pedida)
	}

	vinculadas, err := c.Vinculadas(ctx, strings.ToLower(userID))
	if err != nil {
		return "", err
	}
	if len(vinculadas) == 0 {
		return "", &Recusa{http.StatusForbidden, "Usuário sem empresa"}
	}
	if pedida != "" {
		for _, v := range vinculadas {
			if v == pedida {
				return pedida, nil
			}
		}
		return "", &Recusa{http.StatusForbidden, "Sem acesso a esta empresa"}
	}
	if len(vinculadas) == 1 {
		return vinculadas[0], nil
	}
	return "", falta
}

func exigirAtiva(ctx context.Context, c Consulta, id string) (string, error) {
	ok, err := c.EmpresaAtiva(ctx, id)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", &Recusa{http.StatusForbidden, "Empresa inexistente ou inativa"}
	}
	return id, nil
}

// ConsultaSQL implementa Consulta sobre o banco compartilhado com o CRM.
type ConsultaSQL struct{ DB *sql.DB }

func (q ConsultaSQL) Vinculadas(ctx context.Context, userID string) ([]string, error) {
	if !uuidRe.MatchString(userID) {
		return nil, nil
	}
	rows, err := q.DB.QueryContext(ctx,
		`SELECT eu.empresa_id::text FROM empresa_usuarios eu JOIN empresas e ON e.id = eu.empresa_id
		 WHERE eu.user_id = $1::uuid AND e.ativa`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (q ConsultaSQL) EmpresaAtiva(ctx context.Context, empresaID string) (bool, error) {
	var um int
	err := q.DB.QueryRowContext(ctx, `SELECT 1 FROM empresas WHERE id = $1::uuid AND ativa`, empresaID).Scan(&um)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}
