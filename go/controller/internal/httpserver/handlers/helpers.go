package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

// Common HTTP response helpers
func RespondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, err := json.Marshal(payload)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "Error marshalling JSON response")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}

func RespondWithError(w http.ResponseWriter, code int, message string) {
	RespondWithJSON(w, code, map[string]string{"error": message})
}

func GetUserID(r *http.Request) (string, error) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		return "", fmt.Errorf("user_id is required")
	}
	return userID, nil
}

// GetPathParam gets a path parameter from the request
func GetPathParam(r *http.Request, name string) (string, error) {
	vars := mux.Vars(r)
	value, ok := vars[name]
	if !ok || value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}

// GetIntPathParam gets an integer path parameter from the request
func GetIntPathParam(r *http.Request, name string) (int, error) {
	strValue, err := GetPathParam(r, name)
	if err != nil {
		return 0, err
	}

	intValue, err := strconv.Atoi(strValue)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: must be an integer", name)
	}

	return intValue, nil
}

// DecodeJSONBody decodes a JSON request body into the provided struct
func DecodeJSONBody(r *http.Request, target interface{}) error {
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		return err
	}
	defer r.Body.Close()
	return nil
}
