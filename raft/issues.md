[Leader Election]:
[X] node should ask for votes after election timeout
[X] leader should get elected after majority votes received
[X] if a leader is already elected, reachable and sending heartbeats, no other leader should be elected.
[] at most one leader can be elected in a given term
[X] a candidate must receive votes from a majority of servers to become leader
[] nodes should vote for at most one candidate in a given term
[] nodes should only vote for candidates with logs at least as up-to-date as their own
[] election timeout should be randomized to avoid split votes
[] nodes should reject vote requests from candidates with stale terms
[] nodes should update their term when receiving higher term in vote request

[Log Replication]:
[X] a leader should only replicate entries not previously seen by the follower
[] leader should not commit entries from previous terms directly
[] leader should only commit entries from current term once replicated on majority
[] entries committed in previous terms become committed when current term entry is committed
[] logs should be consistent across all nodes for committed entries
[] if two logs contain an entry with same index and term, they are identical up to that point
[] leader should never overwrite or delete entries in its log
[] follower should truncate conflicting entries when receiving AppendEntries
[] leader should include prevLogIndex and prevLogTerm to ensure log consistency
[] nodes should reject AppendEntries if they don't have matching prevLogIndex/prevLogTerm
[] leader should retry failed AppendEntries with decremented nextIndex
[] committed entries should never be lost once applied to state machine

[Safety Properties]:
[] state machine safety - if server applies log entry at index, no other server applies different entry at same index
[] leader completeness - if log entry committed in given term, entry present in logs of all future leaders
[] log matching - if two entries in different logs have same index and term, then logs identical in all preceding entries
[] leader append-only - leader never overwrites or deletes entries in its log
[] monotonic term progression - currentTerm only increases, never decreases
[] election safety - at most one leader per term

[Liveness Properties]:
[] if majority of servers are reachable, system should make progress
[] leader should eventually be elected if no leader exists and majority reachable
[] committed entries should eventually be applied to all available state machines
[] network partitions should not cause permanent unavailability when majority partition exists

[Cluster/ Membership Changes]:
[] configuration changes should not create periods with two disjoint majorities
[] nodes should use latest configuration for determining majorities
[] configuration changes should be committed before taking effect
[] nodes should gracefully handle adding/removing peers from cluster

