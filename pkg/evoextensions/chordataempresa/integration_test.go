//go:build integration

// Prova, com Postgres de verdade e papel comum (NOSUPERUSER NOBYPASSRLS), que a empresa decidida pelo
// middleware chega ao banco pelo GORM. Banco DESCARTÁVEL com o esquema do CRM isolado e o grupo --nucleo
// do instalador do Processor já instalado:
//
//	CORE_TEST_ADMIN_URL=postgres://postgres:...@host/evo_processor_tests?sslmode=disable
//	CORE_TEST_APP_URL=postgres://evo_proc_teste_app:...@host/evo_processor_tests?sslmode=disable
//	go test -tags integration ./pkg/evoextensions/chordataempresa/
package chordataempresa

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"evo-ai-core-service/internal/types"
	agentmodel "evo-ai-core-service/pkg/agent/model"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func abrir(t *testing.T) (*gorm.DB, *sql.DB, *sql.DB) {
	t.Helper()
	admin, app := os.Getenv("CORE_TEST_ADMIN_URL"), os.Getenv("CORE_TEST_APP_URL")
	if admin == "" || app == "" {
		t.Skip("defina CORE_TEST_ADMIN_URL e CORE_TEST_APP_URL")
	}
	adm, err := sql.Open("postgres", admin)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { adm.Close() })
	db, err := gorm.Open(postgres.Open(app), &gorm.Config{PrepareStmt: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1) // uma conexão: se a empresa vazasse entre transações, o teste pegaria
	t.Cleanup(func() { sqlDB.Close() })
	return db, sqlDB, adm
}

func preparar(t *testing.T, adm *sql.DB) {
	t.Helper()
	passos := []string{
		`TRUNCATE evo_core_agents CASCADE`,
		`INSERT INTO empresas (nome, slug) VALUES ('Empresa padrão', 'padrao') ON CONFLICT (slug) DO NOTHING`,
		`INSERT INTO empresas (id, nome, slug) VALUES ('` + empA + `', 'A', 'a'), ('` + empB + `', 'B', 'b') ON CONFLICT DO NOTHING`,
		`INSERT INTO users (id, email, uid, name) VALUES ('` + usuario1 + `', 'u1@t', 'u1@t', 'U1'), ('` + usuario2 + `', 'u2@t', 'u2@t', 'U2') ON CONFLICT DO NOTHING`,
		`DELETE FROM empresa_usuarios WHERE user_id IN ('` + usuario1 + `', '` + usuario2 + `')`,
		`INSERT INTO empresa_usuarios (empresa_id, user_id) VALUES ('` + empA + `', '` + usuario1 + `'), ('` + empA + `', '` + usuario2 + `'), ('` + empB + `', '` + usuario2 + `')`,
	}
	for _, p := range passos {
		if _, err := adm.Exec(p); err != nil {
			t.Fatalf("preparo falhou (%s): %v", p, err)
		}
	}
}

// servidor monta: recovery -> "login" falso (user_id/role no contexto) -> middleware -> rotas.
func servidor(db *gorm.DB, sqlDB *sql.DB) *gin.Engine {
	gin.SetMode(gin.TestMode)
	if err := db.Use(Plugin{}); err != nil && !strings.Contains(err.Error(), "already") {
		panic(err)
	}
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(func(c *gin.Context) {
		ctx := c.Request.Context()
		if u := c.GetHeader("X-Teste-User"); u != "" {
			id := uuid.MustParse(u)
			ctx = context.WithValue(ctx, "user_id", id)
			ctx = context.WithValue(ctx, "role", &types.Role{Key: c.GetHeader("X-Teste-Role")})
		}
		c.Request = c.Request.WithContext(ctx)
	})
	r.Use(Middleware(sqlDB, ConsultaSQL{DB: sqlDB}))
	r.GET("/agentes", func(c *gin.Context) {
		var ags []agentmodel.Agent
		if err := db.WithContext(c.Request.Context()).Order("name").Find(&ags).Error; err != nil {
			c.String(500, err.Error())
			return
		}
		nomes := make([]string, 0, len(ags))
		for _, a := range ags {
			nomes = append(nomes, a.Name)
		}
		c.String(200, strings.Join(nomes, ","))
	})
	r.POST("/agentes/:nome", func(c *gin.Context) {
		err := db.WithContext(c.Request.Context()).Exec(
			`INSERT INTO evo_core_agents (name, type, card_url) VALUES (?, 'llm', 'http://x')`, c.Param("nome")).Error
		if err != nil {
			c.String(409, err.Error())
			return
		}
		c.Status(201)
	})
	// duplica de propósito, engole o erro e segue gravando: o que valia antes (cada instrução confirmava sozinha)
	r.POST("/tolerante", func(c *gin.Context) {
		ctx := c.Request.Context()
		_ = db.WithContext(ctx).Exec(`INSERT INTO evo_core_agents (name, type, card_url) VALUES ('atendente', 'llm', 'http://x')`).Error
		if err := db.WithContext(ctx).Exec(`INSERT INTO evo_core_agents (name, type, card_url) VALUES ('depois-do-erro', 'llm', 'http://x')`).Error; err != nil {
			c.String(500, err.Error())
			return
		}
		c.Status(201)
	})
	r.POST("/pânico", func(c *gin.Context) {
		db.WithContext(c.Request.Context()).Exec(`INSERT INTO evo_core_agents (name, type, card_url) VALUES ('fantasma', 'llm', 'http://x')`)
		panic("falha no handler")
	})
	return r
}

