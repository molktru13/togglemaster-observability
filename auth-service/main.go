package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	_ "github.com/jackc/pgx/v4/stdlib"
	"github.com/joho/godotenv"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// App struct (para injeção de dependência)
type App struct {
	DB        *sql.DB
	MasterKey string
	failMode  atomic.Bool
}

func main() {
	// Carrega o .env para desenvolvimento local. Em produção, isso não fará nada.
	_ = godotenv.Load()

	// --- Telemetria (OpenTelemetry) ---
	shutdownTelemetry, err := initTelemetry(context.Background(), "auth-service")
	if err != nil {
		log.Printf("Aviso: erro ao inicializar OpenTelemetry: %v", err)
	} else {
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := shutdownTelemetry(shutdownCtx); err != nil {
				log.Printf("Aviso: erro ao encerrar OpenTelemetry: %v", err)
			}
		}()
	}

	// --- Configuração ---
	port := os.Getenv("PORT")
	if port == "" {
		port = "8001" // Porta padrão
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL deve ser definida")
	}

	masterKey := os.Getenv("MASTER_KEY")
	if masterKey == "" {
		log.Fatal("MASTER_KEY deve ser definida")
	}

	// --- Conexão com o Banco ---
	db, err := connectDB(databaseURL)
	if err != nil {
		log.Fatalf("Não foi possível conectar ao banco de dados: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	app := &App{
		DB:        db,
		MasterKey: masterKey,
	}

	// --- Rotas da API ---
	mux := http.NewServeMux()
	mux.Handle("/health", otelhttp.NewHandler(http.HandlerFunc(app.healthHandler), "GET /health"))

	// Endpoint público para validar uma chave
	mux.Handle("/validate", otelhttp.NewHandler(http.HandlerFunc(app.validateKeyHandler), "GET /validate"))

	// Endpoints de "admin" para criar/gerenciar chaves e simulação de incidentes (Fase 4)
	mux.Handle("/admin/keys", otelhttp.NewHandler(app.masterKeyAuthMiddleware(http.HandlerFunc(app.createKeyHandler)), "POST /admin/keys"))
	mux.Handle("/admin/simulate-failure", otelhttp.NewHandler(app.masterKeyAuthMiddleware(http.HandlerFunc(app.simulateFailureHandler)), "POST /admin/simulate-failure"))
	mux.Handle("/admin/recover", otelhttp.NewHandler(app.masterKeyAuthMiddleware(http.HandlerFunc(app.recoverHandler)), "POST /admin/recover"))

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("Serviço de Autenticação (Go) rodando na porta %s", port)
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

// connectDB inicializa e testa a conexão com o PostgreSQL
func connectDB(databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}

	if err = db.Ping(); err != nil {
		return nil, err
	}

	log.Println("Conectado ao PostgreSQL com sucesso!")
	return db, nil
}
