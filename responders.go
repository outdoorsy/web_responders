// The web_responders package takes care of our custom vendor codecs for
// Radiobox, handling responses, and even providing helpers for parsing
// input parameters.
package web_responders

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/stretchr/objx"
)

// database/sql has nullable values which all have the same prefix.
const SqlNullablePrefix = "Null"

var nullableKinds = []reflect.Kind{
	reflect.Chan,
	reflect.Func,
	reflect.Interface,
	reflect.Map,
	reflect.Ptr,
	reflect.Slice,
}

// A Constructor is a function that is called prior to data conversion
// on a data object.  It is called once per value in the response,
// starting with top-level object.  It is passed the object and
// current depth (0 for top level, 1 for fields or indexes within the
// top level object, etc).
//
// The Constructor should return two values - first, the output that
// it generates, and second, whether or not the Response logic should
// descend into the output or just leave it as-is. It is valid to
// return (data, true) if the constructor doesn't have anything it
// needs to do to the data.
//
// One example use of a Constructor: If your system is supposed to
// guarantee a certain structure for links, you can have your codec
// pass a constructor which checks (and fixes) all link structures in
// a response.
type Constructor func(data interface{}, depth int) (output interface{}, descend bool)

// A Fixer is very similar to a Constructor, except that it is called
// *after* parsing, starting with the deepest element and ending with
// the top level element.
//
// The argument passed to a Fixer will be the fully generated
// structure at the given level.  The Fixer should check for any data
// that may have been converted incorrectly for a given codec and fix
// it, then return the correct structure.
type Fixer func(data interface{}) interface{}

// A Response is a type used for storing data about and generating a
// response.  The Output() method is used for retrieving the output
// structure generated by this package.
type Response struct {
	Data        interface{}
	Constructor Constructor
	Fixer       Fixer
	Options     objx.Map

	output interface{}
}

// Output generates and returns the proper output structure for
// Response.Data, based on struct tags, interface matching, and output
// from any Constructor attached to the Response.
//
// Values which implement LazyLoader will have their LazyLoad method
// run first, in order to load any values that haven't been loaded
// yet.  The options passed to LazyLoad will be Response.Options.
//
// You can convert data when necessary by implementing the
// ResponseConverter, ResponseElementConverter, and/or
// NilElementConverter interfaces.  See the documentation for those
// types for more details.
//
// Struct values will be converted to a map[string]interface{}.  Each
// exported field's key in the map is generated using the first of the
// following methods that does not result in an empty string:
// 1. The "response" tag for that field.
// 2. The "db" tag for that field, if it is not "-"
// 3. The lowercase field name.
//
// Unexported struct fields will be ignored.
//
// A value of "-" for the "response" tag of a field will result in
// that field being skipped.
func (response *Response) Output() interface{} {
	if response.output == nil {
		response.output = response.createOutput()
	}
	return response.output
}

func (response *Response) createOutput() interface{} {
	if err, ok := response.Data.(error); ok {
		return err.Error()
	}

	return response.createResponse(response.Data, 0)
}

func (response *Response) createResponse(data interface{}, depth int) interface{} {
	if lazyLoader, ok := data.(LazyLoader); ok {
		lazyLoader.LazyLoad(response.Options)
	}

	responseData := data
	if response.Constructor != nil {
		var descend bool
		responseData, descend = response.Constructor(responseData, depth)
		if !descend {
			return responseData
		}
	}

	if converter, ok := data.(ResponseConverter); ok {
		responseData = converter.ResponseData()
	}

	switch source := responseData.(type) {
	case fmt.Stringer:
		responseData = source.String()
	case error:
		responseData = source.Error()
	}

	value := reflect.ValueOf(responseData)
	for value.Kind() == reflect.Ptr && !value.IsNil() {
		value = value.Elem()
	}

	switch value.Kind() {
	case reflect.Ptr:
		// At this point in the code, value is only a reflect.Ptr if
		// it is nil.
		responseData = nil
	case reflect.Struct:
		responseData = response.createStructResponse(value, depth)
	case reflect.Slice, reflect.Array:
		responseData = response.createSliceResponse(value, depth)
	case reflect.Map:
		responseData = response.createMapResponse(value, depth)
	}

	if response.Fixer != nil {
		responseData = response.Fixer(responseData)
	}

	return responseData
}

