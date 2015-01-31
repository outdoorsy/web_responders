package web_responders

import (
	"reflect"
	"strings"
)

func ResponseTag(field reflect.StructField) string {
	var name string
	if name = field.Tag.Get("response"); name != "" {
		return name
	}
	if name = field.Tag.Get("json"); name != "" && name != "-" {
		return name
	}
	return strings.ToLower(field.Name)
}
