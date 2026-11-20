package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/searchboard/searchboard/internal/domain"
	"github.com/searchboard/searchboard/internal/esclient"
)

// errorBody is the JSON shape of every error response.
type errorBody struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}

// writeFailure maps an internal error to an HTTP response and logs the cause of
// unexpected failures. Details of upstream clusters are limited to a status code
// so credentials or internal addresses never leak into responses.
func writeFailure(w http.ResponseWriter, log *slog.Logger, err error) {
	var apiErr *esclient.APIError

	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, domain.ErrConflict):
		writeError(w, http.StatusConflict, "already exists")
	case errors.As(err, &apiErr):
		log.Warn("cluster returned an error", "status", apiErr.Status, "body", apiErr.Body)
		writeError(w, http.StatusBadGateway, "the cluster rejected the request with HTTP "+http.StatusText(apiErr.Status))
	default:
		log.Error("request failed", "err", err)
		writeError(w, http.StatusBadGateway, "could not complete the request against the cluster")
	}
}
