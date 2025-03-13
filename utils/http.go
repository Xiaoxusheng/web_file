package utils

import "net/http"

func Json(code int, w http.ResponseWriter, data any) {
	w.WriteHeader(code)
	w.Header().Set("Content-Type", "application/json")
	_, err := w.Write(data.([]byte))
	if err != nil {
		return
	}
}

var H = make(map[string]any)

func Success(w http.ResponseWriter, code int) {
	Json(http.StatusOK, w, map[string]any{
		"code":   code,
		"status": "success",
	})
}

func Fail(w http.ResponseWriter, code int, status string) {
	Json(http.StatusOK, w, map[string]any{
		"code":   code,
		"status": status,
	})
}
