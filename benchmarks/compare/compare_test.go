// External comparisons require `go mod tidy` with Internet access.
// Benchmarks use identical logical scalar fields and explicit payload sizes.
package compare

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/oarkflow/vela"
	"github.com/vmihailenco/msgpack/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

var record = map[string]any{"id": uint64(300), "age": uint64(30), "active": true, "name": "Asha"}
var jsonData, _ = json.Marshal(record)
var velaData, _ = vela.Marshal(record)
var msgpackData, _ = msgpack.Marshal(record)
var bytesSink []byte
var anySink any
var protobufMsg = buildProto()
var protobufData, _ = proto.Marshal(protobufMsg)

// Uses official protobuf runtime with a runtime descriptor. No generated code.
// Dynamic messages are NOT representative of the fast generated Go protobuf path.
func buildProto() *dynamicpb.Message {
	label := func(n string, num int32, kind descriptorpb.FieldDescriptorProto_Type) *descriptorpb.FieldDescriptorProto {
		return &descriptorpb.FieldDescriptorProto{Name: proto.String(n), Number: proto.Int32(num), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: kind.Enum()}
	}
	desc := &descriptorpb.FileDescriptorProto{
		Name: proto.String("vela_compare.proto"), Package: proto.String("compare"), Syntax: proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{{Name: proto.String("Person"), Field: []*descriptorpb.FieldDescriptorProto{
			label("id", 1, descriptorpb.FieldDescriptorProto_TYPE_UINT64),
			label("age", 2, descriptorpb.FieldDescriptorProto_TYPE_UINT32),
			label("active", 3, descriptorpb.FieldDescriptorProto_TYPE_BOOL),
			label("name", 4, descriptorpb.FieldDescriptorProto_TYPE_STRING),
		}}},
	}
	fd, err := protodesc.NewFile(desc, nil)
	if err != nil {
		panic(err)
	}
	md := fd.Messages().ByName("Person")
	m := dynamicpb.NewMessage(md)
	m.Set(md.Fields().ByName("id"), protoreflect.ValueOfUint64(300))
	m.Set(md.Fields().ByName("age"), protoreflect.ValueOfUint32(30))
	m.Set(md.Fields().ByName("active"), protoreflect.ValueOfBool(true))
	m.Set(md.Fields().ByName("name"), protoreflect.ValueOfString("Asha"))
	return m
}
func TestPayloadSizes(t *testing.T) {
	t.Logf("standalone bytes: JSON=%d MessagePack=%d VELA=%d Protobuf dynamic(schema-bound)=%d", len(jsonData), len(msgpackData), len(velaData), len(protobufData))
}
func BenchmarkEncode(b *testing.B) {
	b.Run("VELA-Raw", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(velaData)))
		for i := 0; i < b.N; i++ {
			x, e := vela.Marshal(record)
			if e != nil {
				b.Fatal(e)
			}
			bytesSink = x
		}
	})
	b.Run("JSON-Map", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(jsonData)))
		for i := 0; i < b.N; i++ {
			x, e := json.Marshal(record)
			if e != nil {
				b.Fatal(e)
			}
			bytesSink = x
		}
	})
	b.Run("MessagePack-Map", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(msgpackData)))
		for i := 0; i < b.N; i++ {
			x, e := msgpack.Marshal(record)
			if e != nil {
				b.Fatal(e)
			}
			bytesSink = x
		}
	})
	b.Run("Protobuf-Dynamic", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(protobufData)))
		for i := 0; i < b.N; i++ {
			x, e := proto.Marshal(protobufMsg)
			if e != nil {
				b.Fatal(e)
			}
			bytesSink = x
		}
	})
}
func BenchmarkDecode(b *testing.B) {
	b.Run("VELA-Raw", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(velaData)))
		for i := 0; i < b.N; i++ {
			x, e := vela.Unmarshal(velaData)
			if e != nil {
				b.Fatal(e)
			}
			anySink = x
		}
	})
	b.Run("JSON-Map", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(jsonData)))
		for i := 0; i < b.N; i++ {
			var x any
			if e := json.Unmarshal(jsonData, &x); e != nil {
				b.Fatal(e)
			}
			anySink = x
		}
	})
	b.Run("MessagePack-Map", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(msgpackData)))
		for i := 0; i < b.N; i++ {
			var x map[string]any
			if e := msgpack.Unmarshal(msgpackData, &x); e != nil {
				b.Fatal(e)
			}
			anySink = x
		}
	})
	b.Run("Protobuf-Dynamic", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(protobufData)))
		for i := 0; i < b.N; i++ {
			x := dynamicpb.NewMessage(protobufMsg.Descriptor())
			if e := proto.Unmarshal(protobufData, x); e != nil {
				b.Fatal(e)
			}
			anySink = x
		}
	})
}

