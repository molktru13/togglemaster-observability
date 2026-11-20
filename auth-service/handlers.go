package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Estrutura para o corpo da requisição de criação de chave
type CreateKeyRequest struct {
	Name string `json:"name"`
}

// Estrutura para a resposta da criação de chave
type CreateKeyResponse struct {
	Name    string `json:"name"`
	Key     string `json:"key"` // A chave em texto plano é retornada APENAS uma vez
	Message string `json:"message"`
}

// healthHandler é um simples endpoint de verificação de saúde
func (a *App) healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// validateKeyHandler verifica se uma chave de API (enviada via Header) é válida
func (a *App) validateKeyHandler(w http.ResponseWriter, r *http.Request) {
	if a.failMode.Load() {
		span := trace.SpanFromContext(r.Context())
		span.SetStatus(codes.Error, "simulated internal failure")
		span.SetAttributes(attribute.Int("http.response.status_code", http.StatusInternalServerError))
		log.Printf("ERROR [auth-service] Falha interna simulada ativa na validacao de chave (HTTP 500)")
		http.Error(w, "Erro interno simulado no auth-service", http.StatusInternalServerError)
		return
	}

	// Extrai a chave do header "Authorization: Bearer <key>"
	authHeader := r.Header.Get("Authorization")
	keyString := strings.TrimPrefix(authHeader, "Bearer ")

	if keyString == "" {
		http.Error(w, "Authorization header não encontrado", http.StatusUnauthorized)
		return
	}

	// Calcula o hash da chave recebida
	keyHash := hashAPIKey(keyString)

	// Verifica se o hash existe no banco de dados
	dbCtx, dbSpan := otel.Tracer("auth-service").Start(
		r.Context(),
		"postgresql.query api_keys",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.name", "auth_db"),
			attribute.String("peer.service", "auth-db"),
		),
	)
	var id int
	err := a.DB.QueryRowContext(dbCtx, "SELECT id FROM api_keys WHERE key_hash = $1 AND is_active = true", keyHash).Scan(&id)
	if err != nil {
		dbSpan.RecordError(err)
		dbSpan.SetStatus(codes.Error, err.Error())
		dbSpan.End()
		// Se não encontrar (sql.ErrNoRows), ou qualquer outro erro, a chave é inválida
		log.Printf("Falha na validação da chave (hash: %s...): %v", keyHash[:6], err)
		http.Error(w, "Chave de API inválida ou inativa", http.StatusUnauthorized)
		return
	}
	dbSpan.End()

	// Chave válida
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Chave válida"})
}

// createKeyHandler cria uma nova chave de API
func (a *App) createKeyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
		return
	}

	var req CreateKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Corpo da requisição inválido", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "O campo 'name' é obrigatório", http.StatusBadRequest)
		return
	}

	// Gera uma nova chave e seu hash
	newKey, err := generateAPIKey()
	if err != nil {
		http.Error(w, "Erro ao gerar a chave", http.StatusInternalServerError)
		return
	}
	newKeyHash := hashAPIKey(newKey)

	// Salva o hash no banco de dados
	dbCtx, dbSpan := otel.Tracer("auth-service").Start(
		r.Context(),
		"postgresql.insert api_keys",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.name", "auth_db"),
			attribute.String("peer.service", "auth-db"),
		),
	)
	var newID int
	err = a.DB.QueryRowContext(
		dbCtx,
		"INSERT INTO api_keys (name, key_hash) VALUES ($1, $2) RETURNING id",
		req.Name, newKeyHash,
	).Scan(&newID)

	if err != nil {
		dbSpan.RecordError(err)
		dbSpan.SetStatus(codes.Error, err.Error())
		dbSpan.End()
		log.Printf("Erro ao salvar a chave no banco: %v", err)
		http.Error(w, "Erro ao salvar a chave", http.StatusInternalServerError)
		return
	}
	dbSpan.End()

	log.Printf("Nova chave criada com sucesso (ID: %d, Name: %s)", newID, req.Name)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(CreateKeyResponse{
		Name:    req.Name,
		Key:     newKey, // Retorna a chave em texto plano pela última vez
		Message: "Guarde esta chave com segurança! Você não poderá vê-la novamente.",
	})
}

// simulateFailureHandler ativa a simulacao de falhas HTTP 500 para validacao de alertas e Self-Healing
func (a *App) simulateFailureHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
		return
	}

	a.failMode.Store(true)
	log.Printf("WARN [auth-service] Modo de falha simulada ATIVADO via /admin/simulate-failure")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"fail_mode": true,
		"message":   "Modo de falha simulada ativado",
	})
}

// recoverHandler desativa o modo de falha simulada manualmente
func (a *App) recoverHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
		return
	}

	a.failMode.Store(false)
	log.Printf("INFO [auth-service] Modo de falha simulada DESATIVADO via /admin/recover")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"fail_mode": false,
		"message":   "Modo de falha simulada desativado",
	})
}

// --- Middleware ---

// masterKeyAuthMiddleware protege endpoints que só podem ser acessados com a MASTER_KEY
func (a *App) masterKeyAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		keyString := strings.TrimPrefix(authHeader, "Bearer ")

		if keyString != a.MasterKey {
			http.Error(w, "Acesso não autorizado", http.StatusForbidden)
			return
		}
		// Se a chave for válida, continua para o handler principal
		next.ServeHTTP(w, r)
	})
}
