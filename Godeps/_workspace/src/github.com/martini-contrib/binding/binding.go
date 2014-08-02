// Package binding transforms, with validation, a raw request into
// a populated structure used by your application logic.
package binding

import (
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/go-martini/martini"
)

/*
	To the land of Middle-ware Earth:
		One func to rule them all,
		One func to find them,
		One func to bring them all,
		And in this package BIND them.
*/

// Bind accepts a copy of an empty struct and populates it with
// values from the request (if deserialization is successful). It
// wraps up the functionality of the Form and Json middleware
// according to the Content-Type and verb of the request.
// A Content-Type is required for POST and PUT requests.
// Bind invokes the ErrorHandler middleware to bail out if errors
// occurred. If you want to perform your own error handling, use
// Form or Json middleware directly. An interface pointer can
// be added as a second argument in order to map the struct to
// a specific interface.
func Bind(obj interface{}, ifacePtr ...interface{}) martini.Handler {
	return func(context martini.Context, req *http.Request) {
		contentType := req.Header.Get("Content-Type")

		if req.Method == "POST" || req.Method == "PUT" || contentType != "" {
			if strings.Contains(contentType, "form-urlencoded") {
				context.Invoke(Form(obj, ifacePtr...))
			} else if strings.Contains(contentType, "multipart/form-data") {
				context.Invoke(MultipartForm(obj, ifacePtr...))
			} else if strings.Contains(contentType, "json") {
				context.Invoke(Json(obj, ifacePtr...))
			} else {
				errors := newErrors()
				if contentType == "" {
					errors.Overall[ContentTypeError] = "Empty Content-Type"
				} else {
					errors.Overall[ContentTypeError] = "Unsupported Content-Type"
				}
				context.Map(*errors)
			}
		} else {
			context.Invoke(Form(obj, ifacePtr...))
		}

		context.Invoke(ErrorHandler)
	}
}

// Form is middleware to deserialize form-urlencoded data from the request.
// It gets data from the form-urlencoded body, if present, or from the
// query string. It uses the http.Request.ParseForm() method
// to perform deserialization, then reflection is used to map each field
// into the struct with the proper type. Structs with primitive slice types
// (bool, float, int, string) can support deserialization of repeated form
// keys, for example: key=val1&key=val2&key=val3
// An interface pointer can be added as a second argument in order
// to map the struct to a specific interface.
func Form(formStruct interface{}, ifacePtr ...interface{}) martini.Handler {
	return func(context martini.Context, req *http.Request) {
		ensureNotPointer(formStruct)
		formStruct := reflect.New(reflect.TypeOf(formStruct))
		errors := newErrors()
		parseErr := req.ParseForm()

		// Format validation of the request body or the URL would add considerable overhead,
		// and ParseForm does not complain when URL encoding is off.
		// Because an empty request body or url can also mean absence of all needed values,
		// it is not in all cases a bad request, so let's return 422.
		if parseErr != nil {
			errors.Overall[DeserializationError] = parseErr.Error()
		}

		mapForm(formStruct, req.Form, nil, errors)

		validateAndMap(formStruct, context, errors, ifacePtr...)
	}
}

func MultipartForm(formStruct interface{}, ifacePtr ...interface{}) martini.Handler {
	return func(context martini.Context, req *http.Request) {
		ensureNotPointer(formStruct)
		formStruct := reflect.New(reflect.TypeOf(formStruct))
		errors := newErrors()

		// Workaround for multipart forms returning nil instead of an error
		// when content is not multipart
		// https://code.google.com/p/go/issues/detail?id=6334
		multipartReader, err := req.MultipartReader()
		if err != nil {
			errors.Overall[DeserializationError] = err.Error()
		} else {
			form, parseErr := multipartReader.ReadForm(MaxMemory)

			if parseErr != nil {
				errors.Overall[DeserializationError] = parseErr.Error()
			}

			req.MultipartForm = form
		}

		mapForm(formStruct, req.MultipartForm.Value, req.MultipartForm.File, errors)

		validateAndMap(formStruct, context, errors, ifacePtr...)
	}
}

// Json is middleware to deserialize a JSON payload from the request
// into the struct that is passed in. The resulting struct is then
// validated, but no error handling is actually performed here.
// An interface pointer can be added as a second argument in order
// to map the struct to a specific interface.
func Json(jsonStruct interface{}, ifacePtr ...interface{}) martini.Handler {
	return func(context martini.Context, req *http.Request) {
		ensureNotPointer(jsonStruct)
		jsonStruct := reflect.New(reflect.TypeOf(jsonStruct))
		errors := newErrors()

		if req.Body != nil {
			defer req.Body.Close()
		}

		if err := json.NewDecoder(req.Body).Decode(jsonStruct.Interface()); err != nil && err != io.EOF {
			errors.Overall[DeserializationError] = err.Error()
		}

		validateAndMap(jsonStruct, context, errors, ifacePtr...)
	}
}