type rpcMarker interface {
	EchoBytes(context.Context, *wrapperspb.BytesValue) (*wrapperspb.BytesValue, error)
	EchoPerson(context.Context, *dynamicpb.Message) (*dynamicpb.Message, error)
}
type rpcImpl struct{}

func (*rpcImpl) EchoBytes(_ context.Context, v *wrapperspb.BytesValue) (*wrapperspb.BytesValue, error) {
	if _, err := vela.Open(v.Value); err != nil {
		return nil, err
	}
	return v, nil
}
func (*rpcImpl) EchoPerson(_ context.Context, v *dynamicpb.Message) (*dynamicpb.Message, error) {
	return v, nil
}
func bytesHandler(srv any, ctx context.Context, dec func(any) error, intr grpc.UnaryServerInterceptor) (any, error) {
	req := new(wrapperspb.BytesValue)
	if err := dec(req); err != nil {
		return nil, err
	}
	h := func(ctx context.Context, in any) (any, error) {
		return srv.(rpcMarker).EchoBytes(ctx, in.(*wrapperspb.BytesValue))
	}
	if intr == nil {
		return h(ctx, req)
	}
	return intr(ctx, req, &grpc.UnaryServerInfo{Server: srv, FullMethod: "/compare.Echo/EchoBytes"}, h)
}
func personHandler(srv any, ctx context.Context, dec func(any) error, intr grpc.UnaryServerInterceptor) (any, error) {
	req := dynamicpb.NewMessage(protobufMsg.Descriptor())
	if err := dec(req); err != nil {
		return nil, err
	}
	h := func(ctx context.Context, in any) (any, error) {
		return srv.(rpcMarker).EchoPerson(ctx, in.(*dynamicpb.Message))
	}
	if intr == nil {
		return h(ctx, req)
	}
	return intr(ctx, req, &grpc.UnaryServerInfo{Server: srv, FullMethod: "/compare.Echo/EchoPerson"}, h)
}
func startGRPC(b *testing.B) (*grpc.ClientConn, func()) {
	b.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	server := grpc.NewServer()
	server.RegisterService(&grpc.ServiceDesc{
		ServiceName: "compare.Echo", HandlerType: (*rpcMarker)(nil),
		Methods: []grpc.MethodDesc{{MethodName: "EchoBytes", Handler: bytesHandler}, {MethodName: "EchoPerson", Handler: personHandler}},
	}, &rpcImpl{})
	go func() { _ = server.Serve(listener) }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		server.Stop()
		listener.Close()
		b.Fatal(err)
	}
	return conn, func() { conn.Close(); server.Stop(); listener.Close() }
}

// These are REAL localhost TCP gRPC unary calls (not net.Pipe or mocked RPC).
// Both routes use the same gRPC runtime; payload semantics differ, so compare
// carefully. These are not codec-only numbers.
func BenchmarkGRPCLocalhost(b *testing.B) {
	conn, closeServer := startGRPC(b)
	defer closeServer()
	ctx := context.Background()
	b.Run("ProtobufDynamicPerson", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(protobufData)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			response := dynamicpb.NewMessage(protobufMsg.Descriptor())
			if err := conn.Invoke(ctx, "/compare.Echo/EchoPerson", protobufMsg, response); err != nil {
				b.Fatal(err)
			}
			anySink = response
		}
	})
	b.Run("VELAInProtobufBytes", func(b *testing.B) {
		req := wrapperspb.Bytes(velaData)
		b.ReportAllocs()
		b.SetBytes(int64(len(velaData)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			response := new(wrapperspb.BytesValue)
			if err := conn.Invoke(ctx, "/compare.Echo/EchoBytes", req, response); err != nil {
				b.Fatal(err)
			}
			if _, err := vela.Open(response.Value); err != nil {
				b.Fatal(err)
			}
			anySink = response
		}
	})
}
