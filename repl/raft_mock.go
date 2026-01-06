package repl

import (
	"context"
	proto "harmonydb/repl/proto/repl"

	"github.com/golang/mock/gomock"
	"google.golang.org/grpc"
)

// MockClientConn is a mock of ClientConnInterface
type MockClientConn struct {
	ctrl     *gomock.Controller
	recorder *MockClientConnMockRecorder
}

type MockClientConnMockRecorder struct {
	mock *MockClientConn
}

func NewMockClientConn(ctrl *gomock.Controller) *MockClientConn {
	mock := &MockClientConn{ctrl: ctrl}
	mock.recorder = &MockClientConnMockRecorder{mock}
	return mock
}

func (m *MockClientConn) EXPECT() *MockClientConnMockRecorder {
	return m.recorder
}

func (m *MockClientConn) Invoke(ctx context.Context, method string, args interface{}, reply interface{}, opts ...grpc.CallOption) error {
	varargs := []interface{}{ctx, method, args, reply}
	for _, opt := range opts {
		varargs = append(varargs, opt)
	}
	ret := m.ctrl.Call(m, "Invoke", varargs...)
	ret0, _ := ret[0].(error)
	return ret0
}

func (m *MockClientConnMockRecorder) Invoke(ctx, method, args, reply interface{}, opts ...interface{}) *gomock.Call {
	varargs := append([]interface{}{ctx, method, args, reply}, opts...)
	return m.mock.ctrl.RecordCall(m.mock, "Invoke", varargs...)
}

func (m *MockClientConn) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	varargs := []interface{}{ctx, desc, method}
	for _, opt := range opts {
		varargs = append(varargs, opt)
	}
	ret := m.ctrl.Call(m, "NewStream", varargs...)
	ret0, _ := ret[0].(grpc.ClientStream)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

func (m *MockClientConnMockRecorder) NewStream(ctx, desc, method interface{}, opts ...interface{}) *gomock.Call {
	varargs := append([]interface{}{ctx, desc, method}, opts...)
	return m.mock.ctrl.RecordCall(m.mock, "NewStream", varargs...)
}

// createTestNode creates a node for testing with the given ID and peer IDs
func createTestNode(id int64, peerIDs []int64, ctrl *gomock.Controller) *Node {
	// Create mock WAL - expectations should be set in individual tests
	mockWAL := NewMockStorage(ctrl)

	node := &Node{
		ID:      id,
		address: "",
		members: make(map[int64]grpc.ClientConnInterface),
		config:  &ClusterConfig{ThisNodeID: id},
		propc:   make(chan *proto.Message, 256),
		applyc:  make(chan ToApply, 256),
		tickc:   make(chan struct{}, 128),
		wal:     mockWAL,
	}

	// Create peers map
	peers := make(map[int64]bool)
	for _, peerID := range peerIDs {
		peers[peerID] = true
		// Create mock connection for each peer
		mockConn := NewMockClientConn(ctrl)
		node.members[peerID] = mockConn
	}

	// Create Raft FSM with reduced timeouts for faster tests
	raft := &Raft{
		ID:               id,
		currentTerm:      0,
		votedFor:         make(map[int64]int64),
		state:            StateFollower,
		electionTimeout:  3,
		heartbeatTimeout: 1,
		clusterSize:      len(peerIDs) + 1,
		peers:            peers,
		votes:            make(map[int64]bool),
		msgs:             make([]*proto.Message, 0),
		msgsAfterAppend:  make([]*proto.Message, 0),
		raftLog:          newRaftLog(),
		progress:         make(map[int64]Progress),
		readyc:           make(chan Ready, 1),
	}

	// Initialize as follower
	raft.becomeFollower(0)

	node.raft = raft

	return node
}

func createVoteGrantMessage(from, to, term int64) *proto.Message {
	return &proto.Message{
		Type: proto.MessageType_MSG_VOTE_RESP,
		From: from,
		To:   to,
		VoteResp: &proto.RequestVoteResponse{
			Term:        term,
			VoteGranted: true,
		},
	}
}
