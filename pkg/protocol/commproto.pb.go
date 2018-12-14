// Code generated by protoc-gen-go. DO NOT EDIT.
// source: commproto.proto

package commproto

import (
	fmt "fmt"
	math "math"

	proto "github.com/golang/protobuf/proto"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.ProtoPackageIsVersion2 // please upgrade the proto package

type MessageType int32

const (
	MessageType_UNKNOWN_MESSAGE_TYPE MessageType = 0
	MessageType_WELCOME_SERVER       MessageType = 1
	MessageType_WELCOME_CLIENT       MessageType = 2
	MessageType_CONNECT              MessageType = 3
	MessageType_WEBRTC_SUPPORTED     MessageType = 4
	MessageType_WEBRTC_OFFER         MessageType = 5
	MessageType_WEBRTC_ANSWER        MessageType = 6
	MessageType_WEBRTC_ICE_CANDIDATE MessageType = 7
	MessageType_PING                 MessageType = 8
	MessageType_ADD_TOPIC            MessageType = 9
	MessageType_REMOVE_TOPIC         MessageType = 10
	MessageType_TOPIC                MessageType = 11
	MessageType_AUTH                 MessageType = 12
)

var MessageType_name = map[int32]string{
	0:  "UNKNOWN_MESSAGE_TYPE",
	1:  "WELCOME_SERVER",
	2:  "WELCOME_CLIENT",
	3:  "CONNECT",
	4:  "WEBRTC_SUPPORTED",
	5:  "WEBRTC_OFFER",
	6:  "WEBRTC_ANSWER",
	7:  "WEBRTC_ICE_CANDIDATE",
	8:  "PING",
	9:  "ADD_TOPIC",
	10: "REMOVE_TOPIC",
	11: "TOPIC",
	12: "AUTH",
}

var MessageType_value = map[string]int32{
	"UNKNOWN_MESSAGE_TYPE": 0,
	"WELCOME_SERVER":       1,
	"WELCOME_CLIENT":       2,
	"CONNECT":              3,
	"WEBRTC_SUPPORTED":     4,
	"WEBRTC_OFFER":         5,
	"WEBRTC_ANSWER":        6,
	"WEBRTC_ICE_CANDIDATE": 7,
	"PING":                 8,
	"ADD_TOPIC":            9,
	"REMOVE_TOPIC":         10,
	"TOPIC":                11,
	"AUTH":                 12,
}

func (x MessageType) String() string {
	return proto.EnumName(MessageType_name, int32(x))
}

func (MessageType) EnumDescriptor() ([]byte, []int) {
	return fileDescriptor_6f0b7582b0652215, []int{0}
}

type Role int32

const (
	Role_UNKNOWN_ROLE         Role = 0
	Role_CLIENT               Role = 1
	Role_COMMUNICATION_SERVER Role = 2
)

var Role_name = map[int32]string{
	0: "UNKNOWN_ROLE",
	1: "CLIENT",
	2: "COMMUNICATION_SERVER",
}

var Role_value = map[string]int32{
	"UNKNOWN_ROLE":         0,
	"CLIENT":               1,
	"COMMUNICATION_SERVER": 2,
}

func (x Role) String() string {
	return proto.EnumName(Role_name, int32(x))
}

func (Role) EnumDescriptor() ([]byte, []int) {
	return fileDescriptor_6f0b7582b0652215, []int{1}
}

type CoordinatorMessage struct {
	Type                 MessageType `protobuf:"varint,1,opt,name=type,proto3,enum=MessageType" json:"type,omitempty"`
	XXX_NoUnkeyedLiteral struct{}    `json:"-"`
	XXX_unrecognized     []byte      `json:"-"`
	XXX_sizecache        int32       `json:"-"`
}

func (m *CoordinatorMessage) Reset()         { *m = CoordinatorMessage{} }
func (m *CoordinatorMessage) String() string { return proto.CompactTextString(m) }
func (*CoordinatorMessage) ProtoMessage()    {}
func (*CoordinatorMessage) Descriptor() ([]byte, []int) {
	return fileDescriptor_6f0b7582b0652215, []int{0}
}

func (m *CoordinatorMessage) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_CoordinatorMessage.Unmarshal(m, b)
}
func (m *CoordinatorMessage) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_CoordinatorMessage.Marshal(b, m, deterministic)
}
func (m *CoordinatorMessage) XXX_Merge(src proto.Message) {
	xxx_messageInfo_CoordinatorMessage.Merge(m, src)
}
func (m *CoordinatorMessage) XXX_Size() int {
	return xxx_messageInfo_CoordinatorMessage.Size(m)
}
func (m *CoordinatorMessage) XXX_DiscardUnknown() {
	xxx_messageInfo_CoordinatorMessage.DiscardUnknown(m)
}

var xxx_messageInfo_CoordinatorMessage proto.InternalMessageInfo

func (m *CoordinatorMessage) GetType() MessageType {
	if m != nil {
		return m.Type
	}
	return MessageType_UNKNOWN_MESSAGE_TYPE
}

