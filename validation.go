package main

import (
	"fmt"
)

type ValidationError struct {
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

func (v ValidationError) Error() string {
	return fmt.Sprintf("validation error: %s %s", v.Field, v.Message)
}
