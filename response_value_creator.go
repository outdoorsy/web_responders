package web_responders

import (
	"github.com/stretchr/objx"
)

// ResponseElementConverter is a type that converts itself to a
// different structure or type when it is used as a sub-element of a
// response.  It is not used when the type is the top-level response
// data, nor when the top-level response data is a list and the type
// is included in that list.
//
// This is particularly useful for compressing a large struct to
// something much smaller when it is a field in another struct, or
// for only returning links to elements when the elements aren't
// fully populated and another database query isn't necessarily
// needed.
type ResponseElementConverter interface {
	// ResponseElementData should return the data that will be used
	// instead of the ResponseElementConverter *only* when it is a
	// sub-element of a response.
	ResponseElementData(options objx.Map) interface{}
}
