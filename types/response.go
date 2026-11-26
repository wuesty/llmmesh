package types

import "reflect"

const (
	Question string = "Question"
	Result   string = "Result"
)

type JSONResponseDefinition struct {
	Name        string
	Schema      reflect.Type
	Description string
	Strict      bool
}
