package web_responders

// PreMarshaller should be used for types that need to do some work before
// the response is encoded.
//
// Example:
//
//     type Example struct {
//         data string
//     }
//
//     func (e *Example) PreMarshal() {
//         e.data = "Hello!"
//     }
type PreMarshaller interface {
	// PreMarshal should do any work on the object that needs to happen
	// before the response is encoded.
	PreMarshal()
}