// Validate is middleware to enforce required fields. If the struct
// passed in is a Validator, then the user-defined Validate method
// is executed, and its errors are mapped to the context. This middleware
// performs no error handling: it merely detects them and maps them.
func Validate(obj interface{}) martini.Handler {
	return func(context martini.Context, req *http.Request) {
		errors := newErrors()
		validateStruct(errors, obj)

		if validator, ok := obj.(Validator); ok {
			validator.Validate(errors, req)
		}
		context.Map(*errors)

	}
}

func validateStruct(errors *Errors, obj interface{}) {
	typ := reflect.TypeOf(obj)
	val := reflect.ValueOf(obj)

	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
		val = val.Elem()
	}

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)

		// Allow ignored fields in the struct
		if field.Tag.Get("form") == "-" {
			continue
		}

		fieldValue := val.Field(i).Interface()
		zero := reflect.Zero(field.Type).Interface()

		if strings.Index(field.Tag.Get("binding"), "required") > -1 {
			if field.Type.Kind() == reflect.Struct {
				validateStruct(errors, fieldValue)
			} else if reflect.DeepEqual(zero, fieldValue) {
				name := field.Name
				if j := field.Tag.Get("json"); j != "" {
					name = j
				} else if f := field.Tag.Get("form"); f != "" {
					name = f
				}
				errors.Fields[name] = RequireError
			}
		}
	}
}

func mapForm(formStruct reflect.Value, form map[string][]string, formfile map[string][]*multipart.FileHeader, errors *Errors) {
	if formStruct.Kind() == reflect.Ptr {
		formStruct = formStruct.Elem()
	}
	typ := formStruct.Type()

	for i := 0; i < typ.NumField(); i++ {
		typeField := typ.Field(i)
		structField := formStruct.Field(i)

		if typeField.Type.Kind() == reflect.Struct {
			mapForm(structField, form, formfile, errors)
		} else if inputFieldName := typeField.Tag.Get("form"); inputFieldName != "" {
			if !structField.CanSet() {
				continue
			}

			inputValue, exists := form[inputFieldName]
			if exists {
				numElems := len(inputValue)
				if structField.Kind() == reflect.Slice && numElems > 0 {
					sliceOf := structField.Type().Elem().Kind()
					slice := reflect.MakeSlice(structField.Type(), numElems, numElems)
					for i := 0; i < numElems; i++ {
						setWithProperType(sliceOf, inputValue[i], slice.Index(i), inputFieldName, errors)
					}
					formStruct.Field(i).Set(slice)
				} else {
					setWithProperType(typeField.Type.Kind(), inputValue[0], structField, inputFieldName, errors)
				}
				continue
			}

			inputFile, exists := formfile[inputFieldName]
			if !exists {
				continue
			}
			fhType := reflect.TypeOf((*multipart.FileHeader)(nil))
			numElems := len(inputFile)
			if structField.Kind() == reflect.Slice && numElems > 0 && structField.Type().Elem() == fhType {
				slice := reflect.MakeSlice(structField.Type(), numElems, numElems)
				for i := 0; i < numElems; i++ {
					slice.Index(i).Set(reflect.ValueOf(inputFile[i]))
				}
				structField.Set(slice)
			} else if structField.Type() == fhType {
				structField.Set(reflect.ValueOf(inputFile[0]))
			}
		}
	}
}

// ErrorHandler simply counts the number of errors in the
// context and, if more than 0, writes a 400 Bad Request
// response and a JSON payload describing the errors with
// the "Content-Type" set to "application/json".
// Middleware remaining on the stack will not even see the request
// if, by this point, there are any errors.
// This is a "default" handler, of sorts, and you are
// welcome to use your own instead. The Bind middleware
// invokes this automatically for convenience.
func ErrorHandler(errs Errors, resp http.ResponseWriter) {
	if errs.Count() > 0 {
		resp.Header().Set("Content-Type", "application/json; charset=utf-8")
		if _, ok := errs.Overall[DeserializationError]; ok {
			resp.WriteHeader(http.StatusBadRequest)
		} else if _, ok := errs.Overall[ContentTypeError]; ok {
			resp.WriteHeader(http.StatusUnsupportedMediaType)
		} else {
			resp.WriteHeader(StatusUnprocessableEntity)
		}
		errOutput, _ := json.Marshal(errs)
		resp.Write(errOutput)
		return
	}
}

