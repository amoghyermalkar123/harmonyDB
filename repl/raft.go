package repl

import (
	"context"
	"fmt"
	proto "harmonydb/repl/proto/repl"
	"harmonydb/scheduler"
	"harmonydb/wal"
	"log/slog"
	"time"

	"google.golang.org/grpc"
)

// RaftNode is the main interface to interact with the Raft
// consensus protocol
type RaftNode interface {
	Tick()
	Process(ctx context.Context, msg *proto.Message) (*proto.Message, error)
	Apply() chan ToApply
	GetWAL() wal.Storage
}

type NodeInfo struct {
	ID      int64  `yaml:"id"`
	Address string `yaml:"address"`
}

type ClusterConfig struct {
	Cluster struct {
		Nodes []NodeInfo `yaml:"nodes"`
	} `yaml:"cluster"`
	ThisNodeID int64 `yaml:"this_node_id"`
}

// Node is the main raft implementation along with i/o
// it wraps an underlying pure-FSM implementation called RaftSM
// the Database should be using this as a consensus integration
//
// implements the RaftNode interface
type Node struct {
	proto.UnimplementedRaftServiceServer
	ID      int64
	address string
	// todo: this could be abstracted with a Transport API
	// useful for sim testing, checking msg ordering, etc
	members map[int64]grpc.ClientConnInterface
	config  *ClusterConfig
	raft    RaftSM
	propc   chan *proto.Message
	applyc  chan ToApply
	tickc   chan struct{}
	wal     wal.Storage
	server  *grpc.Server
	sched   scheduler.FifoScheduler
}

// ToApply indicates entries that are deemed safe-to-apply
// on a leader. This happens only when entries are safely
// replicated on the majority of the replicas in the cluster
type ToApply struct {
	Entries     []*proto.Log
	CommitIndex int64
}

// Process is the main entrypoint for processing a raft message (heartbeats, elections, appends)
// takes a raft message and applies it to the raft state machine
// This implements the RaftServiceServer interface for gRPC
func (n *Node) Process(ctx context.Context, msg *proto.Message) (*proto.Message, error) {
	if err := n.stepSM(ctx, msg); err != nil {
		return nil, fmt.Errorf("stepsm: %w", err)
	}

	return &proto.Message{}, nil
}

// stepSM sends incoming messages in-order to the run-loop for them to be processed
// via the state machine
func (n *Node) stepSM(ctx context.Context, msg *proto.Message) error {
	select {
	case n.propc <- msg:
	case <-ctx.Done():
		return fmt.Errorf("stepSM: context cancelled: %w", ctx.Err())
	}
	return nil
}

// Tick advances the logical clock of the raft state machine
// the typical usage for this API is for the caller to trigger
// it on a ticker.C based channel sends
func (n *Node) Tick() {
	select {
	case n.tickc <- struct{}{}:
	default:
		fmt.Println("A tick missed to fire. Node blocks too long!")
	}
}

// Apply returns a channel for the caller to receive safe-to-apply entries
func (n *Node) Apply() chan ToApply {
	return n.applyc
}

// GetWAL returns the WAL storage for recovery purposes
func (n *Node) GetWAL() wal.Storage {
	return n.wal
}

// run is the main event loop for a raft node
// performs FSM and I/O Operations in-order
func (n *Node) run() {
	for {
		select {
		case msg := <-n.propc:
			// receive messages in queue sent by Node.Step here
			// then propose them to the quorum
			if err := n.raft.Step(msg); err != nil {
				slog.Error("run: Step", slog.String("err", err.Error()))
				// todo(err): error handling
			}
		case rd := <-n.raft.Ready():
			// Process Ready in order:
			// 1. Persist unstable entries to WAL
			// 2. Send messages that don't need WAL immediately
			// 3. Send messages that needed WAL persistence
			// 4. Send committed entries for application
			// 5. Call Advance

			// Persist unstable entries to WAL
			n.saveToWAL(rd.Entries)

			// Send messages that don't need WAL persistence
			n.send(rd.Messages)

			// Send messages that needed WAL persistence (now safe to send)
			// becayse we added these to wal above in `saveToWAL`
			// todo(improvement): currently we use rd.Entries and r.MessagesAfterAppend
			// to signify the same logs, find out a better way so that these can be just
			// retrieved from 1 field
			n.send(rd.MessagesAfterAppend)

			// Send committed entries for application
			if len(rd.CommittedEntries) > 0 {
				ap := ToApply{
					Entries:     rd.CommittedEntries,
					CommitIndex: rd.HardState.lastCommitIndex,
				}
				n.applyc <- ap
			}

			// Indicate ready has been consumed
			n.raft.Advance()
		case <-n.tickc:
			n.raft.Tick()
		default:
		}
	}
}

func (n *Node) saveToWAL(entries []*proto.Log) {
	for _, entry := range entries {
		n.wal.Append(entry)
	}
}

// send sends a message to the cluster
func (n *Node) send(msgs []*proto.Message) {
	for _, msg := range msgs {
		// skip if no destination
		if msg.To == 0 {
			continue
		}

		// get the connection to the peer
		conn, exists := n.members[msg.To]
		if !exists {
			slog.Error("send: peer not found", slog.Int64("peer_id", msg.To))
			continue
		}

		// send async to avoid blocking the run loop
		go func(m *proto.Message, c grpc.ClientConnInterface) {
			ctx, cancel := context.WithTimeout(context.Background(), 1000*time.Millisecond)
			defer cancel()

			resp := &proto.Message{}
			err := c.Invoke(ctx, proto.RaftService_Process_FullMethodName, m, resp)
			if err != nil {
				slog.Error("send: rpc failed",
					slog.Int64("to", m.To),
					slog.String("type", m.Type.String()),
					slog.String("err", err.Error()))
			}
		}(msg, conn)
	}
}
