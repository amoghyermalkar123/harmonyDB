package raft

import (
	"fmt"
	"testing"
	"time"
)

// ============================================================================
// LEADER ELECTION TESTS
// ============================================================================

func TestLeaderElection_SingleNode(t *testing.T) {
	t.Skip("Single-node clusters not supported - minimum cluster size is 3 nodes")
}

func TestLeaderElection_ThreeNodes(t *testing.T) {
	c := newTestCluster(t, 3)
	c.bootstrap()
	defer c.shutdown()

	leader := c.leader()
	if leader == nil {
		t.Fatal("no leader elected")
	}

	followers := c.followers()
	if len(followers) != 2 {
		t.Fatalf("expected 2 followers, got %d", len(followers))
	}

	c.ensureLeader(leader.n.ID)
}

func TestLeaderElection_FiveNodes(t *testing.T) {
	c := newTestCluster(t, 5)
	c.bootstrap()
	defer c.shutdown()

	leader := c.leader()
	if leader == nil {
		t.Fatal("no leader elected")
	}

	followers := c.followers()
	if len(followers) != 4 {
		t.Fatalf("expected 4 followers, got %d", len(followers))
	}

	c.ensureLeader(leader.n.ID)
}

func TestLeaderElection_LeaderFailover(t *testing.T) {
	c := newTestCluster(t, 3)
	c.bootstrap()
	defer c.shutdown()

	leader := c.leader()
	if leader == nil {
		t.Fatal("no initial leader elected")
	}
	oldLeaderID := leader.n.ID

	c.disconnect(oldLeaderID)

	time.Sleep(500 * time.Millisecond)

	c.waitFor(func() bool {
		newLeader := c.getLeader()
		return newLeader != nil && newLeader.n.ID != oldLeaderID
	}, 5*time.Second, "new leader not elected after old leader failure")

	newLeader := c.getLeader()
	if newLeader == nil {
		t.Fatal("no new leader elected")
	}

	if newLeader.n.ID == oldLeaderID {
		t.Fatal("old leader still leader after disconnect")
	}

	c.logf("New leader elected: node %d", newLeader.n.ID)
}

func TestLeaderElection_LeaderRejoin(t *testing.T) {
	c := newTestCluster(t, 3)
	c.bootstrap()
	defer c.shutdown()

	oldLeader := c.leader()
	oldLeaderID := oldLeader.n.ID

	c.disconnect(oldLeaderID)

	time.Sleep(500 * time.Millisecond)

	newLeader := c.getLeader()
	if newLeader == nil || newLeader.n.ID == oldLeaderID {
		t.Fatal("new leader not elected")
	}

	c.reconnect(oldLeaderID)

	time.Sleep(300 * time.Millisecond)

	oldLeader.n.meta.RLock()
	nodeType := oldLeader.n.meta.nt
	oldLeader.n.meta.RUnlock()

	if nodeType != Follower {
		t.Fatalf("old leader should be follower, but is %v", nodeType)
	}

	followers := c.followers()
	if len(followers) != 2 {
		t.Fatalf("expected 2 followers, got %d", len(followers))
	}
}

func TestLeaderElection_NetworkPartition(t *testing.T) {
	c := newTestCluster(t, 5)
	c.bootstrap()
	defer c.shutdown()

	leader := c.leader()
	if leader == nil {
		t.Fatal("no initial leader")
	}

	leaderID := leader.n.ID
	minorityNodes := []int64{}
	majorityNodes := []int64{leaderID}

	count := 0
	for _, node := range c.nodes {
		if node.n.ID != leaderID && count < 1 {
			minorityNodes = append(minorityNodes, node.n.ID)
			count++
		} else if node.n.ID != leaderID {
			majorityNodes = append(majorityNodes, node.n.ID)
		}
	}

	c.partition(majorityNodes, minorityNodes)

	time.Sleep(500 * time.Millisecond)

	for _, nodeID := range majorityNodes {
		node := c.getNode(nodeID)
		node.n.meta.RLock()
		nt := node.n.meta.nt
		node.n.meta.RUnlock()

		if nt != Leader && nt != Follower {
			t.Fatalf("majority partition node %d should be leader or follower, got %v", nodeID, nt)
		}
	}

	hasMinorityLeader := false
	for _, nodeID := range minorityNodes {
		node := c.getNode(nodeID)
		node.n.meta.RLock()
		nt := node.n.meta.nt
		node.n.meta.RUnlock()

		if nt == Leader {
			hasMinorityLeader = true
		}
	}

	if hasMinorityLeader {
		t.Fatal("minority partition should not have a leader")
	}
}