// This sets the value in a struct of an indeterminate type to the
// matching value from the request (via Form middleware) in the
// same type, so that not all deserialized values have to be strings.
// Supported types are string, int, float, and bool.
func setWithProperType(valueKind reflect.Kind, val string, structField reflect.Value, nameInTag string, errors *Errors) {
	switch valueKind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if val == "" {
			val = "0"
		}
		intVal, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			errors.Fields[nameInTag] = IntegerTypeError
		} else {
			structField.SetInt(intVal)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if val == "" {
			val = "0"
		}
		uintVal, err := strconv.ParseUint(val, 10, 64)
		if err != nil {
			errors.Fields[nameInTag] = IntegerTypeError
		} else {
			structField.SetUint(uintVal)
		}
	case reflect.Bool:
		if val == "" {
			val = "false"
		}
		boolVal, err := strconv.ParseBool(val)
		if err != nil {
			errors.Fields[nameInTag] = BooleanTypeError
		} else {
			structField.SetBool(boolVal)
		}
	case reflect.Float32:
		if val == "" {
			val = "0.0"
		}
		floatVal, err := strconv.ParseFloat(val, 32)
		if err != nil {
			errors.Fields[nameInTag] = FloatTypeError
		} else {
			structField.SetFloat(floatVal)
		}
	case reflect.Float64:
		if val == "" {
			val = "0.0"
		}
		floatVal, err := strconv.ParseFloat(val, 64)
		if err != nil {
			errors.Fields[nameInTag] = FloatTypeError
		} else {
			structField.SetFloat(floatVal)
		}
	case reflect.String:
		structField.SetString(val)
	}
}

// Don't pass in pointers to bind to. Can lead to bugs. See:
// https://github.com/codegangsta/martini-contrib/issues/40
// https://github.com/codegangsta/martini-contrib/pull/34#issuecomment-29683659
func ensureNotPointer(obj interface{}) {
	if reflect.TypeOf(obj).Kind() == reflect.Ptr {
		panic("Pointers are not accepted as binding models")
	}
}

// Performs validation and combines errors from validation
// with errors from deserialization, then maps both the
// resulting struct and the errors to the context.
func validateAndMap(obj reflect.Value, context martini.Context, errors *Errors, ifacePtr ...interface{}) {
	context.Invoke(Validate(obj.Interface()))
	errors.combine(getErrors(context))
	context.Map(*errors)
	context.Map(obj.Elem().Interface())
	if len(ifacePtr) > 0 {
		context.MapTo(obj.Elem().Interface(), ifacePtr[0])
	}
}

func newErrors() *Errors {
	return &Errors{make(map[string]string), make(map[string]string)}
}

func getErrors(context martini.Context) Errors {
	return context.Get(reflect.TypeOf(Errors{})).Interface().(Errors)
}

func (this *Errors) combine(other Errors) {
	for key, val := range other.Fields {
		if _, exists := this.Fields[key]; !exists {
			this.Fields[key] = val
		}
	}
	for key, val := range other.Overall {
		if _, exists := this.Overall[key]; !exists {
			this.Overall[key] = val
		}
	}
}

// Total errors is the sum of errors with the request overall
// and errors on individual fields.
func (self Errors) Count() int {
	return len(self.Overall) + len(self.Fields)
}

type (
	// Errors represents the contract of the response body when the
	// binding step fails before getting to the application.
	Errors struct {
		Overall map[string]string `json:"overall"`
		Fields  map[string]string `json:"fields"`
	}

	// Implement the Validator interface to define your own input
	// validation before the request even gets to your application.
	// The Validate method will be executed during the validation phase.
	Validator interface {
		Validate(*Errors, *http.Request)
	}
)

var (
	// Maximum amount of memory to use when parsing a multipart form.
	// Set this to whatever value you prefer; default is 10 MB.
	MaxMemory = int64(1024 * 1024 * 10)
)

const (
	RequireError         string = "Required"
	ContentTypeError     string = "ContentTypeError"
	DeserializationError string = "DeserializationError"
	IntegerTypeError     string = "IntegerTypeError"
	BooleanTypeError     string = "BooleanTypeError"
	FloatTypeError       string = "FloatTypeError"

	StatusUnprocessableEntity int = 422
)
