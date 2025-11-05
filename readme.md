## Focus of this project:
- Understanding database internals
- Understanding distributed systems


## Not focus of this project:
- Writing my own B+ Tree


Dev log [2025]:
[26/9]: Decided that i was spending too much time focusing on low level details
such as writing my own b+ tree and following a very terrible book. All I needed
was some DI material and an open source implementation to refer to.
I will be working on harmonyDB which is a read-efficient, B+ tree based database.
It will work on mmap for disk-back persistence.
inspirations: nutsdb, bbolt, mkdb

[3/11]: Finished the first draft of the raft consensus protocol. Read the paper
and implemented the algorithm from scratch. From the CRDT project i learned that
focusing on one problem/ un-answered question/ doubt/ bug at a time is the best
way to tackle a complex project like this. I am going to create a temp file where
i log every issue i have solved and open ones and tackle them one by one.
