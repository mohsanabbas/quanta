package pb

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
	unsafe "unsafe"
)

const (
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)

	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type PingRequest struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *PingRequest) Reset() {
	*x = PingRequest{}
	mi := &file_v1_control_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *PingRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*PingRequest) ProtoMessage() {}

func (x *PingRequest) ProtoReflect() protoreflect.Message {
	mi := &file_v1_control_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (*PingRequest) Descriptor() ([]byte, []int) {
	return file_v1_control_proto_rawDescGZIP(), []int{0}
}

type PingReply struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Status        string                 `protobuf:"bytes,1,opt,name=status,proto3" json:"status,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *PingReply) Reset() {
	*x = PingReply{}
	mi := &file_v1_control_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *PingReply) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*PingReply) ProtoMessage() {}

func (x *PingReply) ProtoReflect() protoreflect.Message {
	mi := &file_v1_control_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (*PingReply) Descriptor() ([]byte, []int) {
	return file_v1_control_proto_rawDescGZIP(), []int{1}
}

func (x *PingReply) GetStatus() string {
	if x != nil {
		return x.Status
	}
	return ""
}

type DeployRequest struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Yaml          string                 `protobuf:"bytes,1,opt,name=yaml,proto3" json:"yaml,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *DeployRequest) Reset() {
	*x = DeployRequest{}
	mi := &file_v1_control_proto_msgTypes[2]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *DeployRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*DeployRequest) ProtoMessage() {}

func (x *DeployRequest) ProtoReflect() protoreflect.Message {
	mi := &file_v1_control_proto_msgTypes[2]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (*DeployRequest) Descriptor() ([]byte, []int) {
	return file_v1_control_proto_rawDescGZIP(), []int{2}
}

func (x *DeployRequest) GetYaml() string {
	if x != nil {
		return x.Yaml
	}
	return ""
}

type DeployReply struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Id            string                 `protobuf:"bytes,1,opt,name=id,proto3" json:"id,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *DeployReply) Reset() {
	*x = DeployReply{}
	mi := &file_v1_control_proto_msgTypes[3]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *DeployReply) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*DeployReply) ProtoMessage() {}

func (x *DeployReply) ProtoReflect() protoreflect.Message {
	mi := &file_v1_control_proto_msgTypes[3]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (*DeployReply) Descriptor() ([]byte, []int) {
	return file_v1_control_proto_rawDescGZIP(), []int{3}
}

func (x *DeployReply) GetId() string {
	if x != nil {
		return x.Id
	}
	return ""
}

type PauseRequest struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Id            string                 `protobuf:"bytes,1,opt,name=id,proto3" json:"id,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *PauseRequest) Reset() {
	*x = PauseRequest{}
	mi := &file_v1_control_proto_msgTypes[4]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *PauseRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*PauseRequest) ProtoMessage() {}

func (x *PauseRequest) ProtoReflect() protoreflect.Message {
	mi := &file_v1_control_proto_msgTypes[4]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (*PauseRequest) Descriptor() ([]byte, []int) {
	return file_v1_control_proto_rawDescGZIP(), []int{4}
}

func (x *PauseRequest) GetId() string {
	if x != nil {
		return x.Id
	}
	return ""
}

type PauseReply struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Ok            bool                   `protobuf:"varint,1,opt,name=ok,proto3" json:"ok,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *PauseReply) Reset() {
	*x = PauseReply{}
	mi := &file_v1_control_proto_msgTypes[5]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *PauseReply) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*PauseReply) ProtoMessage() {}

func (x *PauseReply) ProtoReflect() protoreflect.Message {
	mi := &file_v1_control_proto_msgTypes[5]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (*PauseReply) Descriptor() ([]byte, []int) {
	return file_v1_control_proto_rawDescGZIP(), []int{5}
}

func (x *PauseReply) GetOk() bool {
	if x != nil {
		return x.Ok
	}
	return false
}

var File_v1_control_proto protoreflect.FileDescriptor

const file_v1_control_proto_rawDesc = "" +
	"\n" +
	"\x10v1/control.proto\x12\tquanta.v1\"\r\n" +
	"\vPingRequest\"#\n" +
	"\tPingReply\x12\x16\n" +
	"\x06status\x18\x01 \x01(\tR\x06status\"#\n" +
	"\rDeployRequest\x12\x12\n" +
	"\x04yaml\x18\x01 \x01(\tR\x04yaml\"\x1d\n" +
	"\vDeployReply\x12\x0e\n" +
	"\x02id\x18\x01 \x01(\tR\x02id\"\x1e\n" +
	"\fPauseRequest\x12\x0e\n" +
	"\x02id\x18\x01 \x01(\tR\x02id\"\x1c\n" +
	"\n" +
	"PauseReply\x12\x0e\n" +
	"\x02ok\x18\x01 \x01(\bR\x02ok2\xc4\x01\n" +
	"\aControl\x124\n" +
	"\x04Ping\x12\x16.quanta.v1.PingRequest\x1a\x14.quanta.v1.PingReply\x12B\n" +
	"\x0eDeployPipeline\x12\x18.quanta.v1.DeployRequest\x1a\x16.quanta.v1.DeployReply\x12?\n" +
	"\rPausePipeline\x12\x17.quanta.v1.PauseRequest\x1a\x15.quanta.v1.PauseReplyB\x18Z\x16quanta/api/proto/v1;pbb\x06proto3"

var (
	file_v1_control_proto_rawDescOnce sync.Once
	file_v1_control_proto_rawDescData []byte
)

func file_v1_control_proto_rawDescGZIP() []byte {
	file_v1_control_proto_rawDescOnce.Do(func() {
		file_v1_control_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_v1_control_proto_rawDesc), len(file_v1_control_proto_rawDesc)))
	})
	return file_v1_control_proto_rawDescData
}

var file_v1_control_proto_msgTypes = make([]protoimpl.MessageInfo, 6)
var file_v1_control_proto_goTypes = []any{
	(*PingRequest)(nil),
	(*PingReply)(nil),
	(*DeployRequest)(nil),
	(*DeployReply)(nil),
	(*PauseRequest)(nil),
	(*PauseReply)(nil),
}
var file_v1_control_proto_depIdxs = []int32{
	0,
	2,
	4,
	1,
	3,
	5,
	3,
	0,
	0,
	0,
	0,
}

func init() { file_v1_control_proto_init() }
func file_v1_control_proto_init() {
	if File_v1_control_proto != nil {
		return
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_v1_control_proto_rawDesc), len(file_v1_control_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   6,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_v1_control_proto_goTypes,
		DependencyIndexes: file_v1_control_proto_depIdxs,
		MessageInfos:      file_v1_control_proto_msgTypes,
	}.Build()
	File_v1_control_proto = out.File
	file_v1_control_proto_goTypes = nil
	file_v1_control_proto_depIdxs = nil
}
