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

type ConnectorAck struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Checkpoint    *CheckpointToken       `protobuf:"bytes,1,opt,name=checkpoint,proto3" json:"checkpoint,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ConnectorAck) Reset() {
	*x = ConnectorAck{}
	mi := &file_v1_connector_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ConnectorAck) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ConnectorAck) ProtoMessage() {}

func (x *ConnectorAck) ProtoReflect() protoreflect.Message {
	mi := &file_v1_connector_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (*ConnectorAck) Descriptor() ([]byte, []int) {
	return file_v1_connector_proto_rawDescGZIP(), []int{0}
}

func (x *ConnectorAck) GetCheckpoint() *CheckpointToken {
	if x != nil {
		return x.Checkpoint
	}
	return nil
}

var File_v1_connector_proto protoreflect.FileDescriptor

const file_v1_connector_proto_rawDesc = "" +
	"\n" +
	"\x12v1/connector.proto\x12\tquanta.v1\x1a\x0ev1/frame.proto\"J\n" +
	"\fConnectorAck\x12:\n" +
	"\n" +
	"checkpoint\x18\x01 \x01(\v2\x1a.quanta.v1.CheckpointTokenR\n" +
	"checkpoint2D\n" +
	"\tConnector\x127\n" +
	"\x06Stream\x12\x10.quanta.v1.Frame\x1a\x17.quanta.v1.ConnectorAck(\x010\x01B\x18Z\x16quanta/api/proto/v1;pbb\x06proto3"

var (
	file_v1_connector_proto_rawDescOnce sync.Once
	file_v1_connector_proto_rawDescData []byte
)

func file_v1_connector_proto_rawDescGZIP() []byte {
	file_v1_connector_proto_rawDescOnce.Do(func() {
		file_v1_connector_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_v1_connector_proto_rawDesc), len(file_v1_connector_proto_rawDesc)))
	})
	return file_v1_connector_proto_rawDescData
}

var file_v1_connector_proto_msgTypes = make([]protoimpl.MessageInfo, 1)
var file_v1_connector_proto_goTypes = []any{
	(*ConnectorAck)(nil),
	(*CheckpointToken)(nil),
	(*Frame)(nil),
}
var file_v1_connector_proto_depIdxs = []int32{
	1,
	2,
	0,
	2,
	1,
	1,
	1,
	0,
}

func init() { file_v1_connector_proto_init() }
func file_v1_connector_proto_init() {
	if File_v1_connector_proto != nil {
		return
	}
	file_v1_frame_proto_init()
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_v1_connector_proto_rawDesc), len(file_v1_connector_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   1,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_v1_connector_proto_goTypes,
		DependencyIndexes: file_v1_connector_proto_depIdxs,
		MessageInfos:      file_v1_connector_proto_msgTypes,
	}.Build()
	File_v1_connector_proto = out.File
	file_v1_connector_proto_goTypes = nil
	file_v1_connector_proto_depIdxs = nil
}
