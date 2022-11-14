package utils

import (
	"fmt"
	"net/http"

	"github.com/pkg/errors"
)

type statusError int

func (s statusError) Error() string {
	return fmt.Sprintf("%d - %s", int(s), http.StatusText(int(s)))
}

func ErrorHandler(f func(w http.ResponseWriter, r *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := f(w, r); err != nil {
			cause := errors.Cause(err)
			status := http.StatusInternalServerError
			if cause, ok := cause.(statusError); ok {
				status = int(cause)
			}

			w.WriteHeader(status)
		}
	}
}
