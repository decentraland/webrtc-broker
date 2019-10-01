// Package protocol contains the communications protocol protobuffer definition
package protocol

import "github.com/golang/protobuf/proto"

// Message convenience re-export
type Message = proto.Message

// IMarshaller reprensents the marhsall interface
type IMarshaller interface {
	Marshal(Message) ([]byte, error)
	Unmarshal([]byte, Message) error
}

// Marshaller is the default marshaller
type Marshaller struct{}

// Marshal a message into a byte array
func (*Marshaller) Marshal(msg Message) ([]byte, error) {
	return proto.Marshal(msg)
}

// Unmarshal a byte array into the given message
func (*Marshaller) Unmarshal(bytes []byte, msg Message) error {
	return proto.Unmarshal(bytes, msg)
}