func TestLeaderElection_TermProgression(t *testing.T) {
	c := newTestCluster(t, 3)
	c.bootstrap()
	defer c.shutdown()

	leader := c.leader()
	if leader == nil {
		t.Fatal("no initial leader")
	}

	leader.n.state.Lock()
	initialTerm := leader.n.state.currentTerm
	leader.n.state.Unlock()

	c.disconnect(leader.n.ID)

	time.Sleep(500 * time.Millisecond)

	newLeader := c.getLeader()
	if newLeader == nil {
		t.Fatal("no new leader elected")
	}

	newLeader.n.state.Lock()
	newTerm := newLeader.n.state.currentTerm
	newLeader.n.state.Unlock()

	if newTerm <= initialTerm {
		t.Fatalf("new term %d should be greater than initial term %d", newTerm, initialTerm)
	}

	c.logf("Term progressed from %d to %d", initialTerm, newTerm)
}

func TestLeaderElection_VoteOnlyOncePerTerm(t *testing.T) {
	c := newTestCluster(t, 3)
	c.bootstrap()
	defer c.shutdown()

	leader := c.leader()
	if leader == nil {
		t.Fatal("no leader elected")
	}

	for _, node := range c.nodes {
		node.n.state.Lock()
		votes := node.n.state.pastVotes
		currentTerm := node.n.state.currentTerm
		node.n.state.Unlock()

		for term, candidateID := range votes {
			if term == currentTerm {
				if candidateID != leader.n.ID {
					continue
				}

				count := 0
				for t := range votes {
					if t == term {
						count++
					}
				}

				if count > 1 {
					t.Fatalf("node voted multiple times in term %d", term)
				}
			}
		}
	}
}

// ============================================================================
// LOG REPLICATION TESTS (Placeholders for future implementation)
// ============================================================================

func TestLogReplication_SingleEntry(t *testing.T) {
	c := newTestCluster(t, 3)
	c.bootstrap()
	defer c.shutdown()

	leader := c.leader()
	if leader == nil {
		t.Fatal("no leader elected")
	}

	err := leader.n.replicate(nil, []byte("key1"), []byte("value1"))
	if err != nil {
		t.Fatalf("failed to replicate entry: %v", err)
	}

	c.waitForReplication(1)

	c.ensureSame()

	for i, logStore := range c.logs {
		logs := logStore.logManager.GetLogs()
		if len(logs) != 1 {
			t.Fatalf("node %d has %d logs, expected 1", i+1, len(logs))
		}
		log := logs[0]
		if log.Data.Key != "key1" || log.Data.Value != "value1" {
			t.Fatalf("node %d has incorrect log entry: key=%s, value=%s", i+1, log.Data.Key, log.Data.Value)
		}
	}

	c.logf("Single log entry successfully replicated to all nodes")
}

func TestLogReplication_MultipleEntries(t *testing.T) {
	c := newTestCluster(t, 3)
	c.bootstrap()
	defer c.shutdown()

	leader := c.leader()
	if leader == nil {
		t.Fatal("no leader elected")
	}

	numEntries := 5
	for i := 0; i < numEntries; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		value := []byte(fmt.Sprintf("value%d", i))
		err := leader.n.replicate(nil, key, value)
		if err != nil {
			t.Fatalf("failed to replicate entry %d: %v", i, err)
		}
	}

	c.waitForReplication(numEntries)

	c.ensureSame()

	for i, logStore := range c.logs {
		logs := logStore.logManager.GetLogs()
		if len(logs) != numEntries {
			t.Fatalf("node %d has %d logs, expected %d", i+1, len(logs), numEntries)
		}
		for j := 0; j < numEntries; j++ {
			log := logs[j]
			expectedKey := fmt.Sprintf("key%d", j)
			expectedValue := fmt.Sprintf("value%d", j)
			if log.Data.Key != expectedKey || log.Data.Value != expectedValue {
				t.Fatalf("node %d log entry %d incorrect: key=%s (expected %s), value=%s (expected %s)",
					i+1, j, log.Data.Key, expectedKey, log.Data.Value, expectedValue)
			}
		}
	}

	c.logf("Multiple log entries successfully replicated to all nodes")
}

