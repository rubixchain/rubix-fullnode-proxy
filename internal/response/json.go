package response

import (
	"net/http"

	"rubix-fullnode-proxy/internal/constants"
)

func JSON(w http.ResponseWriter, statusCode int, body string) {
	w.Header().Set(constants.HeaderContentType, constants.ContentTypeJSON)
	w.WriteHeader(statusCode)
	w.Write([]byte(body))
}
