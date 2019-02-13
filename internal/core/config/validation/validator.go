package validation

import (
	"fmt"
	"strings"

	validator "gopkg.in/go-playground/validator.v9"
)

// Validatable should be implemented by config structs that want to provide
// validation when the config is loaded.
type Validatable interface {
	Validate() error
}

// ValidateCustomConfig for module-specific config ahead of time for a specific
// module configuration.  This way, the Configure method of modules will be
// guaranteed to receive valid configuration.  The module-specific
// configuration struct must implement the Validate method that returns a bool.
func ValidateCustomConfig(conf interface{}) error {
	if v, ok := conf.(Validatable); ok {
		return v.Validate()
	}
	return nil
}

// ValidateStruct uses the `validate` struct tags to do standard validation
func ValidateStruct(confStruct interface{}) error {
	validate := validator.New()
	err := validate.Struct(confStruct)
	if err != nil {
		return err
	}
	return nil
}

// Error wraps an error and formats it properly
type Error struct {
	error
}

func (e *Error) Error() string {
	if ves, ok := e.error.(validator.ValidationErrors); ok {
		var msgs []string
		for _, ve := range ves {
			fieldName := utils.YAMLNameOfFieldInStruct(ve.Field(), confStruct)
			msgs = append(msgs, fmt.Sprintf("Validation error in field '%s': %s", fieldName, ve.Tag()))
		}
		return strings.Join(msgs, "; ")
	}
	return e.error.Error()
}
