package handlers

import (
	"encoding/json"
	"net/http"

	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

type ErrorResponseWriter interface {
	http.ResponseWriter
	RespondWithError(err error)
	Flush()
}

func RespondWithJSON(w http.ResponseWriter, code int, payload any) {
	log := ctrllog.Log.WithName("http-helpers")

	response, err := json.Marshal(payload)
	if err != nil {
		log.Error(err, "Error marshalling JSON response")
		RespondWithError(w, http.StatusInternalServerError, "Error marshalling JSON response")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response) //nolint:errcheck

	log.V(2).Info("Sent JSON response", "statusCode", code, "responseSize", len(response))
}

func RespondWithError(w http.ResponseWriter, code int, message string) {
	log := ctrllog.Log.WithName("http-helpers")
	log.Info("Responding with error", "statusCode", code, "message", message)

	RespondWithJSON(w, code, map[string]string{"error": message})
}
