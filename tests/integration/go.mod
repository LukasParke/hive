module github.com/luke/hive/tests/integration

go 1.25.0

require (
	connectrpc.com/connect v1.19.1
	github.com/luke/hive/proto v0.0.0
)

require google.golang.org/protobuf v1.36.11 // indirect

replace (
	github.com/luke/hive/agent => ../../agent
	github.com/luke/hive/proto => ../../proto
)
