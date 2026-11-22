package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/searchboard/searchboard/framework"
	"github.com/searchboard/searchboard/internal/domain"
	"github.com/searchboard/searchboard/internal/esclient"
)

// errorBody is the JSON shape of every error response.
type errorBody struct {
	Error string `json:"error"`
}

func writeError(c *framework.Context, status int, message string) {
	c.JSON(status, errorBody{Error: message})
}

// writeFailure maps an internal error to an HTTP response and logs the cause of
// unexpected failures. Details of upstream clusters are limited to a status code
// so credentials or internal addresses never leak into responses.
func writeFailure(c *framework.Context, log *slog.Logger, err error) {
	var apiErr *esclient.APIError

	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeError(c, http.StatusNotFound, "not found")
	case errors.Is(err, domain.ErrConflict):
		writeError(c, http.StatusConflict, "already exists")
	case errors.As(err, &apiErr):
		log.Warn("cluster returned an error", "status", apiErr.Status, "body", apiErr.Body)
		writeError(c, http.StatusBadGateway, "the cluster rejected the request with HTTP "+http.StatusText(apiErr.Status))
	default:
		log.Error("request failed", "err", err)
		writeError(c, http.StatusBadGateway, "could not complete the request against the cluster")
	}
}
