package chordataempresa

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Tabelas isoladas do Core (as mesmas do grupo --nucleo do instalador do Processor).
var Tabelas = []string{
	"evo_core_folders", "evo_core_agent_folders", "evo_core_folder_shares", "evo_core_agents",
	"evo_core_agent_integrations", "evo_core_api_keys", "evo_core_custom_mcp_servers",
	"evo_core_custom_tools", "evo_core_integration_credentials",
}

// Install liga tudo, ou faz o processo NÃO subir: multi-empresa ligada com banco sem RLS é pior do que parada.
func Install(v1 *gin.RouterGroup, db *gorm.DB) {
	if !Ligado() {
		return
	}
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("multi-empresa: sem acesso ao *sql.DB: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if erros := Diagnosticar(ctx, db); len(erros) > 0 {
		for _, e := range erros {
			log.Printf("multi-empresa: %s", e)
		}
		log.Fatalf("multi-empresa ligada, mas o banco não está pronto (%d problema(s)); rode o instalador do Processor com --nucleo e use um papel sem superusuário", len(erros))
	}
	if err := db.Use(Plugin{}); err != nil {
		log.Fatalf("multi-empresa: registrar plugin: %v", err)
	}
	v1.Use(Middleware(sqlDB, ConsultaSQL{DB: sqlDB}))
	log.Println("multi-empresa ligada no Core")
}

// Diagnosticar devolve o que impede ligar com segurança.
func Diagnosticar(ctx context.Context, db *gorm.DB) []string {
	var erros []string
	var papel struct {
		Super, Bypass bool
		Nome          string
	}
	if err := db.WithContext(ctx).Raw(
		`SELECT rolname AS nome, rolsuper AS super, rolbypassrls AS bypass FROM pg_roles WHERE rolname = current_user`).Scan(&papel).Error; err != nil {
		return []string{fmt.Sprintf("não consegui ler o papel do banco: %v", err)}
	}
	if papel.Super || papel.Bypass {
		erros = append(erros, fmt.Sprintf("o papel '%s' é superusuário ou ignora o RLS: o isolamento não vale", papel.Nome))
	}
	for _, t := range []string{"empresas", "empresa_usuarios"} {
		var existe bool
		db.WithContext(ctx).Raw(`SELECT to_regclass(?) IS NOT NULL`, "public."+t).Scan(&existe)
		if !existe {
			erros = append(erros, fmt.Sprintf("falta a tabela '%s' (rode antes o instalador do CRM)", t))
		}
	}
	for _, t := range Tabelas {
		var ok bool
		db.WithContext(ctx).Raw(`
			SELECT c.relrowsecurity AND c.relforcerowsecurity
			   AND EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = c.oid AND p.polname = 'isolamento_por_empresa')
			FROM pg_class c WHERE c.oid = to_regclass(?)`, "public."+t).Scan(&ok)
		if !ok {
			erros = append(erros, fmt.Sprintf("'%s' sem isolamento por empresa", t))
		}
	}
	return erros
}
