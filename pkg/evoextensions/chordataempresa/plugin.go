package chordataempresa

import (
	"database/sql"

	"evo-ai-core-service/pkg/evoextensions/runtimecontext"

	"gorm.io/gorm"
)

const (
	savepoint    = "chordata_empresa_sp"
	chaveSavept  = "chordata:empresa:savepoint"
	posCriar     = "chordata:empresa:create:fim"
	posAtualizar = "chordata:empresa:update:fim"
	posApagar    = "chordata:empresa:delete:fim"
)

// Plugin manda toda instrução GORM de uma requisição para a transação da empresa
// (a que carrega app.empresa_id). Fora de uma requisição a instrução segue no pool, sem
// empresa, e o RLS do banco não devolve nenhuma linha (falha segura).
type Plugin struct{}

func (Plugin) Name() string { return "chordata:empresa" }

func (Plugin) Initialize(db *gorm.DB) error {
	cb := db.Callback()
	if err := cb.Query().Before("gorm:query").Register("chordata:empresa:query", rotear); err != nil {
		return err
	}
	if err := cb.Row().Before("gorm:row").Register("chordata:empresa:row", rotear); err != nil {
		return err
	}
	// Raw/Exec também pode ser escrita (INSERT/UPDATE em SQL cru): mesmo savepoint das escritas.
	if err := cb.Raw().Before("gorm:raw").Register("chordata:empresa:raw", rotearEscrita); err != nil {
		return err
	}
	if err := cb.Raw().After("gorm:raw").Register("chordata:empresa:raw:fim", encerrarSavepoint); err != nil {
		return err
	}
	// Escritas: antes de o GORM tentar abrir a própria transação (uma *sql.Tx já é transação). Cada escrita roda
	// dentro de um SAVEPOINT: um erro de SQL (ex.: nome repetido) desfaz só aquela instrução e a transação da
	// requisição segue utilizável, como era antes (cada instrução confirmava sozinha).
	if err := cb.Create().Before("gorm:begin_transaction").Register("chordata:empresa:create", rotearEscrita); err != nil {
		return err
	}
	if err := cb.Create().After("gorm:create").Register(posCriar, encerrarSavepoint); err != nil {
		return err
	}
	if err := cb.Update().Before("gorm:begin_transaction").Register("chordata:empresa:update", rotearEscrita); err != nil {
		return err
	}
	if err := cb.Update().After("gorm:update").Register(posAtualizar, encerrarSavepoint); err != nil {
		return err
	}
	if err := cb.Delete().Before("gorm:begin_transaction").Register("chordata:empresa:delete", rotearEscrita); err != nil {
		return err
	}
	return cb.Delete().After("gorm:delete").Register(posApagar, encerrarSavepoint)
}

func rotearEscrita(db *gorm.DB) {
	rotear(db)
	tx, ok := db.Statement.ConnPool.(*sql.Tx)
	if !ok || db.Error != nil {
		return
	}
	if _, err := tx.ExecContext(db.Statement.Context, "SAVEPOINT "+savepoint); err != nil {
		_ = db.AddError(err)
		return
	}
	db.InstanceSet(chaveSavept, true)
}

// encerrarSavepoint desfaz só a instrução que falhou, ou libera o savepoint quando deu certo.
func encerrarSavepoint(db *gorm.DB) {
	if v, ok := db.InstanceGet(chaveSavept); !ok || v != true {
		return
	}
	tx, ok := db.Statement.ConnPool.(*sql.Tx)
	if !ok {
		return
	}
	comando := "RELEASE SAVEPOINT " + savepoint
	if db.Error != nil {
		comando = "ROLLBACK TO SAVEPOINT " + savepoint
	}
	if _, err := tx.ExecContext(db.Statement.Context, comando); err != nil && db.Error == nil {
		_ = db.AddError(err)
	}
}

func rotear(db *gorm.DB) {
	if db.Statement == nil || db.Statement.Context == nil {
		return
	}
	if conn, ok := runtimecontext.ConnFromContext(db.Statement.Context); ok {
		db.Statement.ConnPool = conn
	}
}