func chamar(r *gin.Engine, metodo, caminho, user, papel, empresa string) (int, string) {
	req := httptest.NewRequest(metodo, caminho, nil)
	if user != "" {
		req.Header.Set("X-Teste-User", user)
		req.Header.Set("X-Teste-Role", papel)
	}
	if empresa != "" {
		req.Header.Set(Header, empresa)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

func TestIsolamentoPeloGORM(t *testing.T) {
	db, sqlDB, adm := abrir(t)
	preparar(t, adm)
	r := servidor(db, sqlDB)

	if erros := Diagnosticar(context.Background(), db); len(erros) != 0 {
		t.Fatalf("diagnóstico: %v", erros)
	}

	// u2 cria "atendente" em A e em B: o mesmo nome em empresas diferentes é permitido
	for _, emp := range []string{empA, empB} {
		if code, corpo := chamar(r, "POST", "/agentes/atendente", usuario2, "agent", emp); code != 201 {
			t.Fatalf("criar em %s: %d %s", emp, code, corpo)
		}
	}
	if code, _ := chamar(r, "POST", "/agentes/atendente", usuario2, "agent", empA); code != 409 {
		t.Fatalf("nome repetido na mesma empresa deveria falhar, veio %d", code)
	}
	if code, _ := chamar(r, "POST", "/agentes/so-da-B", usuario2, "agent", empB); code != 201 {
		t.Fatal("criar só-da-B")
	}

	// erro de SQL engolido pelo handler não derruba o resto da requisição (savepoint por escrita)
	if code, corpo := chamar(r, "POST", "/tolerante", usuario1, "agent", ""); code != 201 {
		t.Fatalf("handler tolerante: %d %s", code, corpo)
	}
	if code, corpo := chamar(r, "GET", "/agentes", usuario1, "agent", ""); code != 200 || corpo != "atendente,depois-do-erro" {
		t.Fatalf("u1 em A após tolerante: %d %q", code, corpo)
	}
	if _, err := adm.Exec(`DELETE FROM evo_core_agents WHERE name = 'depois-do-erro'`); err != nil {
		t.Fatal(err)
	}

	// u1 (só A) lê só a A, sem precisar do cabeçalho
	if code, corpo := chamar(r, "GET", "/agentes", usuario1, "agent", ""); code != 200 || corpo != "atendente" {
		t.Fatalf("u1 em A: %d %q", code, corpo)
	}
	// u2 escolhe B
	if code, corpo := chamar(r, "GET", "/agentes", usuario2, "agent", empB); code != 200 || corpo != "atendente,so-da-B" {
		t.Fatalf("u2 em B: %d %q", code, corpo)
	}
	if code, corpo := chamar(r, "GET", "/agentes", usuario2, "agent", empA); code != 200 || corpo != "atendente" {
		t.Fatalf("u2 em A: %d %q", code, corpo)
	}
	// regras de acesso
	if code, _ := chamar(r, "GET", "/agentes", usuario1, "agent", empB); code != 403 {
		t.Fatalf("u1 pedindo B deveria ser 403, veio %d", code)
	}
	if code, _ := chamar(r, "GET", "/agentes", usuario2, "agent", ""); code != 400 {
		t.Fatalf("u2 sem cabeçalho deveria ser 400, veio %d", code)
	}
	if code, _ := chamar(r, "GET", "/agentes", "", "", ""); code != 401 {
		t.Fatalf("sem usuário deveria ser 401, veio %d", code)
	}
	if code, corpo := chamar(r, "GET", "/agentes", "cccccccc-cccc-cccc-cccc-cccccccccccc", "super_admin", empB); code != 200 || corpo != "atendente,so-da-B" {
		t.Fatalf("super_admin em B: %d %q", code, corpo)
	}

	// fora de uma requisição (pool, sem empresa): o banco não devolve nada
	var n int64
	if err := db.Model(&agentmodel.Agent{}).Count(&n).Error; err != nil || n != 0 {
		t.Fatalf("pool sem empresa deveria ver 0, viu %d (%v)", n, err)
	}

	// pânico no handler desfaz a transação da requisição
	func() {
		defer func() { _ = recover() }()
		chamar(r, "POST", "/pânico", usuario1, "agent", "")
	}()
	if code, corpo := chamar(r, "GET", "/agentes", usuario1, "agent", ""); code != 200 || strings.Contains(corpo, "fantasma") {
		t.Fatalf("o insert do handler com pânico deveria ter sido desfeito: %d %q", code, corpo)
	}

	// o que foi confirmado está no banco (conferido como dono, sem RLS)
	var total int
	if err := adm.QueryRow(`SELECT count(*) FROM evo_core_agents`).Scan(&total); err != nil || total != 3 {
		t.Fatalf("esperava 3 agentes no total, há %d (%v)", total, err)
	}
	_ = http.StatusOK
}
