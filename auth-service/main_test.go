package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGenerateAndHashAPIKey(t *testing.T) {
	key, err := generateAPIKey()
	if err != nil {
		t.Fatalf("Erro inesperado ao gerar chave: %v", err)
	}
	if !strings.HasPrefix(key, "tm_key_") {
		t.Errorf("Esperado prefixo tm_key_, obteve: %s", key)
	}

	hash := hashAPIKey(key)
	if len(hash) != 64 {
		t.Errorf("Esperado hash SHA-256 de 64 caracteres, obteve tamanho %d", len(hash))
	}

	// Determinismo do hash
	if hashAPIKey(key) != hash {
		t.Errorf("Hash SHA-256 deve ser determinístico")
	}
}

func TestHealthHandler(t *testing.T) {
	app := &App{MasterKey: "test-master-key"}
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	app.healthHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Esperado status 200, obteve %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Errorf("Resposta inesperada no health check: %s", rec.Body.String())
	}
}

func TestMasterKeyMiddleware(t *testing.T) {
	app := &App{MasterKey: "secret-master"}
	protected := app.masterKeyAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Teste sem autorização
	reqUnauthorized := httptest.NewRequest(http.MethodPost, "/admin/keys", nil)
	recUnauthorized := httptest.NewRecorder()
	protected.ServeHTTP(recUnauthorized, reqUnauthorized)
	if recUnauthorized.Code != http.StatusForbidden {
		t.Errorf("Esperado status 403, obteve %d", recUnauthorized.Code)
	}

	// Teste com autorização válida
	reqAuthorized := httptest.NewRequest(http.MethodPost, "/admin/keys", nil)
	reqAuthorized.Header.Set("Authorization", "Bearer secret-master")
	recAuthorized := httptest.NewRecorder()
	protected.ServeHTTP(recAuthorized, reqAuthorized)
	if recAuthorized.Code != http.StatusOK {
		t.Errorf("Esperado status 200, obteve %d", recAuthorized.Code)
	}
}

func TestSimulateFailureAndRecover(t *testing.T) {
	app := &App{MasterKey: "secret-master"}

	// Ativa modo de falha via /admin/simulate-failure
	simReq := httptest.NewRequest(http.MethodPost, "/admin/simulate-failure", nil)
	simReq.Header.Set("Authorization", "Bearer secret-master")
	simRec := httptest.NewRecorder()
	app.masterKeyAuthMiddleware(http.HandlerFunc(app.simulateFailureHandler)).ServeHTTP(simRec, simReq)
	if simRec.Code != http.StatusOK || !app.failMode.Load() {
		t.Fatalf("Esperado ativacao do modo de falha (200), obteve status=%d failMode=%v", simRec.Code, app.failMode.Load())
	}

	// Durante a falha simulada, /validate deve responder HTTP 500
	valReq := httptest.NewRequest(http.MethodGet, "/validate", nil)
	valRec := httptest.NewRecorder()
	app.validateKeyHandler(valRec, valReq)
	if valRec.Code != http.StatusInternalServerError {
		t.Errorf("Esperado status 500 durante failMode, obteve %d", valRec.Code)
	}

	// Recupera via /admin/recover
	recReq := httptest.NewRequest(http.MethodPost, "/admin/recover", nil)
	recReq.Header.Set("Authorization", "Bearer secret-master")
	recRec := httptest.NewRecorder()
	app.masterKeyAuthMiddleware(http.HandlerFunc(app.recoverHandler)).ServeHTTP(recRec, recReq)
	if recRec.Code != http.StatusOK || app.failMode.Load() {
		t.Fatalf("Esperado desativacao do modo de falha (200), obteve status=%d failMode=%v", recRec.Code, app.failMode.Load())
	}

	// Apos recuperacao, requisicao sem Authorization volta a retornar 401 (nao mais 500)
	valReqAfter := httptest.NewRequest(http.MethodGet, "/validate", nil)
	valRecAfter := httptest.NewRecorder()
	app.validateKeyHandler(valRecAfter, valReqAfter)
	if valRecAfter.Code != http.StatusUnauthorized {
		t.Errorf("Esperado status 401 apos recover, obteve %d", valRecAfter.Code)
	}
}
