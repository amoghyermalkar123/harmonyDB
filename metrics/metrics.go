package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Raft State Metrics (Gauges)
	RaftState = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "harmonydb_raft_state",
			Help: "Current Raft state (1=Follower, 2=Candidate, 3=Leader)",
		},
		[]string{"node_id"},
	)

	RaftTerm = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "harmonydb_raft_term",
			Help: "Current term number",
		},
		[]string{"node_id"},
	)

	RaftCommitIndex = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "harmonydb_raft_commit_index",
			Help: "Last committed log index",
		},
		[]string{"node_id"},
	)

	RaftAppliedIndex = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "harmonydb_raft_applied_index",
			Help: "Last applied to state machine index",
		},
		[]string{"node_id"},
	)

	RaftLeaderID = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "harmonydb_raft_leader_id",
			Help: "Current leader's node ID (0 if unknown)",
		},
		[]string{"node_id"},
	)

	RaftClusterSize = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "harmonydb_raft_cluster_size",
			Help: "Number of peers in cluster",
		},
		[]string{"node_id"},
	)

	RaftLogEntries = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "harmonydb_raft_log_entries",
			Help: "Total log entries",
		},
		[]string{"node_id"},
	)

	// Raft Operation Counters
	RaftElectionsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "harmonydb_raft_elections_total",
			Help: "Total elections initiated",
		},
		[]string{"node_id", "result"},
	)

	RaftVotesGrantedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "harmonydb_raft_votes_granted_total",
			Help: "Total votes granted to candidates",
		},
		[]string{"node_id"},
	)

	RaftLeaderChangesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "harmonydb_raft_leader_changes_total",
			Help: "Total leadership transitions",
		},
		[]string{"node_id"},
	)

	RaftAppendEntriesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "harmonydb_raft_append_entries_total",
			Help: "Total AppendEntries RPCs",
		},
		[]string{"node_id", "type", "result"},
	)

	RaftRequestVoteTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "harmonydb_raft_request_vote_total",
			Help: "Total RequestVote RPCs",
		},
		[]string{"node_id", "result"},
	)

	RaftLogTruncationsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "harmonydb_raft_log_truncations_total",
			Help: "Total log truncation events",
		},
		[]string{"node_id"},
	)

	// Raft Latency Histograms
	RaftReplicationDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "harmonydb_raft_replication_duration_seconds",
			Help:    "Time to replicate to majority",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 12), // 1ms to 4s
		},
		[]string{"node_id"},
	)

	RaftAppendEntriesDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "harmonydb_raft_append_entries_duration_seconds",
			Help:    "Per-peer AppendEntries latency",
			Buckets: prometheus.ExponentialBuckets(0.0005, 2, 12), // 0.5ms to 2s
		},
		[]string{"node_id", "peer_id"},
	)

	RaftElectionDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "harmonydb_raft_election_duration_seconds",
			Help:    "Time from candidate to leader",
			Buckets: prometheus.ExponentialBuckets(0.01, 2, 10), // 10ms to 10s
		},
		[]string{"node_id"},
	)

	RaftCommitLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "harmonydb_raft_commit_latency_seconds",
			Help:    "Time from log append to commit",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 12),
		},
		[]string{"node_id"},
	)

	// BTree Metrics
	BTreeOperationsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "harmonydb_btree_operations_total",
			Help: "Total BTree operations",
		},
		[]string{"operation"},
	)

	BTreeCacheHitsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "harmonydb_btree_cache_hits_total",
			Help: "Total page cache hits",
		},
	)

	BTreeCacheMissesTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "harmonydb_btree_cache_misses_total",
			Help: "Total page cache misses",
		},
	)

	BTreeSplitsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "harmonydb_btree_splits_total",
			Help: "Total node splits",
		},
		[]string{"node_type"},
	)

	BTreeOperationDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "harmonydb_btree_operation_duration_seconds",
			Help:    "BTree operation latency",
			Buckets: prometheus.ExponentialBuckets(0.00001, 2, 15), // 10us to 300ms
		},
		[]string{"operation"},
	)

	BTreeTreeDepth = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "harmonydb_btree_tree_depth",
			Help: "Current tree depth",
		},
	)

	BTreeTotalPages = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "harmonydb_btree_total_pages",
			Help: "Total allocated pages",
		},
	)

	BTreeDirtyPages = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "harmonydb_btree_dirty_pages",
			Help: "Pages pending flush",
		},
	)

	BTreeCacheSizeBytes = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "harmonydb_btree_cache_size_bytes",
			Help: "Memory used by page cache",
		},
	)

	// Storage Metrics
	StorageFlushDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "harmonydb_storage_flush_duration_seconds",
			Help:    "Page flush latency",
			Buckets: prometheus.ExponentialBuckets(0.0001, 2, 12),
		},
	)

	StorageFlushPagesTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "harmonydb_storage_flush_pages_total",
			Help: "Total pages flushed to disk",
		},
	)

	StorageWriteBytesTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "harmonydb_storage_write_bytes_total",
			Help: "Total bytes written to disk",
		},
	)

	// WAL Metrics
	WALEntriesTotal = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "harmonydb_wal_entries_total",
			Help: "Total WAL entries",
		},
	)

	WALAppendDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "harmonydb_wal_append_duration_seconds",
			Help:    "WAL append latency",
			Buckets: prometheus.ExponentialBuckets(0.00001, 2, 12),
		},
	)

	// HTTP API Metrics
	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "harmonydb_http_requests_total",
			Help: "Total HTTP requests",
		},
		[]string{"method", "endpoint", "status"},
	)

	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "harmonydb_http_request_duration_seconds",
			Help:    "HTTP request latency",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 12),
		},
		[]string{"method", "endpoint"},
	)

	HTTPRequestSizeBytes = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "harmonydb_http_request_size_bytes",
			Help:    "HTTP request body size",
			Buckets: prometheus.ExponentialBuckets(100, 2, 10),
		},
		[]string{"method", "endpoint"},
	)

	HTTPResponseSizeBytes = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "harmonydb_http_response_size_bytes",
			Help:    "HTTP response body size",
			Buckets: prometheus.ExponentialBuckets(100, 2, 10),
		},
		[]string{"method", "endpoint"},
	)

	HTTPInflightRequests = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "harmonydb_http_inflight_requests",
			Help: "Currently processing requests",
		},
	)
)
