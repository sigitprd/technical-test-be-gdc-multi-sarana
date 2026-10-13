package handler

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-playground/validator/v10"

	"technical-test-be-gdc-multi-sarana/internal/apperror"
)

// FieldError describes one invalid request field.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Validator adapts go-playground/validator to echo.Validator. Field names are
// reported as their JSON (or query) names.
//
// Besides the built-in rules it understands:
//   - "maxbytes=N": a length limit in bytes rather than characters, needed for
//     bcrypt's 72 byte password limit;
//   - "safetext": valid UTF-8 without NUL characters. PostgreSQL rejects both
//     in text columns and parameters, so without this rule such input would
//     surface as a 500 instead of a 400.
type Validator struct {
	v *validator.Validate
}

// NewValidator creates a Validator.
func NewValidator() *Validator {
	v := validator.New(validator.WithRequiredStructEnabled())
	v.RegisterTagNameFunc(func(f reflect.StructField) string {
		for _, tag := range []string{"json", "query", "param"} {
			name, _, _ := strings.Cut(f.Tag.Get(tag), ",")
			if name != "" && name != "-" {
				return name
			}
		}
		return f.Name
	})
	for tag, fn := range map[string]validator.Func{"maxbytes": maxBytes, "safetext": safeText} {
		if err := v.RegisterValidation(tag, fn); err != nil {
			panic(err) // only fails for an invalid tag name, i.e. a programming error
		}
	}
	return &Validator{v: v}
}

func maxBytes(fl validator.FieldLevel) bool {
	limit, err := strconv.Atoi(fl.Param())
	return err == nil && len(fl.Field().String()) <= limit
}

func safeText(fl validator.FieldLevel) bool {
	s := fl.Field().String()
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

// Validate validates i and returns an *apperror.Error with field details.
func (v *Validator) Validate(i any) error {
	err := v.v.Struct(i)
	if err == nil {
		return nil
	}
	var verrs validator.ValidationErrors
	if !errors.As(err, &verrs) {
		return apperror.Internal(err)
	}
	details := make([]FieldError, 0, len(verrs))
	for _, fe := range verrs {
		details = append(details, FieldError{Field: fe.Field(), Message: validationMessage(fe)})
	}
	return apperror.Validation(details)
}

func validationMessage(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "is required"
	case "email":
		return "must be a valid email address"
	case "min":
		if fe.Param() == "1" {
			return "must not be empty"
		}
		return "must be at least " + fe.Param() + " characters"
	case "max":
		return "must be at most " + fe.Param() + " characters"
	case "maxbytes":
		return "must be at most " + fe.Param() + " bytes"
	case "safetext":
		return "must be valid UTF-8 text without NUL characters"
	case "oneof":
		return "must be one of: " + strings.ReplaceAll(fe.Param(), " ", ", ")
	case "gte":
		return "must be greater than or equal to " + fe.Param()
	case "lte":
		return "must be less than or equal to " + fe.Param()
	default:
		return "is invalid"
	}
}