type WelcomeServerMessage struct {
	Type                 MessageType `protobuf:"varint,1,opt,name=type,proto3,enum=MessageType" json:"type,omitempty"`
	Alias                string      `protobuf:"bytes,2,opt,name=alias,proto3" json:"alias,omitempty"`
	Peers                []string    `protobuf:"bytes,3,rep,name=peers,proto3" json:"peers,omitempty"`
	XXX_NoUnkeyedLiteral struct{}    `json:"-"`
	XXX_unrecognized     []byte      `json:"-"`
	XXX_sizecache        int32       `json:"-"`
}

func (m *WelcomeServerMessage) Reset()         { *m = WelcomeServerMessage{} }
func (m *WelcomeServerMessage) String() string { return proto.CompactTextString(m) }
func (*WelcomeServerMessage) ProtoMessage()    {}
func (*WelcomeServerMessage) Descriptor() ([]byte, []int) {
	return fileDescriptor_6f0b7582b0652215, []int{1}
}

func (m *WelcomeServerMessage) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_WelcomeServerMessage.Unmarshal(m, b)
}
func (m *WelcomeServerMessage) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_WelcomeServerMessage.Marshal(b, m, deterministic)
}
func (m *WelcomeServerMessage) XXX_Merge(src proto.Message) {
	xxx_messageInfo_WelcomeServerMessage.Merge(m, src)
}
func (m *WelcomeServerMessage) XXX_Size() int {
	return xxx_messageInfo_WelcomeServerMessage.Size(m)
}
func (m *WelcomeServerMessage) XXX_DiscardUnknown() {
	xxx_messageInfo_WelcomeServerMessage.DiscardUnknown(m)
}

var xxx_messageInfo_WelcomeServerMessage proto.InternalMessageInfo

func (m *WelcomeServerMessage) GetType() MessageType {
	if m != nil {
		return m.Type
	}
	return MessageType_UNKNOWN_MESSAGE_TYPE
}

func (m *WelcomeServerMessage) GetAlias() string {
	if m != nil {
		return m.Alias
	}
	return ""
}

func (m *WelcomeServerMessage) GetPeers() []string {
	if m != nil {
		return m.Peers
	}
	return nil
}

type WelcomeClientMessage struct {
	Type                 MessageType `protobuf:"varint,1,opt,name=type,proto3,enum=MessageType" json:"type,omitempty"`
	Alias                string      `protobuf:"bytes,2,opt,name=alias,proto3" json:"alias,omitempty"`
	XXX_NoUnkeyedLiteral struct{}    `json:"-"`
	XXX_unrecognized     []byte      `json:"-"`
	XXX_sizecache        int32       `json:"-"`
}

func (m *WelcomeClientMessage) Reset()         { *m = WelcomeClientMessage{} }
func (m *WelcomeClientMessage) String() string { return proto.CompactTextString(m) }
func (*WelcomeClientMessage) ProtoMessage()    {}
func (*WelcomeClientMessage) Descriptor() ([]byte, []int) {
	return fileDescriptor_6f0b7582b0652215, []int{2}
}

func (m *WelcomeClientMessage) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_WelcomeClientMessage.Unmarshal(m, b)
}
func (m *WelcomeClientMessage) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_WelcomeClientMessage.Marshal(b, m, deterministic)
}
func (m *WelcomeClientMessage) XXX_Merge(src proto.Message) {
	xxx_messageInfo_WelcomeClientMessage.Merge(m, src)
}
func (m *WelcomeClientMessage) XXX_Size() int {
	return xxx_messageInfo_WelcomeClientMessage.Size(m)
}
func (m *WelcomeClientMessage) XXX_DiscardUnknown() {
	xxx_messageInfo_WelcomeClientMessage.DiscardUnknown(m)
}

var xxx_messageInfo_WelcomeClientMessage proto.InternalMessageInfo

func (m *WelcomeClientMessage) GetType() MessageType {
	if m != nil {
		return m.Type
	}
	return MessageType_UNKNOWN_MESSAGE_TYPE
}

func (m *WelcomeClientMessage) GetAlias() string {
	if m != nil {
		return m.Alias
	}
	return ""
}

type ConnectMessage struct {
	Type                 MessageType `protobuf:"varint,1,opt,name=type,proto3,enum=MessageType" json:"type,omitempty"`
	FromAlias            string      `protobuf:"bytes,2,opt,name=from_alias,json=fromAlias,proto3" json:"from_alias,omitempty"`
	ToAlias              string      `protobuf:"bytes,3,opt,name=to_alias,json=toAlias,proto3" json:"to_alias,omitempty"`
	XXX_NoUnkeyedLiteral struct{}    `json:"-"`
	XXX_unrecognized     []byte      `json:"-"`
	XXX_sizecache        int32       `json:"-"`
}

func (m *ConnectMessage) Reset()         { *m = ConnectMessage{} }
func (m *ConnectMessage) String() string { return proto.CompactTextString(m) }
func (*ConnectMessage) ProtoMessage()    {}
func (*ConnectMessage) Descriptor() ([]byte, []int) {
	return fileDescriptor_6f0b7582b0652215, []int{3}
}

