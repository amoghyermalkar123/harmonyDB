## Intro:

A toy database for learning purposes.

Database details:
- B+ Tree (so optimized for read-heavy workloads)
- MMAP (for in-memory operations with disk-backed persistence for durability)

### Notes:
This section consists notes for my own understanding and in the future for an AI Agent for boosting productivity.
I am using 2 reference points for this:
- https://build-your-own.org/database/#table-of-contents
- Database Internals book (with relevant chapters)


Why not use files if the database is just a file anyway?

There are 2 requirements: to survive crashes; to confirm the state of durability.
Considering that most databases run on filesystems, a filesystem must also meet these requirements.
So filesystems are somewhat similar to databases. However, the big difference is that typical filesystem
use (writing to files) has no durability guarantee, resulting in data loss or corruption after a power loss,
while typical database use guarantees durability.

Hard problem: A database must recover from a crash to a reasonable state.

Types of atomicity

“Atomicity” means different things in different contexts. There are at least 2 kinds atomicity:

    Power-loss atomic: Will a reader observe a bad state after a crash?
    Readers-writer atomic: Will a reader observe a bad state with a concurrent writer?

Another issue with fsync is error handling. If fsync fails, the DB update fails, but what if you read the file afterwards?
You may get the new data even if fsync failed (because of the OS page cache)! This behavior is filesystem dependent (https://www.usenix.org/conference/atc20/presentation/rebello)

Choosing a data structure is vital for a database, because the data structure dictates what kinds of queries are supported.
Real-world OLTP queries fall into 1 of the 3 types:

    Scan the whole data set without an index if the table is small.
    Point query: Query the index by a single key.
    Range query: Query a range of keys in sort order.




### B+ Tree
Find:
The algorithm starts from the root and performs a binary search, comparing the searched key with the keys stored
in the root node until it finds the first separator key that is greater than the searched value.
This locates a searched subtree. As we’ve discussed previously, index keys split the tree into subtrees with
boundaries between two neighboring keys. As soon as we find the subtree, we follow the pointer that corresponds to it
and continue the same search process (locate the separator key, follow the pointer) until we reach a target leaf node,
where we either find the searched key or conclude it is not present by locating its predecessor.
On each level, we get a more detailed view of the tree: we start on the most coarse-grained level


Insert:
To insert the value into a B-Tree, we first have to locate the target leaf and find the insertion point.

Split:
The node is split if the following conditions hold:
For leaf nodes: if the node can hold up to N key-value pairs, and inserting one more key-value pair brings it over its maximum capacity N.
For internal nodes: if the node can hold up to N + 1 pointers, and inserting one more pointer brings it over its maximum capacity N + 1.
**promotion**:
    Splits are done by allocating the new node, transferring half the elements from the splitting node to it, and
    adding its first key and pointer to the parent node. In this case, we say that the key is promoted.

All elements after the split point (including split point in the case of leaf node split) are transferred to the newly created sibling node,
and the rest of the elements remain in the splitting node.

If the parent node is full and does not have space available for the promoted key and pointer to the newly created node,
it has to be split as well. This operation might propagate recursively all the way to the root.