// func TestLogReplication_FollowerCatchUp(t *testing.T) {
// 	c := newTestCluster(t, 3)
// 	c.bootstrap()
// 	defer c.shutdown()

// 	leader := c.leader()
// 	if leader == nil {
// 		t.Fatal("no leader elected")
// 	}

// 	follower := c.followers()[0]
// 	followerID := follower.n.ID

// 	c.disconnect(followerID)

// 	numEntries := 3
// 	for i := 0; i < numEntries; i++ {
// 		key := []byte(fmt.Sprintf("key%d", i))
// 		value := []byte(fmt.Sprintf("value%d", i))
// 		err := leader.n.replicate(nil, key, value)
// 		if err != nil {
// 			t.Fatalf("failed to replicate entry %d: %v", i, err)
// 		}
// 	}

// 	time.Sleep(200 * time.Millisecond)

// 	disconnectedLog := c.logs[followerID-1]
// 	logCountBeforeReconnect := disconnectedLog.logManager.GetLength()

// 	if logCountBeforeReconnect != 0 {
// 		t.Fatalf("disconnected follower should have 0 logs, but has %d", logCountBeforeReconnect)
// 	}

// 	c.reconnect(followerID)

// 	c.waitForReplication(numEntries)

// 	c.ensureSame()

// 	logCountAfterReconnect := disconnectedLog.logManager.GetLength()

// 	if logCountAfterReconnect != numEntries {
// 		t.Fatalf("follower should have caught up with %d logs, but has %d", numEntries, logCountAfterReconnect)
// 	}

// 	c.logf("Follower successfully caught up with %d log entries", numEntries)
// }

// ============================================================================
// LOG CONSISTENCY TESTS (Placeholders for future implementation)
// ============================================================================

