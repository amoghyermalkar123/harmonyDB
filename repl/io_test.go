package repl

import (
	"testing"
	"time"

	proto "harmonydb/repl/proto/repl"

	"github.com/golang/mock/gomock"
	"google.golang.org/grpc"
)

// TestNodeRunProcessesReady tests that Node.run() correctly processes Ready
// Verifies the I/O layer: calls Ready(), persists to WAL, sends messages, calls Advance()
func TestNodeRunProcessesReady(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mock FSM
	mockRaft := NewMockRaftSM(ctrl)

	// Create mock WAL
	mockWAL := NewMockStorage(ctrl)

	// Create mock network connections for peers
	mockConn2 := NewMockClientConn(ctrl)
	mockConn3 := NewMockClientConn(ctrl)

	// Create node with mocks
	node := &Node{
		ID:      1,
		address: "",
		members: map[int64]grpc.ClientConnInterface{
			2: mockConn2,
			3: mockConn3,
		},
		config: &ClusterConfig{ThisNodeID: 1},
		propc:  make(chan *proto.Message, 256),
		applyc: make(chan ToApply, 256),
		tickc:  make(chan struct{}, 128),
		wal:    mockWAL,
		raft:   mockRaft,
	}

	// Prepare the Ready struct that the mock FSM will return
	entry := &proto.Log{
		Term: 1,
		Id:   1,
		Data: &proto.Cmd{
			Op:        "PUT",
			Key:       "testkey",
			Value:     "testvalue",
			RequestId: 100,
		},
	}

	readyData := Ready{
		Messages: []*proto.Message{},  // No immediate messages
		Entries:  []*proto.Log{entry}, // 1 unstable entry to persist
		MessagesAfterAppend: []*proto.Message{
			{
				Type: proto.MessageType_MSG_APP,
				To:   2,
				From: 1,
				Entries: &proto.AppendEntries{
					Term:       1,
					LeaderId:   1,
					PrevLogIdx: 0,
					Entries:    []*proto.Log{entry},
				},
			},
			{
				Type: proto.MessageType_MSG_APP,
				To:   3,
				From: 1,
				Entries: &proto.AppendEntries{
					Term:       1,
					LeaderId:   1,
					PrevLogIdx: 0,
					Entries:    []*proto.Log{entry},
				},
			},
		},
		CommittedEntries: []*proto.Log{},
	}

	readyChan := make(chan Ready, 1)
	readyChan <- readyData

	// Set up expectations in order
	gomock.InOrder(
		// 1. Node.run() calls Ready() to check for work
		mockRaft.EXPECT().Ready().Return(readyChan),

		// 2. Node persists unstable entries to WAL
		mockWAL.EXPECT().Append(entry).Times(1),

		// 3. Node sends MessagesAfterAppend to peers (order may vary)
	)

	// These can happen in any order
	mockConn2.EXPECT().Invoke(gomock.Any(), proto.RaftService_Process_FullMethodName, gomock.Any(), gomock.Any()).Times(1)
	mockConn3.EXPECT().Invoke(gomock.Any(), proto.RaftService_Process_FullMethodName, gomock.Any(), gomock.Any()).Times(1)

	// 4. Node calls Advance() to signal Ready consumed
	mockRaft.EXPECT().Advance().Times(1)

	// 5. Node loops back to Ready() (no more work)
	mockRaft.EXPECT().Ready().Return(make(chan Ready)).AnyTimes()

	// Mock Step() and Tick() for any background calls
	mockRaft.EXPECT().Step(gomock.Any()).Return(nil).AnyTimes()
	mockRaft.EXPECT().Tick().AnyTimes()

	// Start the run loop
	go node.run()

	// Give time for run loop to process
	time.Sleep(10 * time.Millisecond)

	t.Log("I/O layer correctly processed Ready, persisted to WAL, sent messages, and called Advance")
}
