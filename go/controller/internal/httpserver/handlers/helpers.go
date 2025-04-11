package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

type ErrorResponseWriter interface {
	http.ResponseWriter
	RespondWithError(err error)
}

func RespondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	log := ctrllog.Log.WithName("http-helpers")

	response, err := json.Marshal(payload)
	if err != nil {
		log.Error(err, "Error marshalling JSON response")
		RespondWithError(w, http.StatusInternalServerError, "Error marshalling JSON response")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)

	log.V(2).Info("Sent JSON response", "statusCode", code, "responseSize", len(response))
}

func RespondWithError(w http.ResponseWriter, code int, message string) {
	log := ctrllog.Log.WithName("http-helpers")
	log.Info("Responding with error", "statusCode", code, "message", message)

	RespondWithJSON(w, code, map[string]string{"error": message})
}

func GetUserID(r *http.Request) (string, error) {
	log := ctrllog.Log.WithName("http-helpers")

	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		log.Info("Missing user_id parameter in request")
		return "", fmt.Errorf("user_id is required")
	}

	log.V(2).Info("Retrieved user_id from request", "userID", userID)
	return userID, nil
}

// GetPathParam gets a path parameter from the request
func GetPathParam(r *http.Request, name string) (string, error) {
	log := ctrllog.Log.WithName("http-helpers")

	vars := mux.Vars(r)
	value, ok := vars[name]
	if !ok || value == "" {
		log.Info("Missing required path parameter", "paramName", name)
		return "", fmt.Errorf("%s is required", name)
	}

	log.V(2).Info("Retrieved path parameter", "paramName", name, "value", value)
	return value, nil
}

// GetIntPathParam gets an integer path parameter from the request
func GetIntPathParam(r *http.Request, name string) (int, error) {
	log := ctrllog.Log.WithName("http-helpers")

	strValue, err := GetPathParam(r, name)
	if err != nil {
		return 0, err
	}

	intValue, err := strconv.Atoi(strValue)
	if err != nil {
		log.Info("Invalid integer path parameter", "paramName", name, "value", strValue)
		return 0, fmt.Errorf("invalid %s: must be an integer", name)
	}

	log.V(2).Info("Retrieved integer path parameter", "paramName", name, "value", intValue)
	return intValue, nil
}

// DecodeJSONBody decodes a JSON request body into the provided struct
func DecodeJSONBody(r *http.Request, target interface{}) error {
	log := ctrllog.Log.WithName("http-helpers")

	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		log.Info("Failed to decode JSON request body", "error", err.Error())
		return err
	}
	defer r.Body.Close()

	log.V(2).Info("Successfully decoded JSON request body")
	return nil
}