func (m *ConnectMessage) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_ConnectMessage.Unmarshal(m, b)
}
func (m *ConnectMessage) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_ConnectMessage.Marshal(b, m, deterministic)
}
func (m *ConnectMessage) XXX_Merge(src proto.Message) {
	xxx_messageInfo_ConnectMessage.Merge(m, src)
}
func (m *ConnectMessage) XXX_Size() int {
	return xxx_messageInfo_ConnectMessage.Size(m)
}
func (m *ConnectMessage) XXX_DiscardUnknown() {
	xxx_messageInfo_ConnectMessage.DiscardUnknown(m)
}

var xxx_messageInfo_ConnectMessage proto.InternalMessageInfo

func (m *ConnectMessage) GetType() MessageType {
	if m != nil {
		return m.Type
	}
	return MessageType_UNKNOWN_MESSAGE_TYPE
}

func (m *ConnectMessage) GetFromAlias() string {
	if m != nil {
		return m.FromAlias
	}
	return ""
}

func (m *ConnectMessage) GetToAlias() string {
	if m != nil {
		return m.ToAlias
	}
	return ""
}

type WebRtcMessage struct {
	Type                 MessageType `protobuf:"varint,1,opt,name=type,proto3,enum=MessageType" json:"type,omitempty"`
	FromAlias            string      `protobuf:"bytes,2,opt,name=from_alias,json=fromAlias,proto3" json:"from_alias,omitempty"`
	ToAlias              string      `protobuf:"bytes,3,opt,name=to_alias,json=toAlias,proto3" json:"to_alias,omitempty"`
	Sdp                  string      `protobuf:"bytes,4,opt,name=sdp,proto3" json:"sdp,omitempty"`
	XXX_NoUnkeyedLiteral struct{}    `json:"-"`
	XXX_unrecognized     []byte      `json:"-"`
	XXX_sizecache        int32       `json:"-"`
}

func (m *WebRtcMessage) Reset()         { *m = WebRtcMessage{} }
func (m *WebRtcMessage) String() string { return proto.CompactTextString(m) }
func (*WebRtcMessage) ProtoMessage()    {}
func (*WebRtcMessage) Descriptor() ([]byte, []int) {
	return fileDescriptor_6f0b7582b0652215, []int{4}
}

func (m *WebRtcMessage) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_WebRtcMessage.Unmarshal(m, b)
}
func (m *WebRtcMessage) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_WebRtcMessage.Marshal(b, m, deterministic)
}
func (m *WebRtcMessage) XXX_Merge(src proto.Message) {
	xxx_messageInfo_WebRtcMessage.Merge(m, src)
}
func (m *WebRtcMessage) XXX_Size() int {
	return xxx_messageInfo_WebRtcMessage.Size(m)
}
func (m *WebRtcMessage) XXX_DiscardUnknown() {
	xxx_messageInfo_WebRtcMessage.DiscardUnknown(m)
}

var xxx_messageInfo_WebRtcMessage proto.InternalMessageInfo

func (m *WebRtcMessage) GetType() MessageType {
	if m != nil {
		return m.Type
	}
	return MessageType_UNKNOWN_MESSAGE_TYPE
}

func (m *WebRtcMessage) GetFromAlias() string {
	if m != nil {
		return m.FromAlias
	}
	return ""
}

func (m *WebRtcMessage) GetToAlias() string {
	if m != nil {
		return m.ToAlias
	}
	return ""
}

func (m *WebRtcMessage) GetSdp() string {
	if m != nil {
		return m.Sdp
	}
	return ""
}

type WorldCommMessage struct {
	Type                 MessageType `protobuf:"varint,1,opt,name=type,proto3,enum=MessageType" json:"type,omitempty"`
	XXX_NoUnkeyedLiteral struct{}    `json:"-"`
	XXX_unrecognized     []byte      `json:"-"`
	XXX_sizecache        int32       `json:"-"`
}

func (m *WorldCommMessage) Reset()         { *m = WorldCommMessage{} }
func (m *WorldCommMessage) String() string { return proto.CompactTextString(m) }
func (*WorldCommMessage) ProtoMessage()    {}
func (*WorldCommMessage) Descriptor() ([]byte, []int) {
	return fileDescriptor_6f0b7582b0652215, []int{5}
}

func (m *WorldCommMessage) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_WorldCommMessage.Unmarshal(m, b)
}
func (m *WorldCommMessage) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_WorldCommMessage.Marshal(b, m, deterministic)
}
func (m *WorldCommMessage) XXX_Merge(src proto.Message) {
	xxx_messageInfo_WorldCommMessage.Merge(m, src)
}
func (m *WorldCommMessage) XXX_Size() int {
	return xxx_messageInfo_WorldCommMessage.Size(m)
}
func (m *WorldCommMessage) XXX_DiscardUnknown() {
	xxx_messageInfo_WorldCommMessage.DiscardUnknown(m)
}

var xxx_messageInfo_WorldCommMessage proto.InternalMessageInfo

func (m *WorldCommMessage) GetType() MessageType {
	if m != nil {
		return m.Type
	}
	return MessageType_UNKNOWN_MESSAGE_TYPE
}

type PingMessage struct {
	Type                 MessageType `protobuf:"varint,1,opt,name=type,proto3,enum=MessageType" json:"type,omitempty"`
	Time                 float64     `protobuf:"fixed64,2,opt,name=time,proto3" json:"time,omitempty"`
	XXX_NoUnkeyedLiteral struct{}    `json:"-"`
	XXX_unrecognized     []byte      `json:"-"`
	XXX_sizecache        int32       `json:"-"`
}

