package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/kagent-dev/kagent/go/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/pkg/auth"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

type ErrorResponseWriter interface {
	http.ResponseWriter
	RespondWithError(err error)
	Flush()
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
	w.Write(response) //nolint:errcheck

	log.V(2).Info("Sent JSON response", "statusCode", code, "responseSize", len(response))
}

func RespondWithError(w http.ResponseWriter, code int, message string) {
	log := ctrllog.Log.WithName("http-helpers")
	log.Info("Responding with error", "statusCode", code, "message", message)

	RespondWithJSON(w, code, map[string]string{"error": message})
}

func GetUserID(r *http.Request) (string, error) {
	p, err := GetPrincipal(r)
	return p.User.ID, err
}

func Check(authorizer auth.Authorizer, r *http.Request, res auth.Resource) *errors.APIError {
	principal, err := GetPrincipal(r)
	if err != nil {
		return errors.NewBadRequestError("Failed to get user ID", err)
	}
	var verb auth.Verb
	switch r.Method {
	case http.MethodGet:
		verb = auth.VerbGet
	case http.MethodPost:
		verb = auth.VerbCreate
	case http.MethodPut:
		verb = auth.VerbUpdate
	case http.MethodDelete:
		verb = auth.VerbDelete
	default:
		return errors.NewBadRequestError("Unsupported HTTP method", fmt.Errorf("method %s not supported", r.Method))
	}

	err = authorizer.Check(r.Context(), principal, verb, res)
	if err != nil {
		return errors.NewForbiddenError("Not authorized", err)
	}
	return nil
}

func GetPrincipal(r *http.Request) (auth.Principal, error) {
	log := ctrllog.Log.WithName("http-helpers")

	s, ok := auth.AuthSessionFrom(r.Context())
	if !ok || s == nil {
		log.Info("No session found in request context")
		return auth.Principal{}, fmt.Errorf("no session found")
	}
	log.V(2).Info("Retrieved session from request", "userID", s.Principal().User)
	return s.Principal(), nil
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
	defer r.Body.Close() //nolint:errcheck

	log.V(2).Info("Successfully decoded JSON request body")
	return nil
}

// flattenStructToMap uses reflection to add fields of a struct to a map,
// using json tags as keys.
func FlattenStructToMap(data interface{}, targetMap map[string]interface{}) {
	val := reflect.ValueOf(data)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	// Ensure it's a struct
	if val.Kind() != reflect.Struct {
		return // Or handle error appropriately
	}

	typ := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := typ.Field(i)
		fieldValue := val.Field(i)

		// Get JSON tag
		jsonTag := field.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			// Skip fields without json tags or explicitly ignored
			continue
		}

		// Handle tag options like ",omitempty"
		tagParts := strings.Split(jsonTag, ",")
		key := tagParts[0]

		// Add to map
		if fieldValue.Kind() == reflect.Ptr && fieldValue.IsNil() {
			targetMap[key] = nil
		} else {
			targetMap[key] = fieldValue.Interface()
		}
	}
}

func CreateSecret(kubeClient client.Client, name string, namespace string, data map[string]string) (*corev1.Secret, error) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		StringData: data,
	}

	if err := kubeClient.Create(context.Background(), secret); err != nil {
		return nil, err
	}
	return secret, nil
}
