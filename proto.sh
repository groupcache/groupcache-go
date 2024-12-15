#! /bin/sh

# Make sure the script fails fast.
set -e
set -u
set -x

PROTO_DIR=groupcachepb

protoc -I=$PROTO_DIR \
    --go_out=$PROTO_DIR \
    $PROTO_DIR/groupcache.proto

echo FIXME enable example.proto
#protoc -I=$PROTO_DIR \
#   --go_out=. \
#    $PROTO_DIR/example.proto

protoc -I=testpb \
   --go_out=. \
    testpb/test.proto