func (m *PingMessage) Reset()         { *m = PingMessage{} }
func (m *PingMessage) String() string { return proto.CompactTextString(m) }
func (*PingMessage) ProtoMessage()    {}
func (*PingMessage) Descriptor() ([]byte, []int) {
	return fileDescriptor_6f0b7582b0652215, []int{6}
}

func (m *PingMessage) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_PingMessage.Unmarshal(m, b)
}
func (m *PingMessage) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_PingMessage.Marshal(b, m, deterministic)
}
func (m *PingMessage) XXX_Merge(src proto.Message) {
	xxx_messageInfo_PingMessage.Merge(m, src)
}
func (m *PingMessage) XXX_Size() int {
	return xxx_messageInfo_PingMessage.Size(m)
}
func (m *PingMessage) XXX_DiscardUnknown() {
	xxx_messageInfo_PingMessage.DiscardUnknown(m)
}

var xxx_messageInfo_PingMessage proto.InternalMessageInfo

func (m *PingMessage) GetType() MessageType {
	if m != nil {
		return m.Type
	}
	return MessageType_UNKNOWN_MESSAGE_TYPE
}

func (m *PingMessage) GetTime() float64 {
	if m != nil {
		return m.Time
	}
	return 0
}

type ChangeTopicMessage struct {
	Type                 MessageType `protobuf:"varint,1,opt,name=type,proto3,enum=MessageType" json:"type,omitempty"`
	Topic                string      `protobuf:"bytes,2,opt,name=topic,proto3" json:"topic,omitempty"`
	XXX_NoUnkeyedLiteral struct{}    `json:"-"`
	XXX_unrecognized     []byte      `json:"-"`
	XXX_sizecache        int32       `json:"-"`
}

func (m *ChangeTopicMessage) Reset()         { *m = ChangeTopicMessage{} }
func (m *ChangeTopicMessage) String() string { return proto.CompactTextString(m) }
func (*ChangeTopicMessage) ProtoMessage()    {}
func (*ChangeTopicMessage) Descriptor() ([]byte, []int) {
	return fileDescriptor_6f0b7582b0652215, []int{7}
}

func (m *ChangeTopicMessage) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_ChangeTopicMessage.Unmarshal(m, b)
}
func (m *ChangeTopicMessage) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_ChangeTopicMessage.Marshal(b, m, deterministic)
}
func (m *ChangeTopicMessage) XXX_Merge(src proto.Message) {
	xxx_messageInfo_ChangeTopicMessage.Merge(m, src)
}
func (m *ChangeTopicMessage) XXX_Size() int {
	return xxx_messageInfo_ChangeTopicMessage.Size(m)
}
func (m *ChangeTopicMessage) XXX_DiscardUnknown() {
	xxx_messageInfo_ChangeTopicMessage.DiscardUnknown(m)
}

var xxx_messageInfo_ChangeTopicMessage proto.InternalMessageInfo

func (m *ChangeTopicMessage) GetType() MessageType {
	if m != nil {
		return m.Type
	}
	return MessageType_UNKNOWN_MESSAGE_TYPE
}

func (m *ChangeTopicMessage) GetTopic() string {
	if m != nil {
		return m.Topic
	}
	return ""
}

type TopicMessage struct {
	Type                 MessageType `protobuf:"varint,1,opt,name=type,proto3,enum=MessageType" json:"type,omitempty"`
	Topic                string      `protobuf:"bytes,3,opt,name=topic,proto3" json:"topic,omitempty"`
	FromAlias            string      `protobuf:"bytes,4,opt,name=from_alias,json=fromAlias,proto3" json:"from_alias,omitempty"`
	Body                 []byte      `protobuf:"bytes,5,opt,name=body,proto3" json:"body,omitempty"`
	XXX_NoUnkeyedLiteral struct{}    `json:"-"`
	XXX_unrecognized     []byte      `json:"-"`
	XXX_sizecache        int32       `json:"-"`
}

func (m *TopicMessage) Reset()         { *m = TopicMessage{} }
func (m *TopicMessage) String() string { return proto.CompactTextString(m) }
func (*TopicMessage) ProtoMessage()    {}
func (*TopicMessage) Descriptor() ([]byte, []int) {
	return fileDescriptor_6f0b7582b0652215, []int{8}
}

func (m *TopicMessage) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_TopicMessage.Unmarshal(m, b)
}
func (m *TopicMessage) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_TopicMessage.Marshal(b, m, deterministic)
}
func (m *TopicMessage) XXX_Merge(src proto.Message) {
	xxx_messageInfo_TopicMessage.Merge(m, src)
}
func (m *TopicMessage) XXX_Size() int {
	return xxx_messageInfo_TopicMessage.Size(m)
}
func (m *TopicMessage) XXX_DiscardUnknown() {
	xxx_messageInfo_TopicMessage.DiscardUnknown(m)
}

var xxx_messageInfo_TopicMessage proto.InternalMessageInfo

