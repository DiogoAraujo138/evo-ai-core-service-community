package chordataempresa

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"

	"evo-ai-core-service/internal/httpclient/response"
	"evo-ai-core-service/internal/types"
	"evo-ai-core-service/pkg/evoextensions/runtimecontext"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Middleware vai DEPOIS do EvoAuth (precisa de user_id e role no contexto). Abre uma transação
// com app.empresa_id (vale só nela), publica-a no runtimecontext para o Plugin e a encerra no fim
// da requisição: confirma, ou desfaz se o handler entrou em pânico.
func Middleware(db *sql.DB, consulta Consulta) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		empresa, err := Resolver(ctx, consulta, userID(ctx), papel(ctx), c.GetHeader(Header))
		if err != nil {
			recusar(c, err)
			return
		}

		tx, err := db.BeginTx(ctx, nil)
		if err == nil {
			_, err = tx.ExecContext(ctx, `SELECT set_config('app.empresa_id', $1, true)`, empresa)
		}
		if err != nil {
			if tx != nil {
				_ = tx.Rollback()
			}
			log.Printf("chordataempresa: não foi possível abrir a transação da empresa: %v", err)
			response.ErrorResponse(c, "SERVICE_UNAVAILABLE", "Não foi possível resolver a empresa", nil, http.StatusServiceUnavailable)
			c.Abort()
			return
		}

		concluida := false
		defer func() {
			if !concluida { // pânico no handler
				_ = tx.Rollback()
				return
			}
			if err := tx.Commit(); err != nil {
				log.Printf("chordataempresa: falha ao confirmar a transação da empresa %s: %v", empresa, err)
			}
		}()

		ctx = runtimecontext.WithID(ctx, empresa)
		ctx = runtimecontext.WithConn(ctx, tx)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
		concluida = true
	}
}

func recusar(c *gin.Context, err error) {
	var r *Recusa
	if errors.As(err, &r) {
		code := map[int]string{400: "BAD_REQUEST", 401: "UNAUTHORIZED", 403: "FORBIDDEN"}[r.Status]
		response.ErrorResponse(c, code, r.Mensagem, nil, r.Status)
	} else {
		log.Printf("chordataempresa: falha ao resolver a empresa: %v", err)
		response.ErrorResponse(c, "SERVICE_UNAVAILABLE", "Não foi possível resolver a empresa", nil, http.StatusServiceUnavailable)
	}
	c.Abort()
}

func userID(ctx context.Context) string {
	switch v := ctx.Value("user_id").(type) {
	case uuid.UUID:
		return v.String()
	case string:
		return v
	}
	return ""
}

func papel(ctx context.Context) string {
	if r, ok := ctx.Value("role").(*types.Role); ok && r != nil {
		return r.Key
	}
	return ""
}
