module github.com/oarkflow/vela/benchmarks/compare

go 1.23.0

require (
	github.com/oarkflow/vela v0.0.0
	github.com/vmihailenco/msgpack/v5 v5.4.1
	google.golang.org/grpc v1.74.2
	google.golang.org/protobuf v1.36.8
)

require (
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	golang.org/x/net v0.40.0 // indirect
	golang.org/x/sys v0.33.0 // indirect
	golang.org/x/text v0.25.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250528174236-200df99c418a // indirect
)

replace github.com/oarkflow/vela => ../..