func (m *TopicMessage) GetType() MessageType {
	if m != nil {
		return m.Type
	}
	return MessageType_UNKNOWN_MESSAGE_TYPE
}

func (m *TopicMessage) GetTopic() string {
	if m != nil {
		return m.Topic
	}
	return ""
}

func (m *TopicMessage) GetFromAlias() string {
	if m != nil {
		return m.FromAlias
	}
	return ""
}

func (m *TopicMessage) GetBody() []byte {
	if m != nil {
		return m.Body
	}
	return nil
}

type AuthMessage struct {
	Type                 MessageType `protobuf:"varint,1,opt,name=type,proto3,enum=MessageType" json:"type,omitempty"`
	Role                 Role        `protobuf:"varint,2,opt,name=role,proto3,enum=Role" json:"role,omitempty"`
	Method               string      `protobuf:"bytes,3,opt,name=method,proto3" json:"method,omitempty"`
	Body                 []byte      `protobuf:"bytes,4,opt,name=body,proto3" json:"body,omitempty"`
	XXX_NoUnkeyedLiteral struct{}    `json:"-"`
	XXX_unrecognized     []byte      `json:"-"`
	XXX_sizecache        int32       `json:"-"`
}

func (m *AuthMessage) Reset()         { *m = AuthMessage{} }
func (m *AuthMessage) String() string { return proto.CompactTextString(m) }
func (*AuthMessage) ProtoMessage()    {}
func (*AuthMessage) Descriptor() ([]byte, []int) {
	return fileDescriptor_6f0b7582b0652215, []int{9}
}

func (m *AuthMessage) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_AuthMessage.Unmarshal(m, b)
}
func (m *AuthMessage) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_AuthMessage.Marshal(b, m, deterministic)
}
func (m *AuthMessage) XXX_Merge(src proto.Message) {
	xxx_messageInfo_AuthMessage.Merge(m, src)
}
func (m *AuthMessage) XXX_Size() int {
	return xxx_messageInfo_AuthMessage.Size(m)
}
func (m *AuthMessage) XXX_DiscardUnknown() {
	xxx_messageInfo_AuthMessage.DiscardUnknown(m)
}

var xxx_messageInfo_AuthMessage proto.InternalMessageInfo

func (m *AuthMessage) GetType() MessageType {
	if m != nil {
		return m.Type
	}
	return MessageType_UNKNOWN_MESSAGE_TYPE
}

func (m *AuthMessage) GetRole() Role {
	if m != nil {
		return m.Role
	}
	return Role_UNKNOWN_ROLE
}

func (m *AuthMessage) GetMethod() string {
	if m != nil {
		return m.Method
	}
	return ""
}

func (m *AuthMessage) GetBody() []byte {
	if m != nil {
		return m.Body
	}
	return nil
}

type PositionData struct {
	Time                 float64  `protobuf:"fixed64,1,opt,name=time,proto3" json:"time,omitempty"`
	PositionX            float32  `protobuf:"fixed32,2,opt,name=position_x,json=positionX,proto3" json:"position_x,omitempty"`
	PositionY            float32  `protobuf:"fixed32,3,opt,name=position_y,json=positionY,proto3" json:"position_y,omitempty"`
	PositionZ            float32  `protobuf:"fixed32,4,opt,name=position_z,json=positionZ,proto3" json:"position_z,omitempty"`
	RotationX            float32  `protobuf:"fixed32,5,opt,name=rotation_x,json=rotationX,proto3" json:"rotation_x,omitempty"`
	RotationY            float32  `protobuf:"fixed32,6,opt,name=rotation_y,json=rotationY,proto3" json:"rotation_y,omitempty"`
	RotationZ            float32  `protobuf:"fixed32,7,opt,name=rotation_z,json=rotationZ,proto3" json:"rotation_z,omitempty"`
	RotationW            float32  `protobuf:"fixed32,8,opt,name=rotation_w,json=rotationW,proto3" json:"rotation_w,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *PositionData) Reset()         { *m = PositionData{} }
func (m *PositionData) String() string { return proto.CompactTextString(m) }
func (*PositionData) ProtoMessage()    {}
func (*PositionData) Descriptor() ([]byte, []int) {
	return fileDescriptor_6f0b7582b0652215, []int{10}
}

func (m *PositionData) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_PositionData.Unmarshal(m, b)
}
func (m *PositionData) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_PositionData.Marshal(b, m, deterministic)
}
func (m *PositionData) XXX_Merge(src proto.Message) {
	xxx_messageInfo_PositionData.Merge(m, src)
}
func (m *PositionData) XXX_Size() int {
	return xxx_messageInfo_PositionData.Size(m)
}
func (m *PositionData) XXX_DiscardUnknown() {
	xxx_messageInfo_PositionData.DiscardUnknown(m)
}

var xxx_messageInfo_PositionData proto.InternalMessageInfo

func (m *PositionData) GetTime() float64 {
	if m != nil {
		return m.Time
	}
	return 0
}

func (m *PositionData) GetPositionX() float32 {
	if m != nil {
		return m.PositionX
	}
	return 0
}

func (m *PositionData) GetPositionY() float32 {
	if m != nil {
		return m.PositionY
	}
	return 0
}

func (m *PositionData) GetPositionZ() float32 {
	if m != nil {
		return m.PositionZ
	}
	return 0
}

func (m *PositionData) GetRotationX() float32 {
	if m != nil {
		return m.RotationX
	}
	return 0
}

func (m *PositionData) GetRotationY() float32 {
	if m != nil {
		return m.RotationY
	}
	return 0
}

func (m *PositionData) GetRotationZ() float32 {
	if m != nil {
		return m.RotationZ
	}
	return 0
}

func (m *PositionData) GetRotationW() float32 {
	if m != nil {
		return m.RotationW
	}
	return 0
}

type ProfileData struct {
	Time                 float64  `protobuf:"fixed64,1,opt,name=time,proto3" json:"time,omitempty"`
	AvatarType           string   `protobuf:"bytes,2,opt,name=avatar_type,json=avatarType,proto3" json:"avatar_type,omitempty"`
	DisplayName          string   `protobuf:"bytes,3,opt,name=display_name,json=displayName,proto3" json:"display_name,omitempty"`
	PublicKey            string   `protobuf:"bytes,4,opt,name=public_key,json=publicKey,proto3" json:"public_key,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *ProfileData) Reset()         { *m = ProfileData{} }
func (m *ProfileData) String() string { return proto.CompactTextString(m) }
func (*ProfileData) ProtoMessage()    {}
func (*ProfileData) Descriptor() ([]byte, []int) {
	return fileDescriptor_6f0b7582b0652215, []int{11}
}

func (m *ProfileData) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_ProfileData.Unmarshal(m, b)
}
func (m *ProfileData) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_ProfileData.Marshal(b, m, deterministic)
}
func (m *ProfileData) XXX_Merge(src proto.Message) {
	xxx_messageInfo_ProfileData.Merge(m, src)
}
func (m *ProfileData) XXX_Size() int {
	return xxx_messageInfo_ProfileData.Size(m)
}
func (m *ProfileData) XXX_DiscardUnknown() {
	xxx_messageInfo_ProfileData.DiscardUnknown(m)
}

