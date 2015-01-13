package httphelper

import (
	"encoding/json"
	"log"
	"net/http"
)

func Error(w http.ResponseWriter, err error) {
	switch err.(type) {
	case *json.SyntaxError, *json.UnmarshalTypeError:
		JSON(w, 400, "The provided JSON input is invalid")
	default:
		log.Println(err)
		JSON(w, 500, struct{}{})
	}
}

func JSON(w http.ResponseWriter, status int, v interface{}) {
	var result []byte
	var err error
	result, err = json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(result)
}
