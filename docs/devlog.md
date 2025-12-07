## Focus of this project:
- Understanding database internals
- Understanding distributed systems


## Not focus of this project:
- Writing my own B+ Tree
   - update: Welp, wrote my own

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

[9/11]: Finished the first draft of the database now. Implemented B+Tree, raft then
tested them together. Then I also added a file based persistence layer and then tested
the whole thing.
Problems faced: From the start of this journey to the current time as I write this devlog
I didn't face any major hurdles. I found these things clicking pretty nicely and sort of
enjoyed putting this basic database together.
Observations: To but the B+tree and the whole storage side of things together I had to read
database internals book, take a little inspiration from some oss implementations to really
write my own. What I have realized is while theory is understandable in one ways or many, I
can only really internalize something that I am reading/ learning once I implement it or write
about it or teach it in one way or the other, only then can I consider the subject `studied`.
Another observation, fundamental difference between studying and learning. Learning is about
joy, the initial spark and curiosity - a way of procrastinating for smart people. But studying
on the other hand is about commitment and laser sharp focus. To study something means to
relentlessly pursue the understanding of the knowledge that a subject entails no matter how
boring/ tedious/ difficult things might seem during the journey.
During my studying journey of distributed systems and databases I halfway
realized that I was immediately being distracted by shiny new projects/ concepts/ blogs
or ideas constantly. This happened when I was studying a complex topic or tackling a bug
or most redundantly - not knowing what to study next or add to my database project.
Which brings me to the most important skill I need to learn - planning studies. I need to
have closed loops when it comes to studying sessions. I need to know before every study session
what I want to achieve in that session and as the session ends I need to plan for the next one.
This achieves a rather simple thing - Knowing exactly what to do in the next session. After each
session ends I also know (because I plan for the next one) what I couldn't achieve in today's session.

More often than not procrastination is a result rather than a cause. I am not procrastinating because
I don't want to study. I procrastinated because I was unsure what I wanted to achieve today. Planning
is the answer. Some days the session can be short/ some days longer. Not everyday can be productive but
I need to plan everyday with clear actions so that I progress each day, instead of only some.

[11/11] Had this idea where shapes of data are replicated differently. One shape of data is always strongly
replicated and other shape of data is always eventually replicated.

[12/11] Facing issues with testing my database beyond the basic local testing and unit tests. Tried researching
how actual DB's do it but all i can find is jepsen. But it's a steep journey because then I would have to learn
clojure. For which I just don't have any time right now. For now I am going to check a bit in depth if i can find
what etcd and cockroach db do.
Ok after much finding I figured out that jepsen is really only the tool that is battle tested and worth of using
in actual databases. Since I tried maelstrom and faced issues, for one it requires the db to implement it's
protocol and network api's/infra to communicate. Since I have already written my basic draft of distrbuted db
It's a hassle to go back and change things just so I can test it with maelstrom - this is not worth it. I think
I will continue on my journey to learn more and introduce more things in my database for now and will be relying
on basic unit tests, local tests.
In the future I also plan to deploy this to my rpi for stress testing.
As for jepsen testing - my database will never be a serious project. It will be serious enough for me to learn
alot from DB engineering. So I will learn clojure and jepsen a little later. For now the main focus is to learn
by building a lot and then creating some learning material to cement my knowledge.

the best way would be that when I'm satisfied enough with this distributed database project, my mind will be at ease
and then I will take up clojure & jepsen for test my distributed db with those then. For now, we're good with
sticking to the basics.

[22/11] I realized why raft durability has to run seperately from the actual kv storage that makes up the core of the database
I was testing harmonydb on my local today with a few new changes, i had added a docker compose setup for better experience
of running a 3-node local cluster. When i ran the load test the test ran with 100% success rate. raft consensus is reached
when successfull replication and application of committed entries on the leader happens and the updated commit index is
communicated in the next heartbeat/ append entries rpc whicheveer happens first. But with the compose changes i had done
i realized that the database files weren't being created and since in the current implementation raft is purely in-mem i realized
that in real environments as well, the consensus works much seperately than the underlying kv from a file perspective. Everything
should go in the kv in order and something happens to the core durable file, everything is ruined. Hence it's better to have a WAL based
file-backed persistence for raft to maintain totally ordered history incase the main engine is effed.

[25/11] Understood what linearizability is and how it relates to raft. Linearizability is a consistency model that ensures that all operations appear to have happened in some sequential order. 
In the context of Raft, linearizability ensures that all operations appear to have happened in the order they were proposed, even if they are executed asynchronously.
Safety is ensured by not responding back to the client until an entry is durably committed to WAL, replicated to the nodes in the cluster via which we have received quorum and finally added to the primary
durable KV storage engine.
One of my very basic tests showed me this actually, it Put data and immediately tried to retrieve and it was failing. Previous tests didnt because they were adding sleeps between subsequent puts and gets and then i realized that the tests were doing incorrect assertions!

[2/12] Was working on recovery based on WAL trying to figure out how at bootup post crash recovery do we actually build the in-memory
wal based on what we wrote in the file post crash. the problem is what exactly to load. which operations do you load? then i found the answer is simple. use the last applied index, but then even this needs to be persisted before crash. not long after that i realized we also need to re-build the in memory b+tree. So now i am thinking what all metadata we will need to constantly store right before a crash. Also, since every file write is in OS's buffer, how do you make the decision about when to call fsync()?
on one hand it seems safe and in-line with the acid rule, but on the other hand i cant imagine calling fsync for every KV might be the most performance optimal - so you would want to batch some calls then call fsync()? for better write throughput?

for which entries do you decide you want to load the b+tree for? only left to be applied right? but that does not seem optimal. also does not seem right because you also want to serve queries. I guess you load pages? but which ones? wait, it seems obvious to load all pages in-memory lol, atleast for now because this is a toy db.

just realized i cannot implement mmap for my database. i would need a buffer pool manager as the database is not embedded. plus i will need to integrate this in recovery mechanisms and would also need an LRU for the same.

[3/11] slpt: when developing a database in it's early stages, don't work on the decode path. only work on the encode path. this will make sure that as the db evolves incrementally, you can focus on building it bottom to top and are free to make changes with enough flexibility because the file format is flexible. since the decode path is not there it will just create additional data in a file and after restarts or between tests the previously persisted data will not be accessible in the next run, but atleast you are free to change the file format.
