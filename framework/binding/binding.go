// Package binding copies request data into Go structs.
//
// Fields are matched with struct tags: `query:"page"`, `path:"id"`,
// `header:"X-Token"` and `form:"name"` for the corresponding sources, and the
// usual `json:"name"` for JSON bodies. After binding, a `binding:"required"`
// tag rejects zero values.
package binding

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/searchboard/searchboard/framework/codec"
)

var (
	// ErrNotStructPointer is returned when the destination is not a non-nil pointer to a struct.
	ErrNotStructPointer = errors.New("binding: destination must be a non-nil pointer to a struct")
	// ErrRequired is wrapped by the error returned for a missing required field.
	ErrRequired = errors.New("is required")
)

// FieldError reports which field of which source could not be bound or validated.
type FieldError struct {
	Source string // "query", "path", "header", "form", "json" or "validation"
	Field  string
	Err    error
}

func (e *FieldError) Error() string {
	return fmt.Sprintf("binding: %s field %q: %v", e.Source, e.Field, e.Err)
}

func (e *FieldError) Unwrap() error { return e.Err }

// Getter returns the values for a key and whether the key was present.
type Getter func(key string) ([]string, bool)

var durationType = reflect.TypeOf(time.Duration(0))

// Bind fills the struct behind obj from get, using the struct tag named tag to
// find each field's key. Absent keys and empty values for non-string fields
// leave the field untouched, so defaults set by the caller survive.
func Bind(obj any, tag string, get Getter) error {
	value, err := structValue(obj)
	if err != nil {
		return err
	}
	return bindStruct(value, tag, get)
}

func structValue(obj any) (reflect.Value, error) {
	v := reflect.ValueOf(obj)
	if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return reflect.Value{}, ErrNotStructPointer
	}
	return v.Elem(), nil
}

func bindStruct(v reflect.Value, tag string, get Getter) error {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		// Exported fields promoted from an unexported embedded struct stay settable.
		if !field.IsExported() && !field.Anonymous {
			continue
		}

		key := tagName(field, tag)
		if key == "" {
			if field.Anonymous && field.Type.Kind() == reflect.Struct {
				if err := bindStruct(v.Field(i), tag, get); err != nil {
					return err
				}
			}
			continue
		}

		values, ok := get(key)
		if !ok || len(values) == 0 {
			continue
		}
		if err := setField(v.Field(i), values); err != nil {
			return &FieldError{Source: tag, Field: key, Err: err}
		}
	}
	return nil
}

// tagName returns the name part of a struct tag, or "" when absent or "-".
func tagName(field reflect.StructField, tag string) string {
	name, _, _ := strings.Cut(field.Tag.Get(tag), ",")
	if name == "-" {
		return ""
	}
	return name
}

func setField(v reflect.Value, values []string) error {
	switch v.Kind() {
	case reflect.Pointer:
		elem := reflect.New(v.Type().Elem())
		if err := setField(elem.Elem(), values); err != nil {
			return err
		}
		v.Set(elem)
		return nil
	case reflect.Slice:
		slice := reflect.MakeSlice(v.Type(), len(values), len(values))
		for i, raw := range values {
			if err := setScalar(slice.Index(i), raw); err != nil {
				return err
			}
		}
		v.Set(slice)
		return nil
	default:
		if values[0] == "" && v.Kind() != reflect.String {
			return nil
		}
		return setScalar(v, values[0])
	}
}

func setScalar(v reflect.Value, raw string) error {
	if v.Type() == durationType {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return fmt.Errorf("invalid duration %q", raw)
		}
		v.SetInt(int64(d))
		return nil
	}

	switch v.Kind() {
	case reflect.String:
		v.SetString(raw)
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return fmt.Errorf("invalid boolean %q", raw)
		}
		v.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, v.Type().Bits())
		if err != nil {
			return fmt.Errorf("invalid integer %q", raw)
		}
		v.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(raw, 10, v.Type().Bits())
		if err != nil {
			return fmt.Errorf("invalid unsigned integer %q", raw)
		}
		v.SetUint(n)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(raw, v.Type().Bits())
		if err != nil {
			return fmt.Errorf("invalid number %q", raw)
		}
		v.SetFloat(f)
	default:
		return fmt.Errorf("unsupported field type %s", v.Type())
	}
	return nil
}

// JSON decodes body into obj and validates it.
func JSON(body []byte, obj any) error {
	if _, err := structValue(obj); err != nil {
		return err
	}
	if err := codec.Unmarshal(body, obj); err != nil {
		return &FieldError{Source: "json", Field: "body", Err: err}
	}
	return Validate(obj)
}

// Validate enforces `binding:"required"` tags on the struct behind obj,
// descending into nested structs, pointers to structs and slices of structs.
// A required field must hold a non-zero value; use a pointer type to accept zero.
func Validate(obj any) error {
	value, err := structValue(obj)
	if err != nil {
		return err
	}
	return validateStruct(value, "")
}

func validateStruct(v reflect.Value, prefix string) error {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() && !field.Anonymous {
			continue
		}
		name := prefix + displayName(field)
		fv := v.Field(i)

		if hasRequired(field) && fv.IsZero() {
			return &FieldError{Source: "validation", Field: name, Err: ErrRequired}
		}
		if err := validateNested(fv, name); err != nil {
			return err
		}
	}
	return nil
}

func validateNested(fv reflect.Value, name string) error {
	switch fv.Kind() {
	case reflect.Struct:
		return validateStruct(fv, name+".")
	case reflect.Pointer:
		if !fv.IsNil() && fv.Elem().Kind() == reflect.Struct {
			return validateStruct(fv.Elem(), name+".")
		}
	case reflect.Slice:
		for i := 0; i < fv.Len(); i++ {
			elem := fv.Index(i)
			if elem.Kind() == reflect.Struct {
				if err := validateStruct(elem, fmt.Sprintf("%s[%d].", name, i)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func hasRequired(field reflect.StructField) bool {
	for _, rule := range strings.Split(field.Tag.Get("binding"), ",") {
		if strings.TrimSpace(rule) == "required" {
			return true
		}
	}
	return false
}

// displayName prefers the name a client sees (json, query, path, header, form).
func displayName(field reflect.StructField) string {
	for _, tag := range []string{"json", "query", "path", "header", "form"} {
		if name := tagName(field, tag); name != "" {
			return name
		}
	}
	return field.Name
}
