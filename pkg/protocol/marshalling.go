package protocol

import "github.com/golang/protobuf/proto"

type Message = proto.Message

type IMarshaller interface {
	Marshal(Message) ([]byte, error)
	Unmarshal([]byte, Message) error
}

type Marshaller struct{}

func (*Marshaller) Marshal(msg Message) ([]byte, error) {
	return proto.Marshal(msg)
}

func (*Marshaller) Unmarshal(bytes []byte, msg Message) error {
	return proto.Unmarshal(bytes, msg)
}