// createNullableDbResponse checks for "database/sql".Null* types, or
// anything with a similar structure, and pulls out the underlying
// value.  For example:
//
//     type NullInt struct {
//         Int int
//         Valid bool
//     }
//
// If Valid is false, this function will return nil; otherwise, it
// will return the value of the Int field.
func createNullableDbResponse(value reflect.Value) (interface{}, error) {
	typeName := value.Type().Name()
	if strings.HasPrefix(typeName, SqlNullablePrefix) {
		fieldName := typeName[len(SqlNullablePrefix):]
		val := value.FieldByName(fieldName)
		isNotNil := value.FieldByName("Valid")
		if val.IsValid() && isNotNil.IsValid() {
			// We've found a nullable type
			if isNotNil.Interface().(bool) {
				return val.Interface(), nil
			} else {
				return nil, nil
			}
		}
	}
	return nil, errors.New("No Nullable DB value found")
}

// createMapResponse is a helper for generating a response value from
// a value of type map.
func (response *Response) createMapResponse(value reflect.Value, depth int) interface{} {
	respMap := make(objx.Map)
	for _, key := range value.MapKeys() {
		itemResponse := response.createResponseValue(value.MapIndex(key), depth+1)
		respMap.Set(key.Interface().(string), itemResponse)
	}
	return respMap
}

// createSliceResponse is a helper for generating a response value
// from a value of type slice.
func (response *Response) createSliceResponse(value reflect.Value, depth int) interface{} {
	respSlice := make([]interface{}, value.Len())
	for i := 0; i < value.Len(); i++ {
		element := value.Index(i)
		parseFunc := response.createResponseValue
		if depth == 0 {
			parseFunc = func(value reflect.Value, depth int) interface{} {
				valueInter := value.Interface()
				if collectionConverter, ok := valueInter.(CollectionResponseConverter); ok {
					valueInter = collectionConverter.CollectionResponse()
				}
				return response.createResponse(valueInter, depth)
			}
		}
		respSlice[i] = parseFunc(element, depth+1)
	}
	return respSlice
}

// ShouldInclude is a callback function used to ask the calling code to
// determine if the field for the conditional tag should be included in the
// response.
var ShouldInclude = func(tag string) bool {
	return true
}

// createStructResponse is a helper for generating a response value
// from a value of type struct.
func (response *Response) createStructResponse(value reflect.Value, depth int) interface{} {
	// Support "database/sql".Null* types, and any other types
	// matching that structure
	if v, err := createNullableDbResponse(value); err == nil {
		return v
	}

	respMap := make(objx.Map)
	for i := 0; i < value.NumField(); i++ {
		fieldType := value.Type().Field(i)
		fieldValue := value.Field(i)

		if fieldType.Anonymous {
			embeddedResponse := response.createResponse(fieldValue.Interface(), depth).(objx.Map)
			for key, value := range embeddedResponse {
				// Don't overwrite values from the base struct
				if _, ok := respMap[key]; !ok {
					respMap[key] = value
				}
			}
			continue
		}
		name := ResponseTag(fieldType)
		switch name {
		case "-":
			continue
		default:

			cond := fieldType.Tag.Get("cond")
			shouldInclude := false
			condParts := strings.Split(cond, ",")
			for _, part := range condParts {
				shouldInclude = ShouldInclude(part)
				if shouldInclude {
					break
				}
			}
			if !shouldInclude {
				continue
			}

			if fieldType.PkgPath != "" {
				// Handle unexported fields using getters and setters, if possible.
				getterName := strings.Title(fieldType.Name)
				receiver := value
				if receiver.CanAddr() {
					// Methods on values are always callable on the pointer, as well;
					// but the opposite is not true, so always use the pointer when
					// possible.
					receiver = receiver.Addr()
				}
				getterMethod, exists := receiver.Type().MethodByName(getterName)
				if !exists || getterMethod.Type.NumIn() != 1 || getterMethod.Type.NumOut() != 1 {
					continue
				}
				fieldValue = getterMethod.Func.Call([]reflect.Value{receiver})[0]
			}
			respMap[name] = response.createResponseValue(fieldValue, depth+1)
		}
	}
	return respMap
}

// createResponseValue is a helper for generating responses from
// sub-elements of a response.
func (response *Response) createResponseValue(value reflect.Value, depth int) interface{} {
	if !value.IsValid() {
		return nil
	}
	responseValue := value.Interface()
	valueCanBeNil := false
	for _, kind := range nullableKinds {
		if value.Kind() == kind {
			valueCanBeNil = true
			break
		}
	}
	if valueCanBeNil && value.IsNil() {
		nilResponder, ok := responseValue.(NilElementConverter)
		if !ok {
			return nil
		}
		responseValue = nilResponder.NilElementData()
	}
	if converter, ok := responseValue.(ResponseElementConverter); ok {
		responseValue = converter.ResponseElementData(response.Options)
	}
	return response.createResponse(responseValue, depth)
}