var xxx_messageInfo_ProfileData proto.InternalMessageInfo

func (m *ProfileData) GetTime() float64 {
	if m != nil {
		return m.Time
	}
	return 0
}

func (m *ProfileData) GetAvatarType() string {
	if m != nil {
		return m.AvatarType
	}
	return ""
}

func (m *ProfileData) GetDisplayName() string {
	if m != nil {
		return m.DisplayName
	}
	return ""
}

func (m *ProfileData) GetPublicKey() string {
	if m != nil {
		return m.PublicKey
	}
	return ""
}

type ChatData struct {
	Time                 float64  `protobuf:"fixed64,1,opt,name=time,proto3" json:"time,omitempty"`
	MessageId            string   `protobuf:"bytes,2,opt,name=message_id,json=messageId,proto3" json:"message_id,omitempty"`
	Text                 string   `protobuf:"bytes,3,opt,name=text,proto3" json:"text,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *ChatData) Reset()         { *m = ChatData{} }
func (m *ChatData) String() string { return proto.CompactTextString(m) }
func (*ChatData) ProtoMessage()    {}
func (*ChatData) Descriptor() ([]byte, []int) {
	return fileDescriptor_6f0b7582b0652215, []int{12}
}

func (m *ChatData) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_ChatData.Unmarshal(m, b)
}
func (m *ChatData) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_ChatData.Marshal(b, m, deterministic)
}
func (m *ChatData) XXX_Merge(src proto.Message) {
	xxx_messageInfo_ChatData.Merge(m, src)
}
func (m *ChatData) XXX_Size() int {
	return xxx_messageInfo_ChatData.Size(m)
}
func (m *ChatData) XXX_DiscardUnknown() {
	xxx_messageInfo_ChatData.DiscardUnknown(m)
}

var xxx_messageInfo_ChatData proto.InternalMessageInfo

func (m *ChatData) GetTime() float64 {
	if m != nil {
		return m.Time
	}
	return 0
}

func (m *ChatData) GetMessageId() string {
	if m != nil {
		return m.MessageId
	}
	return ""
}

func (m *ChatData) GetText() string {
	if m != nil {
		return m.Text
	}
	return ""
}

func init() {
	proto.RegisterEnum("MessageType", MessageType_name, MessageType_value)
	proto.RegisterEnum("Role", Role_name, Role_value)
	proto.RegisterType((*CoordinatorMessage)(nil), "CoordinatorMessage")
	proto.RegisterType((*WelcomeServerMessage)(nil), "WelcomeServerMessage")
	proto.RegisterType((*WelcomeClientMessage)(nil), "WelcomeClientMessage")
	proto.RegisterType((*ConnectMessage)(nil), "ConnectMessage")
	proto.RegisterType((*WebRtcMessage)(nil), "WebRtcMessage")
	proto.RegisterType((*WorldCommMessage)(nil), "WorldCommMessage")
	proto.RegisterType((*PingMessage)(nil), "PingMessage")
	proto.RegisterType((*ChangeTopicMessage)(nil), "ChangeTopicMessage")
	proto.RegisterType((*TopicMessage)(nil), "TopicMessage")
	proto.RegisterType((*AuthMessage)(nil), "AuthMessage")
	proto.RegisterType((*PositionData)(nil), "PositionData")
	proto.RegisterType((*ProfileData)(nil), "ProfileData")
	proto.RegisterType((*ChatData)(nil), "ChatData")
}

func init() { proto.RegisterFile("commproto.proto", fileDescriptor_6f0b7582b0652215) }

var fileDescriptor_6f0b7582b0652215 = []byte{
	// 721 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0xbc, 0x95, 0xcf, 0x8f, 0x9b, 0x46,
	0x14, 0xc7, 0xcb, 0x1a, 0x7b, 0xed, 0x87, 0x77, 0x4b, 0x47, 0x56, 0x45, 0x0e, 0x55, 0x5d, 0x4e,
	0xab, 0x1c, 0xf6, 0x90, 0x56, 0x3d, 0x56, 0xa2, 0xe3, 0x49, 0x8a, 0x62, 0x03, 0x1d, 0xe3, 0x90,
	0xed, 0x05, 0x61, 0x33, 0x59, 0xa3, 0x02, 0x83, 0xf0, 0x64, 0x1b, 0x22, 0xf5, 0xd6, 0x7f, 0xb8,
	0xe7, 0x5e, 0x2a, 0x86, 0xf1, 0xd6, 0x44, 0x6a, 0xe5, 0xb4, 0x52, 0x2f, 0xd6, 0x9b, 0xcf, 0x17,
	0xde, 0xf7, 0xfd, 0x18, 0x64, 0xf8, 0x74, 0xc7, 0x8b, 0xa2, 0xaa, 0xb9, 0xe0, 0xb7, 0xf2, 0xd7,
	0xfe, 0x16, 0x10, 0xe6, 0xbc, 0x4e, 0xb3, 0x32, 0x11, 0xbc, 0x5e, 0xb1, 0xc3, 0x21, 0xb9, 0x67,
	0x68, 0x0e, 0xba, 0x68, 0x2a, 0x66, 0x69, 0x73, 0xed, 0xe6, 0xfa, 0xd9, 0xf4, 0x56, 0xf1, 0xb0,
	0xa9, 0x18, 0x95, 0x8a, 0x9d, 0xc2, 0x2c, 0x62, 0xf9, 0x8e, 0x17, 0x6c, 0xcd, 0xea, 0x07, 0x76,
	0xfe, 0x9b, 0x68, 0x06, 0xc3, 0x24, 0xcf, 0x92, 0x83, 0x75, 0x31, 0xd7, 0x6e, 0x26, 0xb4, 0x3b,
	0xb4, 0xb4, 0x62, 0xac, 0x3e, 0x58, 0x83, 0xf9, 0xa0, 0xa5, 0xf2, 0x60, 0x7b, 0x8f, 0x2e, 0x38,
	0xcf, 0x58, 0x29, 0xfe, 0xa3, 0x8b, 0x9d, 0xc3, 0x35, 0xe6, 0x65, 0xc9, 0x76, 0x1f, 0x91, 0xe9,
	0x0b, 0x80, 0x37, 0x35, 0x2f, 0xe2, 0xd3, 0x74, 0x93, 0x96, 0x38, 0xb2, 0xf0, 0x27, 0x30, 0x16,
	0x5c, 0x89, 0x03, 0x29, 0x5e, 0x0a, 0x2e, 0x25, 0xfb, 0x57, 0xb8, 0x8a, 0xd8, 0x96, 0x8a, 0xdd,
	0xff, 0x60, 0x86, 0x4c, 0x18, 0x1c, 0xd2, 0xca, 0xd2, 0x25, 0x6d, 0x43, 0xfb, 0x1b, 0x30, 0x23,
	0x5e, 0xe7, 0x29, 0xe6, 0x45, 0x71, 0xfe, 0x62, 0x31, 0x18, 0x41, 0x56, 0xde, 0x9f, 0x5f, 0x32,
	0x02, 0x5d, 0x64, 0x05, 0x93, 0xc5, 0x6a, 0x54, 0xc6, 0xf6, 0x12, 0x10, 0xde, 0x27, 0xe5, 0x3d,
	0x0b, 0x79, 0x95, 0xed, 0x3e, 0x6a, 0x6b, 0xa2, 0x7d, 0xe3, 0xb8, 0x35, 0x79, 0xb0, 0x1b, 0x98,
	0xfe, 0xdb, 0x3c, 0x83, 0x93, 0x3c, 0x1f, 0x0c, 0x57, 0xff, 0x70, 0xb8, 0x08, 0xf4, 0x2d, 0x4f,
	0x1b, 0x6b, 0x38, 0xd7, 0x6e, 0xa6, 0x54, 0xc6, 0xf6, 0x03, 0x18, 0xce, 0x5b, 0xb1, 0x3f, 0xdf,
	0xf9, 0x09, 0xe8, 0x35, 0xcf, 0xbb, 0x69, 0x5c, 0x3f, 0x1b, 0xde, 0x52, 0x9e, 0x33, 0x2a, 0x11,
	0xfa, 0x1c, 0x46, 0x05, 0x13, 0x7b, 0x9e, 0xaa, 0xaa, 0xd4, 0xe9, 0xd1, 0x57, 0x3f, 0xf1, 0xfd,
	0x43, 0x83, 0x69, 0xc0, 0x0f, 0x99, 0xc8, 0x78, 0xb9, 0x48, 0x44, 0xf2, 0x38, 0x65, 0xed, 0xaf,
	0x29, 0xb7, 0xfd, 0x54, 0xea, 0x99, 0xf8, 0x9d, 0x74, 0xbc, 0xa0, 0x93, 0x23, 0x79, 0xdd, 0x93,
	0x1b, 0xe9, 0x79, 0x22, 0xdf, 0xf5, 0xe4, 0xf7, 0xd2, 0xfc, 0x44, 0xfe, 0xa9, 0x95, 0x6b, 0x2e,
	0x12, 0x95, 0x7c, 0xd8, 0xc9, 0x47, 0xf2, 0xba, 0x27, 0x37, 0xd6, 0xa8, 0x2f, 0xdf, 0xf5, 0xe4,
	0xf7, 0xd6, 0x65, 0x5f, 0xee, 0x27, 0xff, 0xc5, 0x1a, 0xf7, 0xe5, 0xc8, 0xfe, 0x4d, 0x03, 0x23,
	0xa8, 0xf9, 0x9b, 0x2c, 0x67, 0x7f, 0xdb, 0xfc, 0x97, 0x60, 0x24, 0x0f, 0x89, 0x48, 0xea, 0x58,
	0x6e, 0xa4, 0xbb, 0x30, 0xd0, 0xa1, 0x76, 0x1f, 0xe8, 0x2b, 0x98, 0xa6, 0xd9, 0xa1, 0xca, 0x93,
	0x26, 0x2e, 0x93, 0x82, 0xa9, 0xa1, 0x1b, 0x8a, 0x79, 0x89, 0x1a, 0xe0, 0xdb, 0x6d, 0x9e, 0xed,
	0xe2, 0x9f, 0x59, 0x73, 0xbc, 0x10, 0x1d, 0x79, 0xc9, 0x1a, 0xfb, 0x47, 0x18, 0xe3, 0x7d, 0x22,
	0xfe, 0x69, 0xfe, 0x45, 0x77, 0x01, 0xe2, 0x2c, 0x3d, 0x7e, 0xac, 0x8a, 0xb8, 0x72, 0xaf, 0x82,
	0xbd, 0x13, 0xca, 0x58, 0xc6, 0x4f, 0x7f, 0xd7, 0xc0, 0x38, 0xb9, 0x34, 0xc8, 0x82, 0xd9, 0xc6,
	0x7b, 0xe9, 0xf9, 0x91, 0x17, 0xaf, 0xc8, 0x7a, 0xed, 0xbc, 0x20, 0x71, 0x78, 0x17, 0x10, 0xf3,
	0x13, 0x84, 0xe0, 0x3a, 0x22, 0x4b, 0xec, 0xaf, 0x48, 0xbc, 0x26, 0xf4, 0x15, 0xa1, 0xa6, 0x76,
	0xca, 0xf0, 0xd2, 0x25, 0x5e, 0x68, 0x5e, 0x20, 0x03, 0x2e, 0xb1, 0xef, 0x79, 0x04, 0x87, 0xe6,
	0x00, 0xcd, 0xc0, 0x8c, 0xc8, 0xf7, 0x34, 0xc4, 0xf1, 0x7a, 0x13, 0x04, 0x3e, 0x0d, 0xc9, 0xc2,
	0xd4, 0x91, 0x09, 0x53, 0x45, 0xfd, 0xe7, 0xcf, 0x09, 0x35, 0x87, 0xe8, 0x33, 0xb8, 0x52, 0xc4,
	0xf1, 0xd6, 0x11, 0xa1, 0xe6, 0xa8, 0xad, 0x44, 0x21, 0x17, 0x93, 0x18, 0x3b, 0xde, 0xc2, 0x5d,
	0x38, 0x21, 0x31, 0x2f, 0xd1, 0x18, 0xf4, 0xc0, 0xf5, 0x5e, 0x98, 0x63, 0x74, 0x05, 0x13, 0x67,
	0xb1, 0x88, 0x43, 0x3f, 0x70, 0xb1, 0x39, 0x69, 0xf3, 0x52, 0xb2, 0xf2, 0x5f, 0x11, 0x45, 0x00,
	0x4d, 0x60, 0xd8, 0x85, 0x46, 0xfb, 0x96, 0xb3, 0x09, 0x7f, 0x30, 0xa7, 0x4f, 0xbf, 0x03, 0xbd,
	0xfd, 0x0a, 0xda, 0xc7, 0x8f, 0xbd, 0x52, 0x7f, 0xd9, 0xf6, 0x08, 0x30, 0x52, 0x7d, 0x68, 0xad,
	0x3f, 0xf6, 0x57, 0xab, 0x8d, 0xe7, 0x62, 0x27, 0x74, 0x7d, 0xef, 0xd8, 0xf5, 0xc5, 0x76, 0x24,
	0xff, 0xa9, 0xbe, 0xfe, 0x33, 0x00, 0x00, 0xff, 0xff, 0x6d, 0x20, 0x68, 0x95, 0xbc, 0x06, 0x00,
	0x00,
}