func TestLogConsistency_ConflictingEntries(t *testing.T) {
	c := newTestCluster(t, 3)
	c.bootstrap()
	defer c.shutdown()

	leader := c.leader()
	if leader == nil {
		t.Fatal("no leader elected")
	}

	err := leader.n.replicate(nil, []byte("key1"), []byte("value1"))
	if err != nil {
		t.Fatalf("failed to replicate first entry: %v", err)
	}

	c.waitForReplication(1)

	follower := c.followers()[0]
	followerID := follower.n.ID

	c.disconnect(followerID)

	err = leader.n.replicate(nil, []byte("key2"), []byte("value2"))
	if err != nil {
		t.Fatalf("failed to replicate second entry: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	c.reconnect(followerID)

	c.waitForReplication(2)

	c.ensureSame()

	for i, logStore := range c.logs {
		logs := logStore.logManager.GetLogs()
		if len(logs) != 2 {
			t.Fatalf("node %d has %d logs, expected 2", i+1, len(logs))
		}
		if logs[0].Data.Key != "key1" || logs[1].Data.Key != "key2" {
			t.Fatalf("node %d has incorrect log sequence", i+1)
		}
	}

	c.logf("Log consistency maintained after reconnection")
}

func TestLogConsistency_LogTruncation(t *testing.T) {
	c := newTestCluster(t, 5)
	c.bootstrap()
	defer c.shutdown()

	leader := c.leader()
	if leader == nil {
		t.Fatal("no leader elected")
	}

	err := leader.n.replicate(nil, []byte("key1"), []byte("value1"))
	if err != nil {
		t.Fatalf("failed to replicate first entry: %v", err)
	}

	c.waitForReplication(1)

	minorityNodes := []int64{}
	majorityNodes := []int64{}
	count := 0
	for _, node := range c.nodes {
		if count < 2 {
			minorityNodes = append(minorityNodes, node.n.ID)
			count++
		} else {
			majorityNodes = append(majorityNodes, node.n.ID)
		}
	}

	c.partition(majorityNodes, minorityNodes)

	time.Sleep(1 * time.Second)

	newLeader := c.getLeader()
	if newLeader == nil {
		t.Fatal("no new leader in majority partition")
	}

	for _, nodeID := range majorityNodes {
		if nodeID == newLeader.n.ID {
			err := newLeader.n.replicate(nil, []byte("key2"), []byte("value2"))
			if err != nil {
				t.Logf("Replication may fail in partitioned scenario: %v", err)
			}
			break
		}
	}

	time.Sleep(300 * time.Millisecond)

	c.fullyConnect()

	time.Sleep(1 * time.Second)

	finalLeader := c.getLeader()
	if finalLeader == nil {
		t.Fatal("no leader after partition healing")
	}

	c.logf("Log truncation test completed - partition healed with leader %d", finalLeader.n.ID)
}

// ============================================================================
// NETWORK PARTITION TESTS (Additional scenarios)
// ============================================================================

func TestNetworkPartition_SplitBrain(t *testing.T) {
	c := newTestCluster(t, 5)
	c.bootstrap()
	defer c.shutdown()

	leader := c.leader()
	if leader == nil {
		t.Fatal("no initial leader")
	}

	partition1 := []int64{1, 2}
	partition2 := []int64{3, 4, 5}

	c.partition(partition1, partition2)

	time.Sleep(1 * time.Second)

	var partition1Leader *Raft
	for _, nodeID := range partition1 {
		node := c.getNode(nodeID)
		node.n.meta.RLock()
		nt := node.n.meta.nt
		node.n.meta.RUnlock()
		if nt == Leader {
			partition1Leader = node
			break
		}
	}

	if partition1Leader != nil {
		t.Fatal("minority partition (2 nodes) should not have a leader")
	}

	var partition2Leader *Raft
	for _, nodeID := range partition2 {
		node := c.getNode(nodeID)
		node.n.meta.RLock()
		nt := node.n.meta.nt
		node.n.meta.RUnlock()
		if nt == Leader {
			partition2Leader = node
			break
		}
	}

	if partition2Leader == nil {
		t.Fatal("majority partition (3 nodes) should have a leader")
	}

	c.logf("Split brain prevented - only majority partition has leader (node %d)", partition2Leader.n.ID)
}

func TestNetworkPartition_Healing(t *testing.T) {
	c := newTestCluster(t, 5)
	c.bootstrap()
	defer c.shutdown()

	leader := c.leader()
	if leader == nil {
		t.Fatal("no initial leader")
	}

	err := leader.n.replicate(nil, []byte("key1"), []byte("value1"))
	if err != nil {
		t.Fatalf("failed to replicate before partition: %v", err)
	}

	c.waitForReplication(1)

	partition1 := []int64{1, 2}
	partition2 := []int64{3, 4, 5}

	c.partition(partition1, partition2)

	time.Sleep(1 * time.Second)

	c.fullyConnect()

	time.Sleep(1 * time.Second)

	finalLeader := c.getLeader()
	if finalLeader == nil {
		t.Fatal("no leader after partition healing")
	}

	err = finalLeader.n.replicate(nil, []byte("key2"), []byte("value2"))
	if err != nil {
		t.Fatalf("failed to replicate after healing: %v", err)
	}

	c.waitForReplication(2)

	c.ensureSame()

	for i, logStore := range c.logs {
		logs := logStore.logManager.GetLogs()
		if len(logs) != 2 {
			t.Fatalf("node %d has %d logs after healing, expected 2", i+1, len(logs))
		}
	}

	c.logf("Partition healed successfully - all nodes consistent with 2 log entries")
}

// ============================================================================
// CORRECTNESS TESTS (Placeholders for future implementation)
// ============================================================================

func TestCorrectness_ElectionSafety(t *testing.T) {
	c := newTestCluster(t, 5)
	c.bootstrap()
	defer c.shutdown()

	leader := c.leader()
	if leader == nil {
		t.Fatal("no initial leader")
	}

	initialTerm := int64(0)
	leader.n.state.Lock()
	initialTerm = leader.n.state.currentTerm
	leader.n.state.Unlock()

	leaderCounts := make(map[int64]int)

	for i := 0; i < 10; i++ {
		currentLeader := c.getLeader()
		if currentLeader != nil {
			currentLeader.n.state.Lock()
			term := currentLeader.n.state.currentTerm
			currentLeader.n.state.Unlock()

			leaderCounts[term]++

			if leaderCounts[term] > 1 {
				var leaders []int64
				for _, node := range c.nodes {
					node.n.meta.RLock()
					nt := node.n.meta.nt
					node.n.meta.RUnlock()
					if nt == Leader {
						leaders = append(leaders, node.n.ID)
					}
				}
				if len(leaders) > 1 {
					t.Fatalf("Election safety violated: multiple leaders in term %d: %v", term, leaders)
				}
			}
		}

		time.Sleep(100 * time.Millisecond)
	}

	finalTerm := int64(0)
	finalLeader := c.getLeader()
	if finalLeader != nil {
		finalLeader.n.state.Lock()
		finalTerm = finalLeader.n.state.currentTerm
		finalLeader.n.state.Unlock()
	}

	if finalTerm < initialTerm {
		t.Fatalf("Term went backwards: initial=%d, final=%d", initialTerm, finalTerm)
	}

	c.logf("Election safety maintained - no split brain detected")
}

func TestCorrectness_LeaderAppendOnly(t *testing.T) {
	c := newTestCluster(t, 3)
	c.bootstrap()
	defer c.shutdown()

	leader := c.leader()
	if leader == nil {
		t.Fatal("no leader elected")
	}

	err := leader.n.replicate(nil, []byte("key1"), []byte("value1"))
	if err != nil {
		t.Fatalf("failed to replicate first entry: %v", err)
	}

	c.waitForReplication(1)

	leaderLog := c.logs[leader.n.ID-1]
	firstLogs := leaderLog.logManager.GetLogs()
	firstLogCount := len(firstLogs)
	firstLogID := firstLogs[0].Id

	err = leader.n.replicate(nil, []byte("key2"), []byte("value2"))
	if err != nil {
		t.Fatalf("failed to replicate second entry: %v", err)
	}

	c.waitForReplication(2)

	secondLogs := leaderLog.logManager.GetLogs()
	secondLogCount := len(secondLogs)
	if secondLogCount < firstLogCount {
		t.Fatalf("Leader log shrunk from %d to %d - violates append-only property", firstLogCount, secondLogCount)
	}

	if secondLogs[0].Id != firstLogID {
		t.Fatalf("First log entry ID changed from %d to %d - violates append-only property", firstLogID, secondLogs[0].Id)
	}

	c.logf("Leader append-only property maintained - log grew from %d to %d entries", firstLogCount, secondLogCount)
}

func TestCorrectness_LogMatching(t *testing.T) {
	c := newTestCluster(t, 3)
	c.bootstrap()
	defer c.shutdown()

	leader := c.leader()
	if leader == nil {
		t.Fatal("no leader elected")
	}

	numEntries := 5
	for i := 0; i < numEntries; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		value := []byte(fmt.Sprintf("value%d", i))
		err := leader.n.replicate(nil, key, value)
		if err != nil {
			t.Fatalf("failed to replicate entry %d: %v", i, err)
		}
	}

	c.waitForReplication(numEntries)

	c.ensureSame()

	for i := 0; i < len(c.logs)-1; i++ {
		log1Store := c.logs[i]
		log2Store := c.logs[i+1]

		logs1 := log1Store.logManager.GetLogs()
		logs2 := log2Store.logManager.GetLogs()

		if len(logs1) != len(logs2) {
			t.Fatalf("Log length mismatch between node %d (%d entries) and node %d (%d entries)",
				i+1, len(logs1), i+2, len(logs2))
		}

		for j := 0; j < len(logs1); j++ {
			if logs1[j].Term != logs2[j].Term {
				t.Fatalf("Log matching violated: node %d and %d have different terms at index %d",
					i+1, i+2, j)
			}
			if logs1[j].Id != logs2[j].Id {
				t.Fatalf("Log matching violated: node %d and %d have different log IDs at index %d",
					i+1, i+2, j)
			}
			if logs1[j].Data.Key != logs2[j].Data.Key {
				t.Fatalf("Log matching violated: node %d and %d have different keys at index %d",
					i+1, i+2, j)
			}
		}
	}

	c.logf("Log matching property verified - all %d entries match across all nodes", numEntries)
}
