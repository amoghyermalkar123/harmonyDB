package harmonydb

import (
	"fmt"
	"harmonydb/wal"
	"log/slog"
)

// ReplayWAL replays WAL entries to the B+tree that haven't been applied yet
// It calculates which entries to apply based on: commitIndex - lastApplied
func ReplayWAL(btree *BTree, walStorage wal.Storage) error {
	// Read metadata to get the last applied index
	lastCommitIndex, lastAppliedIndex := btree.readMeta()

	slog.Info("starting WAL replay",
		"last_commit_index", lastCommitIndex,
		"last_applied_index", lastAppliedIndex)

	// If lastAppliedIndex >= lastCommitIndex, nothing to replay
	if lastAppliedIndex >= lastCommitIndex {
		slog.Info("no entries to replay", "last_applied", lastAppliedIndex)
		return nil
	}

	// Get entries that need to be applied: (lastAppliedIndex, lastCommitIndex]
	// This is the range: lastAppliedIndex < entry.Id <= lastCommitIndex
	entriesToReplay := walStorage.GetLogsAfter(lastAppliedIndex + 1)

	appliedCount := 0
	for _, entry := range entriesToReplay {
		// Only apply entries up to lastCommitIndex
		if entry.GetId() > lastCommitIndex {
			break
		}

		cmd := entry.GetData()
		if cmd == nil {
			slog.Error("entry has no data", "entry_id", entry.GetId())
			continue
		}

		// Apply the command based on operation type
		switch cmd.GetOp() {
		case "PUT":
			key := []byte(cmd.GetKey())
			value := []byte(cmd.GetValue())

			if err := btree.put(key, value); err != nil {
				return fmt.Errorf("replay PUT entry %d: %w", entry.GetId(), err)
			}

			appliedCount++
			slog.Info("replayed entry",
				"id", entry.GetId(),
				"op", cmd.GetOp(),
				"key", cmd.GetKey())

		default:
			slog.Error("unknown operation in WAL",
				"op", cmd.GetOp(),
				"entry_id", entry.GetId())
		}
	}

	// Update metadata with the new lastAppliedIndex
	if appliedCount > 0 {
		newLastApplied := lastAppliedIndex + int64(appliedCount)
		btree.updateMeta(newLastApplied, lastCommitIndex)
		slog.Info("WAL replay complete",
			"entries_applied", appliedCount,
			"new_last_applied", newLastApplied)
	}

	return nil
}
